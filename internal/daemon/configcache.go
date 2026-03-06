package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const configCacheFile = "config_cache.json"

// defaultConfigDir returns the default config cache directory.
// Uses os.UserConfigDir()/savecraft.
func defaultConfigDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "savecraft")
}

// saveConfigCache writes the game config map to configDir/config_cache.json.
func saveConfigCache(configDir string, games map[string]GameConfig) error {
	if configDir == "" {
		return nil
	}
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := json.MarshalIndent(games, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	path := filepath.Join(configDir, configCacheFile)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write config cache: %w", err)
	}
	return nil
}

// loadConfigCache reads game config from configDir/config_cache.json.
// Returns nil map and nil error if the file doesn't exist or is corrupt.
func loadConfigCache(configDir string) (map[string]GameConfig, error) {
	if configDir == "" {
		return nil, nil
	}
	path := filepath.Join(configDir, configCacheFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil //nolint:nilerr // missing/unreadable cache is not an error
	}
	var games map[string]GameConfig
	if err := json.Unmarshal(data, &games); err != nil {
		return nil, nil //nolint:nilerr // corrupt cache is not an error
	}
	return games, nil
}
