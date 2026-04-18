package proxy

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/rileysndr/proxmox-userdata-proxy/internal/snippet"
)

// Custom field names we add to the API
const (
	FieldCloudInitUserdata = "cloudinit_userdata"
	FieldCloudInitStorage  = "cloudinit_storage"
)

// TransformResult contains the result of transforming a request.
type TransformResult struct {
	// Modified body to send to Proxmox
	Body string
	// ContentType for the request
	ContentType string
	// SnippetVolumeID if a snippet was created (for rollback)
	SnippetVolumeID string
	// StorageID used (for rollback)
	StorageID string
	// VMID extracted from request
	VMID int
	// Whether we created a snippet
	CreatedSnippet bool
}

// TransformCreateVM transforms a VM creation request, extracting cloud-init fields
// and injecting cicustom if needed.
func TransformCreateVM(body []byte, contentType string, writer *snippet.Writer) (*TransformResult, error) {
	result := &TransformResult{}

	// Parse based on content type
	var params map[string]interface{}
	var err error

	if strings.Contains(contentType, "application/json") {
		params, err = parseJSON(body)
	} else {
		// Default to form-urlencoded (Proxmox's preferred format)
		params, err = parseFormURLEncoded(body)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to parse request body: %w", err)
	}

	// Extract VMID
	vmid, err := extractVMID(params)
	if err != nil {
		return nil, err
	}
	result.VMID = vmid

	// Check for our custom fields
	userdata, hasUserdata := params[FieldCloudInitUserdata].(string)
	storageID, hasStorage := params[FieldCloudInitStorage].(string)

	if hasUserdata {
		if !hasStorage {
			return nil, fmt.Errorf("%s is required when %s is provided", FieldCloudInitStorage, FieldCloudInitUserdata)
		}

		// Write the snippet
		volumeID, err := writer.Write(storageID, vmid, userdata)
		if err != nil {
			return nil, fmt.Errorf("failed to write cloud-init snippet: %w", err)
		}

		result.SnippetVolumeID = volumeID
		result.StorageID = storageID
		result.CreatedSnippet = true

		// Remove our custom fields
		delete(params, FieldCloudInitUserdata)
		delete(params, FieldCloudInitStorage)

		// Add or merge cicustom parameter
		if existing, ok := params["cicustom"].(string); ok {
			// Merge with existing cicustom (don't override user= if already set)
			if !strings.Contains(existing, "user=") {
				params["cicustom"] = fmt.Sprintf("user=%s,%s", volumeID, existing)
			}
		} else {
			params["cicustom"] = fmt.Sprintf("user=%s", volumeID)
		}
	}

	// Re-encode the body
	if strings.Contains(contentType, "application/json") {
		encoded, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("failed to encode JSON: %w", err)
		}
		result.Body = string(encoded)
		result.ContentType = "application/json"
	} else {
		result.Body = encodeFormURLEncoded(params)
		result.ContentType = "application/x-www-form-urlencoded"
	}

	return result, nil
}

// TransformCloneVM transforms a VM clone request.
// For clones, we need to handle cloud-init differently - the snippet is applied
// after the clone via a config update.
func TransformCloneVM(body []byte, contentType string, sourceVMID int) (*TransformResult, error) {
	result := &TransformResult{}

	var params map[string]interface{}
	var err error

	if strings.Contains(contentType, "application/json") {
		params, err = parseJSON(body)
	} else {
		params, err = parseFormURLEncoded(body)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to parse request body: %w", err)
	}

	// For clone, the new VMID is in 'newid' parameter
	newid, err := extractNewVMID(params)
	if err != nil {
		return nil, err
	}
	result.VMID = newid

	// Check for our custom fields - store them for post-clone config
	_, hasUserdata := params[FieldCloudInitUserdata].(string)
	_, hasStorage := params[FieldCloudInitStorage].(string)

	if hasUserdata && !hasStorage {
		return nil, fmt.Errorf("%s is required when %s is provided", FieldCloudInitStorage, FieldCloudInitUserdata)
	}

	// For clones, we don't write the snippet yet - we'll do it after the clone succeeds
	// Just remove our custom fields from the clone request
	delete(params, FieldCloudInitUserdata)
	delete(params, FieldCloudInitStorage)

	// Re-encode
	if strings.Contains(contentType, "application/json") {
		encoded, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("failed to encode JSON: %w", err)
		}
		result.Body = string(encoded)
		result.ContentType = "application/json"
	} else {
		result.Body = encodeFormURLEncoded(params)
		result.ContentType = "application/x-www-form-urlencoded"
	}

	return result, nil
}

func parseJSON(body []byte) (map[string]interface{}, error) {
	var params map[string]interface{}
	if err := json.Unmarshal(body, &params); err != nil {
		return nil, err
	}
	return params, nil
}

func parseFormURLEncoded(body []byte) (map[string]interface{}, error) {
	values, err := url.ParseQuery(string(body))
	if err != nil {
		return nil, err
	}

	params := make(map[string]interface{})
	for key, vals := range values {
		if len(vals) == 1 {
			params[key] = vals[0]
		} else {
			params[key] = vals
		}
	}
	return params, nil
}

func encodeFormURLEncoded(params map[string]interface{}) string {
	values := url.Values{}
	for key, val := range params {
		switch v := val.(type) {
		case string:
			values.Set(key, v)
		case []string:
			for _, s := range v {
				values.Add(key, s)
			}
		case float64:
			values.Set(key, strconv.FormatFloat(v, 'f', -1, 64))
		case int:
			values.Set(key, strconv.Itoa(v))
		case bool:
			if v {
				values.Set(key, "1")
			} else {
				values.Set(key, "0")
			}
		default:
			values.Set(key, fmt.Sprintf("%v", v))
		}
	}
	return values.Encode()
}

func extractVMID(params map[string]interface{}) (int, error) {
	vmidVal, ok := params["vmid"]
	if !ok {
		return 0, fmt.Errorf("vmid is required")
	}

	switch v := vmidVal.(type) {
	case float64:
		return int(v), nil
	case int:
		return v, nil
	case string:
		vmid, err := strconv.Atoi(v)
		if err != nil {
			return 0, fmt.Errorf("invalid vmid: %s", v)
		}
		return vmid, nil
	default:
		return 0, fmt.Errorf("invalid vmid type: %T", vmidVal)
	}
}

func extractNewVMID(params map[string]interface{}) (int, error) {
	vmidVal, ok := params["newid"]
	if !ok {
		return 0, fmt.Errorf("newid is required for clone operations")
	}

	switch v := vmidVal.(type) {
	case float64:
		return int(v), nil
	case int:
		return v, nil
	case string:
		vmid, err := strconv.Atoi(v)
		if err != nil {
			return 0, fmt.Errorf("invalid newid: %s", v)
		}
		return vmid, nil
	default:
		return 0, fmt.Errorf("invalid newid type: %T", vmidVal)
	}
}

// Path patterns for VM operations
var (
	createVMPattern = regexp.MustCompile(`^/api2/json/nodes/([^/]+)/qemu/?$`)
	cloneVMPattern  = regexp.MustCompile(`^/api2/json/nodes/([^/]+)/qemu/(\d+)/clone/?$`)
)

// ParsePath extracts operation type and parameters from the request path.
func ParsePath(path string) (operation string, node string, vmid int) {
	if matches := createVMPattern.FindStringSubmatch(path); matches != nil {
		return "create", matches[1], 0
	}
	if matches := cloneVMPattern.FindStringSubmatch(path); matches != nil {
		vmid, _ := strconv.Atoi(matches[2])
		return "clone", matches[1], vmid
	}
	return "passthrough", "", 0
}
