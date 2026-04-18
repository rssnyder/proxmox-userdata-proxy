package config

import (
	"os"
	"testing"
)

func TestLoad_AllRequiredFields(t *testing.T) {
	os.Setenv("PROXMOX_URL", "https://proxmox.local:8006")
	os.Setenv("SNIPPET_STORAGE_PATH", "/var/lib/vz")
	defer func() {
		os.Unsetenv("PROXMOX_URL")
		os.Unsetenv("SNIPPET_STORAGE_PATH")
	}()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if cfg.ProxmoxURL != "https://proxmox.local:8006" {
		t.Errorf("expected ProxmoxURL 'https://proxmox.local:8006', got '%s'", cfg.ProxmoxURL)
	}
	if cfg.ListenAddr != ":8443" {
		t.Errorf("expected default ListenAddr ':8443', got '%s'", cfg.ListenAddr)
	}
	if path, ok := cfg.StorageMap["local"]; !ok || path != "/var/lib/vz" {
		t.Errorf("expected StorageMap['local'] = '/var/lib/vz', got '%s'", path)
	}
}

func TestLoad_MissingProxmoxURL(t *testing.T) {
	os.Unsetenv("PROXMOX_URL")
	os.Setenv("SNIPPET_STORAGE_PATH", "/var/lib/vz")
	defer func() {
		os.Unsetenv("SNIPPET_STORAGE_PATH")
	}()

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for missing PROXMOX_URL")
	}
}

func TestLoad_MissingStorageMap(t *testing.T) {
	os.Setenv("PROXMOX_URL", "https://proxmox.local:8006")
	os.Unsetenv("SNIPPET_STORAGE_PATH")
	os.Unsetenv("SNIPPET_STORAGE_MAP")
	defer func() {
		os.Unsetenv("PROXMOX_URL")
	}()

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for missing storage configuration")
	}
}

func TestLoad_StorageMapParsing(t *testing.T) {
	os.Setenv("PROXMOX_URL", "https://proxmox.local:8006")
	os.Setenv("SNIPPET_STORAGE_MAP", "local=/var/lib/vz,nfs-shared=/mnt/pve/nfs,backup=/mnt/backup")
	defer func() {
		os.Unsetenv("PROXMOX_URL")
		os.Unsetenv("SNIPPET_STORAGE_MAP")
	}()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	expected := map[string]string{
		"local":      "/var/lib/vz",
		"nfs-shared": "/mnt/pve/nfs",
		"backup":     "/mnt/backup",
	}

	for storage, path := range expected {
		if got, ok := cfg.StorageMap[storage]; !ok || got != path {
			t.Errorf("expected StorageMap['%s'] = '%s', got '%s' (ok=%v)", storage, path, got, ok)
		}
	}
}

func TestLoad_ProxmoxInsecure(t *testing.T) {
	os.Setenv("PROXMOX_URL", "https://proxmox.local:8006")
	os.Setenv("SNIPPET_STORAGE_PATH", "/var/lib/vz")
	os.Setenv("PROXMOX_INSECURE", "true")
	defer func() {
		os.Unsetenv("PROXMOX_URL")
		os.Unsetenv("SNIPPET_STORAGE_PATH")
		os.Unsetenv("PROXMOX_INSECURE")
	}()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if !cfg.ProxmoxInsecure {
		t.Error("expected ProxmoxInsecure to be true")
	}
}

func TestConfig_TLSEnabled(t *testing.T) {
	tests := []struct {
		name     string
		certFile string
		keyFile  string
		expected bool
	}{
		{"both set", "/path/cert.pem", "/path/key.pem", true},
		{"only cert", "/path/cert.pem", "", false},
		{"only key", "", "/path/key.pem", false},
		{"neither set", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				TLSCertFile: tt.certFile,
				TLSKeyFile:  tt.keyFile,
			}
			if got := cfg.TLSEnabled(); got != tt.expected {
				t.Errorf("TLSEnabled() = %v, expected %v", got, tt.expected)
			}
		})
	}
}

func TestConfig_GetStoragePath(t *testing.T) {
	cfg := &Config{
		StorageMap: map[string]string{
			"local":      "/var/lib/vz",
			"nfs-shared": "/mnt/pve/nfs",
		},
	}

	// Existing storage
	path, ok := cfg.GetStoragePath("local")
	if !ok || path != "/var/lib/vz" {
		t.Errorf("expected '/var/lib/vz', got '%s' (ok=%v)", path, ok)
	}

	// Non-existent storage
	path, ok = cfg.GetStoragePath("nonexistent")
	if ok {
		t.Errorf("expected ok=false for nonexistent storage, got path='%s'", path)
	}
}
