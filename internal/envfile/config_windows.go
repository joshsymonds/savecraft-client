//go:build windows

package envfile

import (
	"os"
	"path/filepath"

	"github.com/joshsymonds/savecraft.gg/internal/appname"
)

// ConfigDir returns the Windows configuration directory for the given app.
// Uses %APPDATA%\{AppName} or falls back to ~/AppData/Roaming/{AppName}.
func ConfigDir(appName string) string {
	if appData := os.Getenv("APPDATA"); appData != "" {
		return filepath.Join(appData, appname.TitleName(appName))
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", appName)
	}

	return filepath.Join(home, "AppData", "Roaming", appname.TitleName(appName))
}
