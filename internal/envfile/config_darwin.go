//go:build darwin

package envfile

import (
	"os"
	"path/filepath"
)

// ConfigDir returns the macOS configuration directory for savecraft.
// Uses ~/Library/Application Support/Savecraft.
func ConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", "savecraft")
	}

	return filepath.Join(home, "Library", "Application Support", "Savecraft")
}
