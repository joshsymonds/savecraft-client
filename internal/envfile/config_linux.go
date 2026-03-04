//go:build !darwin && !windows

package envfile

import (
	"os"
	"path/filepath"
)

// ConfigDir returns the Linux configuration directory for the given app.
// Uses $XDG_CONFIG_HOME/{appName} or falls back to ~/.config/{appName}.
func ConfigDir(appName string) string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, appName)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", appName)
	}

	return filepath.Join(home, ".config", appName)
}
