package snippet

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

// Writer handles writing cloud-init snippets to the filesystem.
type Writer struct {
	storageMap map[string]string
}

// NewWriter creates a new snippet writer with the given storage mappings.
func NewWriter(storageMap map[string]string) *Writer {
	return &Writer{
		storageMap: storageMap,
	}
}

// Filename generates a consistent snippet filename for a VM.
func Filename(vmid int) string {
	return fmt.Sprintf("vm-%d-cloud-init-user.yaml", vmid)
}

// VolumeID returns the Proxmox volume ID for a snippet.
func VolumeID(storageID string, vmid int) string {
	return fmt.Sprintf("%s:snippets/%s", storageID, Filename(vmid))
}

// Write writes the cloud-init content to the appropriate storage location.
// Returns the volume ID that can be used in cicustom.
func (w *Writer) Write(storageID string, vmid int, content string) (string, error) {
	basePath, ok := w.storageMap[storageID]
	if !ok {
		return "", fmt.Errorf("unknown storage ID: %s (configured storages: %v)", storageID, w.availableStorages())
	}

	snippetsDir := filepath.Join(basePath, "snippets")

	// Ensure snippets directory exists
	if err := os.MkdirAll(snippetsDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create snippets directory %s: %w", snippetsDir, err)
	}

	filename := Filename(vmid)
	fullPath := filepath.Join(snippetsDir, filename)

	// Validate the content looks like valid cloud-init
	if err := validateCloudInit(content); err != nil {
		return "", fmt.Errorf("invalid cloud-init content: %w", err)
	}

	// Write the file
	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("failed to write snippet file %s: %w", fullPath, err)
	}

	return VolumeID(storageID, vmid), nil
}

// Delete removes a snippet file.
func (w *Writer) Delete(storageID string, vmid int) error {
	basePath, ok := w.storageMap[storageID]
	if !ok {
		return fmt.Errorf("unknown storage ID: %s", storageID)
	}

	fullPath := filepath.Join(basePath, "snippets", Filename(vmid))

	if err := os.Remove(fullPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete snippet file %s: %w", fullPath, err)
	}

	return nil
}

// Exists checks if a snippet file exists.
func (w *Writer) Exists(storageID string, vmid int) bool {
	basePath, ok := w.storageMap[storageID]
	if !ok {
		return false
	}

	fullPath := filepath.Join(basePath, "snippets", Filename(vmid))
	_, err := os.Stat(fullPath)
	return err == nil
}

func (w *Writer) availableStorages() []string {
	storages := make([]string, 0, len(w.storageMap))
	for k := range w.storageMap {
		storages = append(storages, k)
	}
	return storages
}

// validateCloudInit performs basic validation on cloud-init content.
func validateCloudInit(content string) error {
	if content == "" {
		return fmt.Errorf("content is empty")
	}

	// Check for cloud-config header or valid YAML structure
	// Cloud-init files should start with #cloud-config or be valid YAML
	cloudConfigHeader := regexp.MustCompile(`^#cloud-config\s*\n`)
	if !cloudConfigHeader.MatchString(content) {
		// If no header, check if it looks like YAML (starts with a key or list)
		yamlStart := regexp.MustCompile(`^(\s*[a-zA-Z_][a-zA-Z0-9_]*\s*:|^\s*-\s)`)
		if !yamlStart.MatchString(content) {
			return fmt.Errorf("content does not appear to be valid cloud-init (missing #cloud-config header or valid YAML structure)")
		}
	}

	return nil
}
