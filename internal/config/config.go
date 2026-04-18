package config

import (
	"fmt"
	"os"
	"strings"
)

type Config struct {
	// Proxmox connection settings
	ProxmoxURL      string
	ProxmoxInsecure bool

	// Proxy settings
	ListenAddr string

	// TLS settings (optional)
	TLSCertFile string
	TLSKeyFile  string

	// Snippet storage settings
	StorageMap map[string]string // storage ID -> filesystem path

	// Logging
	LogLevel string
}

func Load() (*Config, error) {
	cfg := &Config{
		ProxmoxURL:      os.Getenv("PROXMOX_URL"),
		ProxmoxInsecure: os.Getenv("PROXMOX_INSECURE") == "true",
		ListenAddr:      getEnvOrDefault("PROXY_LISTEN_ADDR", ":8443"),
		TLSCertFile:     os.Getenv("TLS_CERT_FILE"),
		TLSKeyFile:      os.Getenv("TLS_KEY_FILE"),
		LogLevel:        getEnvOrDefault("LOG_LEVEL", "info"),
		StorageMap:      make(map[string]string),
	}

	// Parse storage map from environment
	// Format: "local=/var/lib/vz,nfs-shared=/mnt/pve/nfs-shared"
	storageMapStr := os.Getenv("SNIPPET_STORAGE_MAP")
	if storageMapStr != "" {
		for _, mapping := range strings.Split(storageMapStr, ",") {
			parts := strings.SplitN(strings.TrimSpace(mapping), "=", 2)
			if len(parts) == 2 {
				cfg.StorageMap[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
			}
		}
	}

	// Also support single storage path for simple setups
	if singlePath := os.Getenv("SNIPPET_STORAGE_PATH"); singlePath != "" {
		if _, exists := cfg.StorageMap["local"]; !exists {
			cfg.StorageMap["local"] = singlePath
		}
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *Config) Validate() error {
	if c.ProxmoxURL == "" {
		return fmt.Errorf("PROXMOX_URL is required")
	}
	if len(c.StorageMap) == 0 {
		return fmt.Errorf("at least one storage mapping is required (SNIPPET_STORAGE_MAP or SNIPPET_STORAGE_PATH)")
	}
	return nil
}

func (c *Config) TLSEnabled() bool {
	return c.TLSCertFile != "" && c.TLSKeyFile != ""
}

func (c *Config) GetStoragePath(storageID string) (string, bool) {
	path, ok := c.StorageMap[storageID]
	return path, ok
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
