package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/joshsymonds/savecraft.gg/internal/envfile"
	"github.com/joshsymonds/savecraft.gg/internal/pluginmgr"
	"github.com/joshsymonds/savecraft.gg/internal/svcmgr"
)

func buildUninstallCommand(cfg svcmgr.Config, appName string) *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Completely remove the daemon, its service, and all data",
		RunE: func(_ *cobra.Command, _ []string) error {
			paths, err := resolveUninstallPaths(appName)
			if err != nil {
				return fmt.Errorf("resolve paths: %w", err)
			}

			fmt.Println()
			fmt.Println("  Uninstalling Savecraft Daemon")
			fmt.Println("  =============================")
			fmt.Println()

			if uninstallErr := svcmgr.Uninstall(cfg, paths, os.Stdout); uninstallErr != nil {
				return fmt.Errorf("uninstall: %w", uninstallErr)
			}

			fmt.Println()
			fmt.Println("  Done. All Savecraft files have been removed.")
			fmt.Println()

			return nil
		},
	}
}

func resolveUninstallPaths(appName string) (svcmgr.UninstallPaths, error) {
	exePath, err := os.Executable()
	if err != nil {
		return svcmgr.UninstallPaths{}, fmt.Errorf("get executable path: %w", err)
	}

	// Plugin cache returns e.g. ~/.local/share/savecraft/plugins — we want the parent.
	pluginCacheDir := pluginmgr.DefaultCacheDir(appName)
	dataDir := filepath.Dir(pluginCacheDir)

	paths := svcmgr.UninstallPaths{
		ConfigDir: envfile.ConfigDir(appName),
		CacheDir:  cacheDir(appName),
		DataDir:   dataDir,
		Binary:    exePath,
	}

	if runtime.GOOS == "darwin" {
		home, homeErr := os.UserHomeDir()
		if homeErr == nil {
			paths.LogDir = filepath.Join(home, "Library", "Logs", appName)
		}
	}

	return paths, nil
}

// cacheDir returns the XDG cache directory (Linux: ~/.cache/{appName}).
// This is separate from the plugin data directory.
func cacheDir(appName string) string {
	if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
		return filepath.Join(xdg, appName)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	switch runtime.GOOS {
	case "darwin":
		// macOS uses ~/Library/Caches/{AppName} but the daemon doesn't
		// currently write here — config and plugins are in Application Support.
		return ""
	case "windows":
		// Windows cache is in %LOCALAPPDATA% which is already covered by DataDir.
		return ""
	default:
		return filepath.Join(home, ".cache", appName)
	}
}
