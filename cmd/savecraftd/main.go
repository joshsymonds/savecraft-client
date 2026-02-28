// Package main is the entrypoint for the savecraft daemon.
package main

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/joshsymonds/savecraft.gg/internal/daemon"
	"github.com/joshsymonds/savecraft.gg/internal/osfs"
	"github.com/joshsymonds/savecraft.gg/internal/pluginmgr"
	"github.com/joshsymonds/savecraft.gg/internal/push"
	"github.com/joshsymonds/savecraft.gg/internal/runner"
	"github.com/joshsymonds/savecraft.gg/internal/selfupdate"
	"github.com/joshsymonds/savecraft.gg/internal/signing"
	"github.com/joshsymonds/savecraft.gg/internal/watcher"
	"github.com/joshsymonds/savecraft.gg/internal/wsconn"
)

var version = "dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println("savecraft-daemon " + version)
		return
	}

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

	binaryPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}
	cfg.Daemon.BinaryPath = binaryPath

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

	var opts []runner.Option
	skipVerify := os.Getenv("SAVECRAFT_SKIP_VERIFY") != ""
	if skipVerify {
		logger.Warn("plugin signature verification disabled via SAVECRAFT_SKIP_VERIFY")
	} else {
		opts = append(opts, runner.WithVerifier(signing.PublicKey()))
	}

	wr, err := runner.NewWazeroRunner(ctx, opts...)
	if err != nil {
		return fmt.Errorf("create runner: %w", err)
	}
	defer func() {
		if closeErr := wr.Close(ctx); closeErr != nil {
			logger.Error("close runner", slog.String("error", closeErr.Error()))
		}
	}()

	cacheDir := pluginmgr.DefaultCacheDir()
	cache := pluginmgr.NewCache(cacheDir)
	reg := pluginmgr.NewHTTPRegistry(cfg.ServerURL, cfg.AuthToken)

	var pubKey ed25519.PublicKey
	if !skipVerify {
		pubKey = signing.PublicKey()
	}

	mgr := pluginmgr.NewManager(reg, cache, wr, pubKey, logger)
	if cfg.PluginDir != "" {
		mgr.SetLocalDir(cfg.PluginDir)
	}

	updateCacheDir := filepath.Join(pluginmgr.DefaultCacheDir(), "updates")
	updater := selfupdate.New(cfg.ServerURL, cfg.AuthToken, pubKey, updateCacheDir)

	pusher, err := push.New(cfg.ServerURL, cfg.AuthToken)
	if err != nil {
		return fmt.Errorf("create push client: %w", err)
	}

	wsURL := cfg.ServerURL + "/ws/daemon"
	ws := wsconn.New(wsURL, cfg.AuthToken)

	dmn := daemon.New(cfg.Daemon, fsys, wt, wr, pusher, ws, mgr, updater)

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

	cfgVersion := os.Getenv("SAVECRAFT_VERSION")
	if cfgVersion == "" {
		cfgVersion = "dev"
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
			Version:   cfgVersion,
			Games:     make(map[string]daemon.GameConfig),
		},
	}, nil
}
