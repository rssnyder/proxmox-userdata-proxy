package snippet

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFilename(t *testing.T) {
	tests := []struct {
		vmid     int
		expected string
	}{
		{100, "vm-100-cloud-init-user.yaml"},
		{9000, "vm-9000-cloud-init-user.yaml"},
		{1, "vm-1-cloud-init-user.yaml"},
	}

	for _, tt := range tests {
		got := Filename(tt.vmid)
		if got != tt.expected {
			t.Errorf("Filename(%d) = %s, expected %s", tt.vmid, got, tt.expected)
		}
	}
}

func TestVolumeID(t *testing.T) {
	tests := []struct {
		storageID string
		vmid      int
		expected  string
	}{
		{"local", 100, "local:snippets/vm-100-cloud-init-user.yaml"},
		{"nfs-shared", 9000, "nfs-shared:snippets/vm-9000-cloud-init-user.yaml"},
	}

	for _, tt := range tests {
		got := VolumeID(tt.storageID, tt.vmid)
		if got != tt.expected {
			t.Errorf("VolumeID(%s, %d) = %s, expected %s", tt.storageID, tt.vmid, got, tt.expected)
		}
	}
}

func TestWriter_Write(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "snippet-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	writer := NewWriter(map[string]string{
		"local": tmpDir,
	})

	content := "#cloud-config\npackages:\n  - nginx\n"
	volumeID, err := writer.Write("local", 100, content)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	expectedVolumeID := "local:snippets/vm-100-cloud-init-user.yaml"
	if volumeID != expectedVolumeID {
		t.Errorf("expected volumeID '%s', got '%s'", expectedVolumeID, volumeID)
	}

	// Verify file was created
	filePath := filepath.Join(tmpDir, "snippets", "vm-100-cloud-init-user.yaml")
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read written file: %v", err)
	}

	if string(data) != content {
		t.Errorf("file content mismatch: expected '%s', got '%s'", content, string(data))
	}
}

func TestWriter_Write_UnknownStorage(t *testing.T) {
	writer := NewWriter(map[string]string{
		"local": "/tmp/test",
	})

	_, err := writer.Write("nonexistent", 100, "#cloud-config\n")
	if err == nil {
		t.Fatal("expected error for unknown storage")
	}
	if !strings.Contains(err.Error(), "unknown storage ID") {
		t.Errorf("expected 'unknown storage ID' error, got: %v", err)
	}
}

func TestWriter_Write_InvalidCloudInit(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "snippet-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	writer := NewWriter(map[string]string{
		"local": tmpDir,
	})

	tests := []struct {
		name    string
		content string
	}{
		{"empty", ""},
		{"invalid content", "this is not yaml or cloud-config"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := writer.Write("local", 100, tt.content)
			if err == nil {
				t.Error("expected error for invalid cloud-init content")
			}
		})
	}
}

func TestWriter_Write_ValidFormats(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "snippet-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	writer := NewWriter(map[string]string{
		"local": tmpDir,
	})

	tests := []struct {
		name    string
		content string
	}{
		{"cloud-config header", "#cloud-config\npackages:\n  - nginx\n"},
		{"yaml without header", "packages:\n  - nginx\n"},
		{"yaml list", "- item1\n- item2\n"},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vmid := 100 + i
			_, err := writer.Write("local", vmid, tt.content)
			if err != nil {
				t.Errorf("expected success for valid content, got error: %v", err)
			}
		})
	}
}

func TestWriter_Delete(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "snippet-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	writer := NewWriter(map[string]string{
		"local": tmpDir,
	})

	// Write a file first
	content := "#cloud-config\npackages:\n  - nginx\n"
	_, err = writer.Write("local", 100, content)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Verify file exists
	filePath := filepath.Join(tmpDir, "snippets", "vm-100-cloud-init-user.yaml")
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Fatal("file should exist before delete")
	}

	// Delete
	err = writer.Delete("local", 100)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify file is gone
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Error("file should not exist after delete")
	}
}

func TestWriter_Delete_NonExistent(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "snippet-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	writer := NewWriter(map[string]string{
		"local": tmpDir,
	})

	// Deleting a non-existent file should not error
	err = writer.Delete("local", 99999)
	if err != nil {
		t.Errorf("Delete of non-existent file should not error, got: %v", err)
	}
}

func TestWriter_Delete_UnknownStorage(t *testing.T) {
	writer := NewWriter(map[string]string{
		"local": "/tmp/test",
	})

	err := writer.Delete("nonexistent", 100)
	if err == nil {
		t.Fatal("expected error for unknown storage")
	}
}

func TestWriter_Exists(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "snippet-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	writer := NewWriter(map[string]string{
		"local": tmpDir,
	})

	// Should not exist initially
	if writer.Exists("local", 100) {
		t.Error("file should not exist initially")
	}

	// Write file
	content := "#cloud-config\npackages:\n  - nginx\n"
	_, err = writer.Write("local", 100, content)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Should exist now
	if !writer.Exists("local", 100) {
		t.Error("file should exist after write")
	}

	// Unknown storage should return false
	if writer.Exists("nonexistent", 100) {
		t.Error("unknown storage should return false")
	}
}

func TestWriter_CreatesSnippetsDirectory(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "snippet-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Use a subdirectory that doesn't exist yet
	storageDir := filepath.Join(tmpDir, "newdir")

	writer := NewWriter(map[string]string{
		"local": storageDir,
	})

	content := "#cloud-config\npackages:\n  - nginx\n"
	_, err = writer.Write("local", 100, content)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Verify snippets directory was created
	snippetsDir := filepath.Join(storageDir, "snippets")
	info, err := os.Stat(snippetsDir)
	if err != nil {
		t.Fatalf("snippets directory should exist: %v", err)
	}
	if !info.IsDir() {
		t.Error("snippets should be a directory")
	}
}
