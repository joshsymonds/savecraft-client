package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/joshsymonds/savecraft.gg/internal/envfile"
	"github.com/joshsymonds/savecraft.gg/internal/pluginmgr"
	pb "github.com/joshsymonds/savecraft.gg/internal/proto/savecraft/v1"
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

// deregisterSource reads credentials from the env file and sends a
// DeregisterSource proto over WebSocket. Failures are logged but never
// block uninstall.
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
	if err := wsDeregister(ctx, serverURL, authToken); err != nil {
		fmt.Printf("  Warning: could not deregister from server: %v\n", err)
		fmt.Println("  (The server will clean this up automatically.)")
	} else {
		fmt.Println("  Deregistered successfully.")
	}
	fmt.Println()
}

// wsDeregister connects to /ws/daemon, sends SourceOnline + DeregisterSource,
// and waits for the server to close the connection.
func wsDeregister(ctx context.Context, serverURL, authToken string) error {
	wsURL := strings.Replace(serverURL, "http://", "ws://", 1)
	wsURL = strings.Replace(wsURL, "https://", "wss://", 1)
	wsURL += "/ws/daemon"

	conn, resp, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{
			"Authorization": {"Bearer " + authToken},
		},
	})
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		return fmt.Errorf("dial %s: %w", wsURL, err)
	}
	defer conn.CloseNow()

	// Send SourceOnline so the DO recognizes this connection.
	onlineMsg := &pb.Message{Payload: &pb.Message_SourceOnline{SourceOnline: &pb.SourceOnline{
		Version:   "uninstall",
		Platform:  runtime.GOOS + "-" + runtime.GOARCH,
		Os:        runtime.GOOS,
		Arch:      runtime.GOARCH,
		Timestamp: timestamppb.Now(),
	}}}
	onlineData, marshalErr := proto.Marshal(onlineMsg)
	if marshalErr != nil {
		return fmt.Errorf("marshal source online: %w", marshalErr)
	}
	if writeErr := conn.Write(ctx, websocket.MessageBinary, onlineData); writeErr != nil {
		return fmt.Errorf("send source online: %w", writeErr)
	}

	// Send DeregisterSource — the server will clean up and close the connection.
	deregMsg := &pb.Message{Payload: &pb.Message_DeregisterSource{DeregisterSource: &pb.DeregisterSource{}}}
	deregData, marshalErr := proto.Marshal(deregMsg)
	if marshalErr != nil {
		return fmt.Errorf("marshal deregister: %w", marshalErr)
	}
	if writeErr := conn.Write(ctx, websocket.MessageBinary, deregData); writeErr != nil {
		return fmt.Errorf("send deregister: %w", writeErr)
	}

	// Wait for the server to close the connection (confirms cleanup).
	_, _, readErr := conn.Read(ctx)
	if readErr != nil {
		// Expected: server closes the connection after deregistration.
		// websocket.CloseError with StatusNormalClosure or StatusGoingAway is success.
		var closeErr websocket.CloseError
		if errors.As(readErr, &closeErr) {
			return nil
		}
		// Any other read error also means the connection is closed — acceptable.
		return nil
	}

	return nil
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
