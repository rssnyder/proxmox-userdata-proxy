package proxy

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rileysndr/proxmox-userdata-proxy/internal/snippet"
)

func setupTestWriter(t *testing.T) (*snippet.Writer, string) {
	tmpDir, err := os.MkdirTemp("", "transform-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	writer := snippet.NewWriter(map[string]string{
		"local":      tmpDir,
		"nfs-shared": tmpDir,
	})
	return writer, tmpDir
}

func TestParsePath(t *testing.T) {
	tests := []struct {
		path      string
		operation string
		node      string
		vmid      int
	}{
		{"/api2/json/nodes/pve/qemu", "create", "pve", 0},
		{"/api2/json/nodes/pve/qemu/", "create", "pve", 0},
		{"/api2/json/nodes/node1/qemu", "create", "node1", 0},
		{"/api2/json/nodes/pve/qemu/100/clone", "clone", "pve", 100},
		{"/api2/json/nodes/pve/qemu/9000/clone/", "clone", "pve", 9000},
		{"/api2/json/nodes/pve/qemu/100/status", "passthrough", "", 0},
		{"/api2/json/nodes/pve/lxc", "passthrough", "", 0},
		{"/api2/json/cluster/resources", "passthrough", "", 0},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			op, node, vmid := ParsePath(tt.path)
			if op != tt.operation {
				t.Errorf("operation: expected '%s', got '%s'", tt.operation, op)
			}
			if node != tt.node {
				t.Errorf("node: expected '%s', got '%s'", tt.node, node)
			}
			if vmid != tt.vmid {
				t.Errorf("vmid: expected %d, got %d", tt.vmid, vmid)
			}
		})
	}
}

func TestTransformCreateVM_WithCloudInit_FormEncoded(t *testing.T) {
	writer, tmpDir := setupTestWriter(t)
	defer os.RemoveAll(tmpDir)

	body := url.Values{}
	body.Set("vmid", "100")
	body.Set("name", "test-vm")
	body.Set("memory", "2048")
	body.Set("cores", "2")
	body.Set("cloudinit_userdata", "#cloud-config\npackages:\n  - nginx\n")
	body.Set("cloudinit_storage", "local")

	result, err := TransformCreateVM([]byte(body.Encode()), "application/x-www-form-urlencoded", writer)
	if err != nil {
		t.Fatalf("TransformCreateVM failed: %v", err)
	}

	if result.VMID != 100 {
		t.Errorf("expected VMID 100, got %d", result.VMID)
	}
	if !result.CreatedSnippet {
		t.Error("expected CreatedSnippet to be true")
	}
	if result.StorageID != "local" {
		t.Errorf("expected StorageID 'local', got '%s'", result.StorageID)
	}
	if result.SnippetVolumeID != "local:snippets/vm-100-cloud-init-user.yaml" {
		t.Errorf("unexpected SnippetVolumeID: %s", result.SnippetVolumeID)
	}

	// Verify body was transformed
	values, _ := url.ParseQuery(result.Body)
	if values.Get("cloudinit_userdata") != "" {
		t.Error("cloudinit_userdata should be removed from body")
	}
	if values.Get("cloudinit_storage") != "" {
		t.Error("cloudinit_storage should be removed from body")
	}
	if !strings.Contains(values.Get("cicustom"), "user=local:snippets/vm-100-cloud-init-user.yaml") {
		t.Errorf("cicustom should be set, got: %s", values.Get("cicustom"))
	}
	if values.Get("vmid") != "100" {
		t.Error("vmid should be preserved")
	}
	if values.Get("name") != "test-vm" {
		t.Error("name should be preserved")
	}

	// Verify snippet file was created
	filePath := filepath.Join(tmpDir, "snippets", "vm-100-cloud-init-user.yaml")
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Error("snippet file should be created")
	}
}

func TestTransformCreateVM_WithCloudInit_JSON(t *testing.T) {
	writer, tmpDir := setupTestWriter(t)
	defer os.RemoveAll(tmpDir)

	body := map[string]interface{}{
		"vmid":               200,
		"name":               "test-vm-json",
		"memory":             4096,
		"cloudinit_userdata": "#cloud-config\npackages:\n  - vim\n",
		"cloudinit_storage":  "local",
	}
	jsonBody, _ := json.Marshal(body)

	result, err := TransformCreateVM(jsonBody, "application/json", writer)
	if err != nil {
		t.Fatalf("TransformCreateVM failed: %v", err)
	}

	if result.VMID != 200 {
		t.Errorf("expected VMID 200, got %d", result.VMID)
	}
	if result.ContentType != "application/json" {
		t.Errorf("expected content-type 'application/json', got '%s'", result.ContentType)
	}

	// Verify JSON body was transformed
	var resultBody map[string]interface{}
	json.Unmarshal([]byte(result.Body), &resultBody)

	if _, exists := resultBody["cloudinit_userdata"]; exists {
		t.Error("cloudinit_userdata should be removed from body")
	}
	if _, exists := resultBody["cloudinit_storage"]; exists {
		t.Error("cloudinit_storage should be removed from body")
	}
	if cicustom, ok := resultBody["cicustom"].(string); !ok || !strings.Contains(cicustom, "user=local:snippets/vm-200-cloud-init-user.yaml") {
		t.Errorf("cicustom should be set, got: %v", resultBody["cicustom"])
	}
}

func TestTransformCreateVM_NoCloudInit(t *testing.T) {
	writer, tmpDir := setupTestWriter(t)
	defer os.RemoveAll(tmpDir)

	body := url.Values{}
	body.Set("vmid", "100")
	body.Set("name", "test-vm")
	body.Set("memory", "2048")

	result, err := TransformCreateVM([]byte(body.Encode()), "application/x-www-form-urlencoded", writer)
	if err != nil {
		t.Fatalf("TransformCreateVM failed: %v", err)
	}

	if result.CreatedSnippet {
		t.Error("expected CreatedSnippet to be false")
	}
	if result.SnippetVolumeID != "" {
		t.Error("expected empty SnippetVolumeID")
	}

	// Body should be unchanged (except re-encoding)
	values, _ := url.ParseQuery(result.Body)
	if values.Get("vmid") != "100" {
		t.Error("vmid should be preserved")
	}
}

func TestTransformCreateVM_MissingStorage(t *testing.T) {
	writer, tmpDir := setupTestWriter(t)
	defer os.RemoveAll(tmpDir)

	body := url.Values{}
	body.Set("vmid", "100")
	body.Set("cloudinit_userdata", "#cloud-config\npackages:\n  - nginx\n")
	// Missing cloudinit_storage

	_, err := TransformCreateVM([]byte(body.Encode()), "application/x-www-form-urlencoded", writer)
	if err == nil {
		t.Fatal("expected error for missing cloudinit_storage")
	}
	if !strings.Contains(err.Error(), "cloudinit_storage is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestTransformCreateVM_MissingVMID(t *testing.T) {
	writer, tmpDir := setupTestWriter(t)
	defer os.RemoveAll(tmpDir)

	body := url.Values{}
	body.Set("name", "test-vm")
	// Missing vmid

	_, err := TransformCreateVM([]byte(body.Encode()), "application/x-www-form-urlencoded", writer)
	if err == nil {
		t.Fatal("expected error for missing vmid")
	}
}

func TestTransformCreateVM_UnknownStorage(t *testing.T) {
	writer, tmpDir := setupTestWriter(t)
	defer os.RemoveAll(tmpDir)

	body := url.Values{}
	body.Set("vmid", "100")
	body.Set("cloudinit_userdata", "#cloud-config\npackages:\n  - nginx\n")
	body.Set("cloudinit_storage", "nonexistent-storage")

	_, err := TransformCreateVM([]byte(body.Encode()), "application/x-www-form-urlencoded", writer)
	if err == nil {
		t.Fatal("expected error for unknown storage")
	}
	if !strings.Contains(err.Error(), "unknown storage ID") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestTransformCreateVM_PreservesExistingCicustom(t *testing.T) {
	writer, tmpDir := setupTestWriter(t)
	defer os.RemoveAll(tmpDir)

	body := url.Values{}
	body.Set("vmid", "100")
	body.Set("cicustom", "network=local:snippets/network.yaml")
	body.Set("cloudinit_userdata", "#cloud-config\npackages:\n  - nginx\n")
	body.Set("cloudinit_storage", "local")

	result, err := TransformCreateVM([]byte(body.Encode()), "application/x-www-form-urlencoded", writer)
	if err != nil {
		t.Fatalf("TransformCreateVM failed: %v", err)
	}

	values, _ := url.ParseQuery(result.Body)
	cicustom := values.Get("cicustom")

	// Should have both user and network
	if !strings.Contains(cicustom, "user=local:snippets/vm-100-cloud-init-user.yaml") {
		t.Errorf("cicustom should contain user snippet, got: %s", cicustom)
	}
	if !strings.Contains(cicustom, "network=local:snippets/network.yaml") {
		t.Errorf("cicustom should preserve network snippet, got: %s", cicustom)
	}
}

func TestTransformCloneVM(t *testing.T) {
	body := url.Values{}
	body.Set("newid", "101")
	body.Set("name", "cloned-vm")
	body.Set("full", "1")
	body.Set("cloudinit_userdata", "#cloud-config\npackages:\n  - nginx\n")
	body.Set("cloudinit_storage", "local")

	result, err := TransformCloneVM([]byte(body.Encode()), "application/x-www-form-urlencoded", 100)
	if err != nil {
		t.Fatalf("TransformCloneVM failed: %v", err)
	}

	if result.VMID != 101 {
		t.Errorf("expected VMID 101, got %d", result.VMID)
	}

	// Cloud-init fields should be removed (they're handled after clone succeeds)
	values, _ := url.ParseQuery(result.Body)
	if values.Get("cloudinit_userdata") != "" {
		t.Error("cloudinit_userdata should be removed")
	}
	if values.Get("cloudinit_storage") != "" {
		t.Error("cloudinit_storage should be removed")
	}
	if values.Get("newid") != "101" {
		t.Error("newid should be preserved")
	}
}

func TestTransformCloneVM_MissingNewID(t *testing.T) {
	body := url.Values{}
	body.Set("name", "cloned-vm")
	// Missing newid

	_, err := TransformCloneVM([]byte(body.Encode()), "application/x-www-form-urlencoded", 100)
	if err == nil {
		t.Fatal("expected error for missing newid")
	}
}

func TestTransformCloneVM_MissingStorageWithUserdata(t *testing.T) {
	body := url.Values{}
	body.Set("newid", "101")
	body.Set("cloudinit_userdata", "#cloud-config\n")
	// Missing cloudinit_storage

	_, err := TransformCloneVM([]byte(body.Encode()), "application/x-www-form-urlencoded", 100)
	if err == nil {
		t.Fatal("expected error for missing cloudinit_storage")
	}
}

func TestExtractVMID_Types(t *testing.T) {
	tests := []struct {
		name     string
		params   map[string]interface{}
		expected int
		hasError bool
	}{
		{"float64", map[string]interface{}{"vmid": float64(100)}, 100, false},
		{"int", map[string]interface{}{"vmid": 100}, 100, false},
		{"string", map[string]interface{}{"vmid": "100"}, 100, false},
		{"missing", map[string]interface{}{}, 0, true},
		{"invalid string", map[string]interface{}{"vmid": "not-a-number"}, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vmid, err := extractVMID(tt.params)
			if tt.hasError {
				if err == nil {
					t.Error("expected error")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if vmid != tt.expected {
					t.Errorf("expected %d, got %d", tt.expected, vmid)
				}
			}
		})
	}
}
