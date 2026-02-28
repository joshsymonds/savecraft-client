//go:build !darwin && !windows

package envfile

import (
	"os"
	"path/filepath"
)

// ConfigDir returns the Linux configuration directory for savecraft.
// Uses $XDG_CONFIG_HOME/savecraft or falls back to ~/.config/savecraft.
func ConfigDir() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "savecraft")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", "savecraft")
	}

	return filepath.Join(home, ".config", "savecraft")
}
