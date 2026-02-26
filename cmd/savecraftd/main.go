// Package main is the entrypoint for the savecraft daemon.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/joshsymonds/savecraft.gg/internal/daemon"
	"github.com/joshsymonds/savecraft.gg/internal/osfs"
	"github.com/joshsymonds/savecraft.gg/internal/push"
	"github.com/joshsymonds/savecraft.gg/internal/runner"
	"github.com/joshsymonds/savecraft.gg/internal/watcher"
	"github.com/joshsymonds/savecraft.gg/internal/wsconn"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	if err := run(logger); err != nil {
		logger.Error("daemon exited with error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	fsys := osfs.New()

	wt, err := watcher.New()
	if err != nil {
		return fmt.Errorf("create watcher: %w", err)
	}
	defer func() {
		if closeErr := wt.Close(); closeErr != nil {
			logger.Error("close watcher", slog.String("error", closeErr.Error()))
		}
	}()

	wr, err := runner.NewWazeroRunner(ctx)
	if err != nil {
		return fmt.Errorf("create runner: %w", err)
	}
	defer func() {
		if closeErr := wr.Close(ctx); closeErr != nil {
			logger.Error("close runner", slog.String("error", closeErr.Error()))
		}
	}()

	if loadErr := loadPlugins(ctx, logger, wr, cfg); loadErr != nil {
		return fmt.Errorf("load plugins: %w", loadErr)
	}

	pusher, err := push.New(cfg.ServerURL, cfg.AuthToken)
	if err != nil {
		return fmt.Errorf("create push client: %w", err)
	}

	wsURL := cfg.ServerURL + "/ws/daemon"
	ws := wsconn.New(wsURL, cfg.AuthToken)

	dmn := daemon.New(cfg.Daemon, fsys, wt, wr, pusher, ws)

	logger.Info("starting daemon",
		slog.String("server", cfg.ServerURL),
		slog.String("device_id", cfg.Daemon.DeviceID),
		slog.Int("games", len(cfg.Daemon.Games)),
	)

	if runErr := dmn.Run(ctx); runErr != nil {
		return fmt.Errorf("daemon run: %w", runErr)
	}
	return nil
}

// appConfig holds bootstrap configuration loaded from the environment.
type appConfig struct {
	ServerURL string
	AuthToken string `json:"-"`
	DeviceID  string
	PluginDir string
	Daemon    daemon.Config
}

func loadConfig() (*appConfig, error) {
	serverURL := os.Getenv("SAVECRAFT_SERVER_URL")
	if serverURL == "" {
		return nil, fmt.Errorf("SAVECRAFT_SERVER_URL is required")
	}

	authToken := os.Getenv("SAVECRAFT_AUTH_TOKEN")
	if authToken == "" {
		return nil, fmt.Errorf("SAVECRAFT_AUTH_TOKEN is required")
	}

	deviceID := os.Getenv("SAVECRAFT_DEVICE_ID")
	if deviceID == "" {
		hostname, err := os.Hostname()
		if err != nil {
			return nil, fmt.Errorf("get hostname for device ID: %w", err)
		}
		deviceID = hostname
	}

	pluginDir := os.Getenv("SAVECRAFT_PLUGIN_DIR")
	if pluginDir == "" {
		pluginDir = "plugins"
	}

	version := os.Getenv("SAVECRAFT_VERSION")
	if version == "" {
		version = "dev"
	}

	return &appConfig{
		ServerURL: serverURL,
		AuthToken: authToken,
		DeviceID:  deviceID,
		PluginDir: pluginDir,
		Daemon: daemon.Config{
			ServerURL: serverURL,
			AuthToken: authToken,
			DeviceID:  deviceID,
			Version:   version,
			Games:     make(map[string]daemon.GameConfig),
		},
	}, nil
}

func loadPlugins(ctx context.Context, logger *slog.Logger, wr *runner.WazeroRunner, cfg *appConfig) error {
	pluginDir := cfg.PluginDir
	entries, err := os.ReadDir(pluginDir)
	if os.IsNotExist(err) {
		logger.WarnContext(ctx, "plugin directory not found, no plugins loaded", slog.String("path", pluginDir))
		return nil
	}
	if err != nil {
		return fmt.Errorf("read plugin dir %s: %w", pluginDir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".wasm" {
			continue
		}

		name := entry.Name()
		gameID := name[:len(name)-len(".wasm")]
		pluginPath := filepath.Join(pluginDir, name)

		wasmBytes, readErr := os.ReadFile(filepath.Clean(pluginPath))
		if readErr != nil {
			return fmt.Errorf("read plugin %s: %w", name, readErr)
		}

		if loadErr := wr.LoadPlugin(ctx, gameID, wasmBytes); loadErr != nil {
			return fmt.Errorf("load plugin %s: %w", name, loadErr)
		}

		logger.InfoContext(ctx, "loaded plugin", slog.String("game_id", gameID), slog.String("file", name))
	}

	return nil
}
