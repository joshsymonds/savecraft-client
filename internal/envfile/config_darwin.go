//go:build darwin

package envfile

import (
	"os"
	"path/filepath"

	"github.com/joshsymonds/savecraft.gg/internal/appname"
)

// ConfigDir returns the macOS configuration directory for the given app.
// Uses ~/Library/Application Support/{AppName}.
func ConfigDir(appName string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", appName)
	}

	return filepath.Join(home, "Library", "Application Support", appname.TitleName(appName))
}
