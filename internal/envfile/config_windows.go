//go:build windows

package envfile

import (
	"os"
	"path/filepath"
)

// ConfigDir returns the Windows configuration directory for the given app.
// Uses %APPDATA%\{AppName} or falls back to ~/AppData/Roaming/{AppName}.
func ConfigDir(appName string) string {
	if appData := os.Getenv("APPDATA"); appData != "" {
		return filepath.Join(appData, titleName(appName))
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", appName)
	}

	return filepath.Join(home, "AppData", "Roaming", titleName(appName))
}
