package cmd

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/joshsymonds/savecraft.gg/internal/daemon"
	"github.com/joshsymonds/savecraft.gg/internal/envfile"
	"github.com/joshsymonds/savecraft.gg/internal/osfs"
	"github.com/joshsymonds/savecraft.gg/internal/pluginmgr"
	"github.com/joshsymonds/savecraft.gg/internal/push"
	"github.com/joshsymonds/savecraft.gg/internal/runner"
	"github.com/joshsymonds/savecraft.gg/internal/selfupdate"
	"github.com/joshsymonds/savecraft.gg/internal/signing"
	"github.com/joshsymonds/savecraft.gg/internal/watcher"
	"github.com/joshsymonds/savecraft.gg/internal/wsconn"
)

func buildRunFunc(serverURLDefault, installURLDefault string) func(cmd *cobra.Command, args []string) error {
	return func(_ *cobra.Command, _ []string) error {
		return runDaemon(serverURLDefault, installURLDefault)
	}
}

func runDaemon(serverURLDefault, installURLDefault string) error {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	// Load env file values first (env vars override).
	loadEnvFileDefaults()

	cfg, err := loadConfig(serverURLDefault, installURLDefault)
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

	subsystems, err := createSubsystems(ctx, cfg, logger)
	if err != nil {
		return err
	}
	defer subsystems.close(ctx, logger)

	dmn := daemon.New(cfg.Daemon, subsystems.fsys, subsystems.watcher, subsystems.runner,
		subsystems.pusher, subsystems.ws, subsystems.plugins, subsystems.updater)

	logger.Info("starting daemon",
		slog.String("server", cfg.ServerURL),
		slog.String("install", cfg.InstallURL),
		slog.String("device_id", cfg.Daemon.DeviceID),
		slog.Int("games", len(cfg.Daemon.Games)),
	)

	if runErr := dmn.Run(ctx); runErr != nil {
		return fmt.Errorf("daemon run: %w", runErr)
	}

	return nil
}

type subsystems struct {
	fsys    *osfs.OSFS
	watcher *watcher.FSWatcher
	runner  *runner.WazeroRunner
	plugins *pluginmgr.Manager
	updater *selfupdate.HTTPUpdater
	pusher  *push.Client
	ws      *wsconn.Client
}

func (s *subsystems) close(ctx context.Context, logger *slog.Logger) {
	if closeErr := s.runner.Close(ctx); closeErr != nil {
		logger.ErrorContext(ctx, "close runner", slog.String("error", closeErr.Error()))
	}

	if closeErr := s.watcher.Close(); closeErr != nil {
		logger.ErrorContext(ctx, "close watcher", slog.String("error", closeErr.Error()))
	}
}

func createSubsystems(ctx context.Context, cfg *appConfig, logger *slog.Logger) (*subsystems, error) {
	fsys := osfs.New()

	wt, err := watcher.New()
	if err != nil {
		return nil, fmt.Errorf("create watcher: %w", err)
	}

	skipVerify := os.Getenv("SAVECRAFT_SKIP_VERIFY") != ""

	var opts []runner.Option
	if skipVerify {
		logger.WarnContext(ctx, "plugin signature verification disabled via SAVECRAFT_SKIP_VERIFY")
	} else {
		opts = append(opts, runner.WithVerifier(signing.PublicKey()))
	}

	wr, err := runner.NewWazeroRunner(ctx, opts...)
	if err != nil {
		wt.Close()

		return nil, fmt.Errorf("create runner: %w", err)
	}

	var pubKey ed25519.PublicKey
	if !skipVerify {
		pubKey = signing.PublicKey()
	}

	cacheDir := pluginmgr.DefaultCacheDir()
	cache := pluginmgr.NewCache(cacheDir)
	reg := pluginmgr.NewHTTPRegistry(cfg.ServerURL, cfg.AuthToken)

	mgr := pluginmgr.NewManager(reg, cache, wr, pubKey, logger)
	if cfg.PluginDir != "" {
		mgr.SetLocalDir(cfg.PluginDir)
	}

	updateCacheDir := filepath.Join(cacheDir, "updates")
	updater := selfupdate.New(cfg.InstallURL, pubKey, updateCacheDir)

	pusher, err := push.New(cfg.ServerURL, cfg.AuthToken)
	if err != nil {
		wr.Close(ctx)
		wt.Close()

		return nil, fmt.Errorf("create push client: %w", err)
	}

	wsURL := cfg.ServerURL + "/ws/daemon"
	ws := wsconn.New(wsURL, cfg.AuthToken)

	return &subsystems{
		fsys:    fsys,
		watcher: wt,
		runner:  wr,
		plugins: mgr,
		updater: updater,
		pusher:  pusher,
		ws:      ws,
	}, nil
}

// appConfig holds bootstrap configuration loaded from the environment.
type appConfig struct {
	ServerURL  string
	InstallURL string
	AuthToken  string `json:"-"`
	PluginDir  string
	Daemon     daemon.Config
}

func loadConfig(serverURLDefault, installURLDefault string) (*appConfig, error) {
	serverURL := os.Getenv("SAVECRAFT_SERVER_URL")
	if serverURL == "" {
		serverURL = serverURLDefault
	}
	if serverURL == "" {
		return nil, fmt.Errorf("SAVECRAFT_SERVER_URL is required (set in env or run 'savecraftd pair' first)")
	}

	installURL := os.Getenv("SAVECRAFT_INSTALL_URL")
	if installURL == "" {
		installURL = installURLDefault
	}

	authToken := os.Getenv("SAVECRAFT_AUTH_TOKEN")
	if authToken == "" {
		return nil, fmt.Errorf("SAVECRAFT_AUTH_TOKEN is required (set in env or run 'savecraftd pair' first)")
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
		ServerURL:  serverURL,
		InstallURL: installURL,
		AuthToken:  authToken,
		PluginDir:  pluginDir,
		Daemon: daemon.Config{
			ServerURL: serverURL,
			AuthToken: authToken,
			DeviceID:  deviceID,
			Version:   cfgVersion,
			Games:     make(map[string]daemon.GameConfig),
		},
	}, nil
}

// loadEnvFileDefaults reads the env file and sets environment variables
// for any keys not already set. This allows 'savecraftd pair' to write
// credentials that 'savecraftd run' picks up automatically.
func loadEnvFileDefaults() {
	loadEnvFileDefaultsFromPath(envfile.EnvFilePath())
}

func loadEnvFileDefaultsFromPath(path string) {
	vars, err := envfile.Read(path)
	if err != nil {
		return
	}

	for key, value := range vars {
		if os.Getenv(key) == "" {
			os.Setenv(key, value)
		}
	}
}
