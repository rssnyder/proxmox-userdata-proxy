package proxy

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/rileysndr/proxmox-userdata-proxy/internal/proxmox"
	"github.com/rileysndr/proxmox-userdata-proxy/internal/snippet"
)

func setupTestHandler(t *testing.T, proxmoxHandler http.HandlerFunc) (*Handler, *httptest.Server, string) {
	mockServer := httptest.NewServer(proxmoxHandler)

	client, err := proxmox.NewClient(mockServer.URL, true)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	// Create temp directory for snippets
	tmpDir, err := os.MkdirTemp("", "handler-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	writer := snippet.NewWriter(map[string]string{
		"local": tmpDir,
	})

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := NewHandler(client, writer, logger)

	return handler, mockServer, tmpDir
}

func TestHandler_HealthCheck(t *testing.T) {
	handler, mockServer, tmpDir := setupTestHandler(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("health check should not reach Proxmox")
	})
	defer mockServer.Close()
	defer os.RemoveAll(tmpDir)

	paths := []string{"/health", "/healthz"}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			if rr.Code != http.StatusOK {
				t.Errorf("expected status 200, got %d", rr.Code)
			}

			var resp map[string]string
			json.Unmarshal(rr.Body.Bytes(), &resp)
			if resp["status"] != "ok" {
				t.Errorf("expected status 'ok', got '%s'", resp["status"])
			}
		})
	}
}

func TestHandler_Passthrough(t *testing.T) {
	var receivedPath string
	var receivedMethod string

	handler, mockServer, tmpDir := setupTestHandler(t, func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		receivedMethod = r.Method
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": []map[string]interface{}{
				{"vmid": 100, "name": "test-vm"},
			},
		})
	})
	defer mockServer.Close()
	defer os.RemoveAll(tmpDir)

	req := httptest.NewRequest(http.MethodGet, "/api2/json/nodes/pve/qemu?full=1", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}
	if receivedPath != "/api2/json/nodes/pve/qemu" {
		t.Errorf("expected path '/api2/json/nodes/pve/qemu', got '%s'", receivedPath)
	}
	if receivedMethod != http.MethodGet {
		t.Errorf("expected method GET, got '%s'", receivedMethod)
	}
}

func TestHandler_CreateVM_WithCloudInit(t *testing.T) {
	var receivedBody string

	handler, mockServer, tmpDir := setupTestHandler(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		receivedBody = string(body)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": "UPID:pve:00001234:00000000:12345678:qmcreate:100:root@pam:",
		})
	})
	defer mockServer.Close()
	defer os.RemoveAll(tmpDir)

	body := url.Values{}
	body.Set("vmid", "100")
	body.Set("name", "test-vm")
	body.Set("memory", "2048")
	body.Set("cloudinit_userdata", "#cloud-config\npackages:\n  - nginx\n")
	body.Set("cloudinit_storage", "local")

	req := httptest.NewRequest(http.MethodPost, "/api2/json/nodes/pve/qemu", strings.NewReader(body.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Verify cloud-init fields were removed and cicustom was added
	values, _ := url.ParseQuery(receivedBody)
	if values.Get("cloudinit_userdata") != "" {
		t.Error("cloudinit_userdata should be removed before forwarding")
	}
	if values.Get("cloudinit_storage") != "" {
		t.Error("cloudinit_storage should be removed before forwarding")
	}
	if !strings.Contains(values.Get("cicustom"), "user=local:snippets/vm-100-cloud-init-user.yaml") {
		t.Errorf("cicustom should be set, got: %s", values.Get("cicustom"))
	}

	// Verify snippet file exists
	if _, err := os.Stat(tmpDir + "/snippets/vm-100-cloud-init-user.yaml"); os.IsNotExist(err) {
		t.Error("snippet file should be created")
	}
}

func TestHandler_CreateVM_WithoutCloudInit(t *testing.T) {
	var receivedBody string

	handler, mockServer, tmpDir := setupTestHandler(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		receivedBody = string(body)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": "UPID:pve:00001234:00000000:12345678:qmcreate:100:root@pam:",
		})
	})
	defer mockServer.Close()
	defer os.RemoveAll(tmpDir)

	body := url.Values{}
	body.Set("vmid", "100")
	body.Set("name", "test-vm")
	body.Set("memory", "2048")

	req := httptest.NewRequest(http.MethodPost, "/api2/json/nodes/pve/qemu", strings.NewReader(body.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	// Body should be passed through mostly unchanged
	values, _ := url.ParseQuery(receivedBody)
	if values.Get("vmid") != "100" {
		t.Error("vmid should be preserved")
	}
	if values.Get("cicustom") != "" {
		t.Error("cicustom should not be added when no cloud-init provided")
	}
}

func TestHandler_CreateVM_ProxmoxError_RollsBackSnippet(t *testing.T) {
	handler, mockServer, tmpDir := setupTestHandler(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"errors": map[string]string{
				"vmid": "VM 100 already exists",
			},
		})
	})
	defer mockServer.Close()
	defer os.RemoveAll(tmpDir)

	body := url.Values{}
	body.Set("vmid", "100")
	body.Set("name", "test-vm")
	body.Set("cloudinit_userdata", "#cloud-config\npackages:\n  - nginx\n")
	body.Set("cloudinit_storage", "local")

	req := httptest.NewRequest(http.MethodPost, "/api2/json/nodes/pve/qemu", strings.NewReader(body.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rr.Code)
	}

	// Snippet should be rolled back (deleted)
	if _, err := os.Stat(tmpDir + "/snippets/vm-100-cloud-init-user.yaml"); !os.IsNotExist(err) {
		t.Error("snippet file should be deleted on Proxmox error")
	}
}

func TestHandler_CreateVM_InvalidCloudInit(t *testing.T) {
	handler, mockServer, tmpDir := setupTestHandler(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("request should not reach Proxmox for invalid cloud-init")
	})
	defer mockServer.Close()
	defer os.RemoveAll(tmpDir)

	body := url.Values{}
	body.Set("vmid", "100")
	body.Set("cloudinit_userdata", "") // Empty content is invalid
	body.Set("cloudinit_storage", "local")

	req := httptest.NewRequest(http.MethodPost, "/api2/json/nodes/pve/qemu", strings.NewReader(body.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandler_CreateVM_UnknownStorage(t *testing.T) {
	handler, mockServer, tmpDir := setupTestHandler(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("request should not reach Proxmox for unknown storage")
	})
	defer mockServer.Close()
	defer os.RemoveAll(tmpDir)

	body := url.Values{}
	body.Set("vmid", "100")
	body.Set("cloudinit_userdata", "#cloud-config\npackages:\n  - nginx\n")
	body.Set("cloudinit_storage", "nonexistent-storage")

	req := httptest.NewRequest(http.MethodPost, "/api2/json/nodes/pve/qemu", strings.NewReader(body.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rr.Code)
	}
}

func TestHandler_CloneVM_WithCloudInit(t *testing.T) {
	requestCount := 0
	var cloneBody string
	var configBody string

	handler, mockServer, tmpDir := setupTestHandler(t, func(w http.ResponseWriter, r *http.Request) {
		requestCount++

		if strings.Contains(r.URL.Path, "/clone") {
			body, _ := io.ReadAll(r.Body)
			cloneBody = string(body)

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": "UPID:pve:00001234:00000000:12345678:qmclone:101:root@pam:",
			})
		} else if strings.Contains(r.URL.Path, "/config") {
			body, _ := io.ReadAll(r.Body)
			configBody = string(body)

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": nil,
			})
		}
	})
	defer mockServer.Close()
	defer os.RemoveAll(tmpDir)

	body := url.Values{}
	body.Set("newid", "101")
	body.Set("name", "cloned-vm")
	body.Set("full", "1")
	body.Set("cloudinit_userdata", "#cloud-config\npackages:\n  - nginx\n")
	body.Set("cloudinit_storage", "local")

	req := httptest.NewRequest(http.MethodPost, "/api2/json/nodes/pve/qemu/100/clone", strings.NewReader(body.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Should make 2 requests: clone + config update
	if requestCount != 2 {
		t.Errorf("expected 2 requests (clone + config), got %d", requestCount)
	}

	// Clone request should not have cloud-init fields
	cloneValues, _ := url.ParseQuery(cloneBody)
	if cloneValues.Get("cloudinit_userdata") != "" {
		t.Error("cloudinit_userdata should not be in clone request")
	}

	// Config update should have cicustom
	configValues, _ := url.ParseQuery(configBody)
	if !strings.Contains(configValues.Get("cicustom"), "user=local:snippets/vm-101-cloud-init-user.yaml") {
		t.Errorf("config update should set cicustom, got: %s", configValues.Get("cicustom"))
	}

	// Snippet file should exist
	if _, err := os.Stat(tmpDir + "/snippets/vm-101-cloud-init-user.yaml"); os.IsNotExist(err) {
		t.Error("snippet file should be created")
	}
}

func TestHandler_CloneVM_WithoutCloudInit(t *testing.T) {
	requestCount := 0

	handler, mockServer, tmpDir := setupTestHandler(t, func(w http.ResponseWriter, r *http.Request) {
		requestCount++

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": "UPID:pve:00001234:00000000:12345678:qmclone:101:root@pam:",
		})
	})
	defer mockServer.Close()
	defer os.RemoveAll(tmpDir)

	body := url.Values{}
	body.Set("newid", "101")
	body.Set("name", "cloned-vm")
	body.Set("full", "1")

	req := httptest.NewRequest(http.MethodPost, "/api2/json/nodes/pve/qemu/100/clone", strings.NewReader(body.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	// Should only make 1 request (clone only, no config update)
	if requestCount != 1 {
		t.Errorf("expected 1 request (clone only), got %d", requestCount)
	}
}

func TestHandler_HasCloudInitFields(t *testing.T) {
	handler := &Handler{}

	tests := []struct {
		name        string
		body        string
		contentType string
		expected    bool
	}{
		{"form with field", "vmid=100&cloudinit_userdata=test", "application/x-www-form-urlencoded", true},
		{"form without field", "vmid=100&name=test", "application/x-www-form-urlencoded", false},
		{"json with field", `{"vmid":100,"cloudinit_userdata":"test"}`, "application/json", true},
		{"json without field", `{"vmid":100,"name":"test"}`, "application/json", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := handler.hasCloudInitFields([]byte(tt.body), tt.contentType)
			if got != tt.expected {
				t.Errorf("hasCloudInitFields() = %v, expected %v", got, tt.expected)
			}
		})
	}
}
