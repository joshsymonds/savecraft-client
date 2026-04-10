//go:build !windows

package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// resolveKnownFolder returns the platform-appropriate path for a known folder.
// On non-Windows platforms, it returns sensible defaults based on XDG/macOS conventions.
// Supported names: DOCUMENTS, SAVED_GAMES, LOCALAPPDATA, LOCALAPPDATA_LOW.
func resolveKnownFolder(name string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve known folder %s: %w", name, err)
	}

	switch name {
	case "DOCUMENTS":
		return filepath.Join(home, "Documents"), nil
	case "SAVED_GAMES":
		return "", fmt.Errorf("resolve known folder %s: no equivalent on %s", name, runtime.GOOS)
	case "LOCALAPPDATA":
		if runtime.GOOS == "darwin" {
			return filepath.Join(home, "Library"), nil
		}
		return filepath.Join(home, ".local", "share"), nil
	case "LOCALAPPDATA_LOW":
		if runtime.GOOS == "darwin" {
			return filepath.Join(home, "Library"), nil
		}
		return filepath.Join(home, ".local", "share"), nil
	default:
		return "", fmt.Errorf("resolve known folder: unknown folder %q", name)
	}
}
