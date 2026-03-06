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
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := json.MarshalIndent(games, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	path := filepath.Join(configDir, configCacheFile)
	if writeErr := os.WriteFile(path, data, 0o600); writeErr != nil {
		return fmt.Errorf("write config cache: %w", writeErr)
	}
	return nil
}

// loadConfigCache reads game config from configDir/config_cache.json.
// Returns nil if the file doesn't exist, is unreadable, or is corrupt.
func loadConfigCache(configDir string) map[string]GameConfig {
	if configDir == "" {
		return nil
	}
	path := filepath.Join(configDir, configCacheFile)
	data, err := os.ReadFile(path) //#nosec G304 -- path is built from os.UserConfigDir + constant filename
	if err != nil {
		return nil
	}
	var games map[string]GameConfig
	if unmarshalErr := json.Unmarshal(data, &games); unmarshalErr != nil {
		return nil
	}
	return games
}
