//go:build windows

package envfile

import (
	"os"
	"path/filepath"
)

// ConfigDir returns the Windows configuration directory for savecraft.
// Uses %APPDATA%\Savecraft or falls back to ~/AppData/Roaming/Savecraft.
func ConfigDir() string {
	if appData := os.Getenv("APPDATA"); appData != "" {
		return filepath.Join(appData, "Savecraft")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", "savecraft")
	}

	return filepath.Join(home, "AppData", "Roaming", "Savecraft")
}
