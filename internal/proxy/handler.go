package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/rileysndr/proxmox-userdata-proxy/internal/proxmox"
	"github.com/rileysndr/proxmox-userdata-proxy/internal/snippet"
)

// Handler is the main proxy handler.
type Handler struct {
	client  *proxmox.Client
	writer  *snippet.Writer
	logger  *slog.Logger
}

// NewHandler creates a new proxy handler.
func NewHandler(client *proxmox.Client, writer *snippet.Writer, logger *slog.Logger) *Handler {
	return &Handler{
		client:  client,
		writer:  writer,
		logger:  logger,
	}
}

// ServeHTTP handles all proxy requests.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	requestID := RequestID(r.Context())
	logger := h.logger.With(slog.String("request_id", requestID))

	// Health check endpoints
	if r.URL.Path == "/health" || r.URL.Path == "/healthz" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		return
	}

	// Determine operation type
	operation, node, sourceVMID := ParsePath(r.URL.Path)

	logger.Debug("handling request",
		slog.String("operation", operation),
		slog.String("node", node),
		slog.Int("source_vmid", sourceVMID),
	)

	switch operation {
	case "create":
		h.handleCreateVM(w, r, node, logger)
	case "clone":
		h.handleCloneVM(w, r, node, sourceVMID, logger)
	default:
		h.handlePassthrough(w, r, logger)
	}
}

func (h *Handler) handleCreateVM(w http.ResponseWriter, r *http.Request, node string, logger *slog.Logger) {
	if r.Method != http.MethodPost {
		h.handlePassthrough(w, r, logger)
		return
	}

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "failed to read request body", err)
		return
	}
	r.Body.Close()

	contentType := r.Header.Get("Content-Type")

	// Check if this request has our custom fields
	if !h.hasCloudInitFields(body, contentType) {
		// No custom fields, pass through unchanged
		logger.Debug("no cloud-init fields, passing through")
		h.forwardRequest(w, r, bytes.NewReader(body), contentType, logger)
		return
	}

	// Transform the request
	logger.Info("transforming create VM request with cloud-init")
	result, err := TransformCreateVM(body, contentType, h.writer)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "failed to transform request", err)
		return
	}

	logger.Info("created cloud-init snippet",
		slog.Int("vmid", result.VMID),
		slog.String("volume_id", result.SnippetVolumeID),
	)

	// Forward to Proxmox
	resp, err := h.client.Forward(r.Method, r.URL.Path, strings.NewReader(result.Body), result.ContentType, r.Header.Get("Authorization"))
	if err != nil {
		// Rollback: delete snippet on error
		if result.CreatedSnippet {
			if delErr := h.writer.Delete(result.StorageID, result.VMID); delErr != nil {
				logger.Error("failed to rollback snippet", slog.Any("error", delErr))
			}
		}
		h.writeError(w, http.StatusBadGateway, "failed to forward request to Proxmox", err)
		return
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		h.writeError(w, http.StatusBadGateway, "failed to read Proxmox response", err)
		return
	}

	// If Proxmox returned an error, rollback the snippet
	if resp.StatusCode >= 400 {
		logger.Error("Proxmox returned error",
			slog.Int("status", resp.StatusCode),
			slog.String("response", string(respBody)),
		)
		if result.CreatedSnippet {
			logger.Info("rolling back snippet due to Proxmox error")
			if delErr := h.writer.Delete(result.StorageID, result.VMID); delErr != nil {
				logger.Error("failed to rollback snippet", slog.Any("error", delErr))
			}
		}
	}

	// Copy response to client
	h.copyResponseWithBody(w, resp, respBody)
}

func (h *Handler) handleCloneVM(w http.ResponseWriter, r *http.Request, node string, sourceVMID int, logger *slog.Logger) {
	if r.Method != http.MethodPost {
		h.handlePassthrough(w, r, logger)
		return
	}

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "failed to read request body", err)
		return
	}
	r.Body.Close()

	contentType := r.Header.Get("Content-Type")

	// Check if this request has our custom fields
	if !h.hasCloudInitFields(body, contentType) {
		logger.Debug("no cloud-init fields in clone request, passing through")
		h.forwardRequest(w, r, bytes.NewReader(body), contentType, logger)
		return
	}

	// Parse the request to extract cloud-init fields
	var params map[string]interface{}
	if strings.Contains(contentType, "application/json") {
		json.Unmarshal(body, &params)
	} else {
		params, _ = parseFormURLEncoded(body)
	}

	userdata := params[FieldCloudInitUserdata].(string)
	storageID := params[FieldCloudInitStorage].(string)

	// Transform and forward the clone request (without cloud-init fields)
	result, err := TransformCloneVM(body, contentType, sourceVMID)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "failed to transform clone request", err)
		return
	}

	logger.Info("forwarding clone request",
		slog.Int("source_vmid", sourceVMID),
		slog.Int("new_vmid", result.VMID),
	)

	// Forward clone request to Proxmox
	resp, err := h.client.Forward(r.Method, r.URL.Path, strings.NewReader(result.Body), result.ContentType, r.Header.Get("Authorization"))
	if err != nil {
		h.writeError(w, http.StatusBadGateway, "failed to forward clone request to Proxmox", err)
		return
	}
	defer resp.Body.Close()

	// Read clone response
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		h.writeError(w, http.StatusBadGateway, "failed to read Proxmox response", err)
		return
	}

	// If clone failed, return the error
	if resp.StatusCode >= 400 {
		h.copyResponseWithBody(w, resp, respBody)
		return
	}

	// Clone succeeded, now write snippet and update VM config
	logger.Info("clone succeeded, writing cloud-init snippet",
		slog.Int("vmid", result.VMID),
	)

	volumeID, err := h.writer.Write(storageID, result.VMID, userdata)
	if err != nil {
		// Clone succeeded but snippet failed - log error but return clone success
		// User can manually configure cloud-init later
		logger.Error("failed to write cloud-init snippet after clone",
			slog.Any("error", err),
		)
		h.copyResponseWithBody(w, resp, respBody)
		return
	}

	// Update VM config with cicustom
	configPath := fmt.Sprintf("/api2/json/nodes/%s/qemu/%d/config", node, result.VMID)
	configBody := fmt.Sprintf("cicustom=user=%s", volumeID)

	configResp, err := h.client.Forward(http.MethodPut, configPath, strings.NewReader(configBody), "application/x-www-form-urlencoded", r.Header.Get("Authorization"))
	if err != nil {
		logger.Error("failed to update VM config with cloud-init",
			slog.Any("error", err),
		)
		// Return original clone response - VM exists but cloud-init not configured
		h.copyResponseWithBody(w, resp, respBody)
		return
	}
	configResp.Body.Close()

	if configResp.StatusCode >= 400 {
		logger.Error("Proxmox rejected cloud-init config update",
			slog.Int("status", configResp.StatusCode),
		)
	} else {
		logger.Info("successfully configured cloud-init on cloned VM",
			slog.Int("vmid", result.VMID),
			slog.String("volume_id", volumeID),
		)
	}

	// Return original clone response
	h.copyResponseWithBody(w, resp, respBody)
}

func (h *Handler) handlePassthrough(w http.ResponseWriter, r *http.Request, logger *slog.Logger) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "failed to read request body", err)
		return
	}
	r.Body.Close()

	logger.Debug("passing through request")
	h.forwardRequest(w, r, bytes.NewReader(body), r.Header.Get("Content-Type"), logger)
}

func (h *Handler) forwardRequest(w http.ResponseWriter, r *http.Request, body io.Reader, contentType string, logger *slog.Logger) {
	resp, err := h.client.Forward(r.Method, r.URL.Path+"?"+r.URL.RawQuery, body, contentType, r.Header.Get("Authorization"))
	if err != nil {
		h.writeError(w, http.StatusBadGateway, "failed to forward request to Proxmox", err)
		return
	}
	defer resp.Body.Close()

	h.copyResponse(w, resp)
}

func (h *Handler) hasCloudInitFields(body []byte, contentType string) bool {
	bodyStr := string(body)
	return strings.Contains(bodyStr, FieldCloudInitUserdata)
}

func (h *Handler) copyResponse(w http.ResponseWriter, resp *http.Response) {
	// Copy headers
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func (h *Handler) copyResponseWithBody(w http.ResponseWriter, resp *http.Response, body []byte) {
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	w.WriteHeader(resp.StatusCode)
	w.Write(body)
}

func (h *Handler) writeError(w http.ResponseWriter, status int, message string, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	errorResp := map[string]interface{}{
		"errors": map[string]string{
			"message": message,
		},
	}
	if err != nil {
		errorResp["errors"].(map[string]string)["detail"] = err.Error()
	}

	json.NewEncoder(w).Encode(errorResp)
}
