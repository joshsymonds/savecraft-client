package cmd

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/joshsymonds/savecraft.gg/internal/daemon"
	"github.com/joshsymonds/savecraft.gg/internal/envfile"
	"github.com/joshsymonds/savecraft.gg/internal/osfs"
	"github.com/joshsymonds/savecraft.gg/internal/pluginmgr"
	"github.com/joshsymonds/savecraft.gg/internal/push"
	"github.com/joshsymonds/savecraft.gg/internal/regclient"
	"github.com/joshsymonds/savecraft.gg/internal/runner"
	"github.com/joshsymonds/savecraft.gg/internal/selfupdate"
	"github.com/joshsymonds/savecraft.gg/internal/signing"
	"github.com/joshsymonds/savecraft.gg/internal/watcher"
	"github.com/joshsymonds/savecraft.gg/internal/wsconn"
)

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

func createSubsystems(ctx context.Context, cfg *appConfig, appName string, logger *slog.Logger) (*subsystems, error) {
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

	cacheDir := pluginmgr.DefaultCacheDir(appName)
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
	ws := wsconn.New(wsURL, cfg.AuthToken, wsconn.WithLogger(logger))

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
		return nil, fmt.Errorf("SAVECRAFT_SERVER_URL is required")
	}

	installURL := os.Getenv("SAVECRAFT_INSTALL_URL")
	if installURL == "" {
		installURL = installURLDefault
	}
	if installURL == "" {
		return nil, fmt.Errorf("SAVECRAFT_INSTALL_URL is required")
	}

	authToken := os.Getenv("SAVECRAFT_AUTH_TOKEN")

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

	deviceUUID := os.Getenv("SAVECRAFT_DEVICE_UUID")

	return &appConfig{
		ServerURL:  serverURL,
		InstallURL: installURL,
		AuthToken:  authToken,
		PluginDir:  pluginDir,
		Daemon: daemon.Config{
			ServerURL:  serverURL,
			AuthToken:  authToken,
			DeviceID:   deviceID,
			DeviceUUID: deviceUUID,
			Version:    cfgVersion,
			Games:      make(map[string]daemon.GameConfig),
		},
	}, nil
}

// daemonConfigDefaults creates a daemon.Config with minimal defaults.
func daemonConfigDefaults(deviceID, version string) daemon.Config {
	return daemon.Config{
		DeviceID: deviceID,
		Version:  version,
		Games:    make(map[string]daemon.GameConfig),
	}
}

// autoRegister checks if the daemon has credentials. If not, it calls the
// device registration endpoint, persists the credentials to the env file,
// and updates the config in place. Returns (result, true, nil) if registration
// happened, or (nil, false, nil) if credentials already exist.
func autoRegister(ctx context.Context, cfg *appConfig, envPath string) (*regclient.RegisterResult, bool, error) {
	if cfg.AuthToken != "" {
		return nil, false, nil
	}

	result, err := regclient.Register(ctx, cfg.ServerURL, cfg.Daemon.DeviceID)
	if err != nil {
		return nil, false, fmt.Errorf("device registration: %w", err)
	}

	// Persist credentials.
	if writeErr := envfile.Write(envPath, map[string]string{
		"SAVECRAFT_AUTH_TOKEN":  result.Token,
		"SAVECRAFT_DEVICE_UUID": result.DeviceUUID,
		"SAVECRAFT_SERVER_URL":  cfg.ServerURL,
	}); writeErr != nil {
		return nil, false, fmt.Errorf("persist credentials: %w", writeErr)
	}

	// Update config in place.
	cfg.AuthToken = result.Token
	cfg.Daemon.AuthToken = result.Token
	cfg.Daemon.DeviceUUID = result.DeviceUUID

	return result, true, nil
}

// loadEnvFileDefaults reads the env file and sets environment variables
// for any keys not already set. Auto-registration writes credentials here
// on first boot; subsequent runs pick them up automatically.
func loadEnvFileDefaults(appName string) {
	loadEnvFileDefaultsFromPath(envfile.EnvFilePath(appName))
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
