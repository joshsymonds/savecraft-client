package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/spf13/cobra"

	"github.com/joshsymonds/savecraft.gg/internal/envfile"
	"github.com/joshsymonds/savecraft.gg/internal/pluginmgr"
	"github.com/joshsymonds/savecraft.gg/internal/regclient"
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

			// Best-effort remote deregistration before local cleanup.
			deregisterSource(appName)

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

// deregisterSource reads credentials from the env file and asks the server
// to delete this source. Failures are logged but never block uninstall.
func deregisterSource(appName string) {
	vars, err := envfile.Read(envfile.EnvFilePath(appName))
	if err != nil {
		return
	}

	serverURL := vars["SAVECRAFT_SERVER_URL"]
	authToken := vars["SAVECRAFT_AUTH_TOKEN"]
	if serverURL == "" || authToken == "" {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	fmt.Println("  Deregistering from server...")
	if err := regclient.Deregister(ctx, serverURL, authToken); err != nil {
		fmt.Printf("  Warning: could not deregister from server: %v\n", err)
		fmt.Println("  (The server will clean this up automatically.)")
	} else {
		fmt.Println("  Deregistered successfully.")
	}
	fmt.Println()
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
