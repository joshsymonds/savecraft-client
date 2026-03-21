package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/spf13/cobra"

	"github.com/joshsymonds/savecraft.gg/internal/daemon"
	"github.com/joshsymonds/savecraft.gg/internal/envfile"
	"github.com/joshsymonds/savecraft.gg/internal/localapi"
	"github.com/joshsymonds/savecraft.gg/internal/pluginmgr"
	"github.com/joshsymonds/savecraft.gg/internal/selfupdate"
	"github.com/joshsymonds/savecraft.gg/internal/svcmgr"
)

func buildRunFunc(
	serverURLDefault, installURLDefault, appName, statusPortDefault, frontendURL string,
) func(cmd *cobra.Command, args []string) error {
	return func(_ *cobra.Command, _ []string) error {
		return runDaemon(serverURLDefault, installURLDefault, appName, statusPortDefault, frontendURL)
	}
}

func runDaemon(serverURLDefault, installURLDefault, appName, statusPortDefault, frontendURL string) error {
	ringBuf := localapi.NewRingBuffer(
		localapi.DefaultBufferSize,
		slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}),
	)
	logger := slog.New(ringBuf)
	slog.SetDefault(logger)

	svcCfg := svcmgr.Config{
		Name:        appName + "-daemon",
		DisplayName: "Savecraft Daemon",
		Description: "Syncs game saves to the cloud via Savecraft",
		AppName:     appName,
	}

	var prog *svcmgr.Program
	prog = svcmgr.New(svcCfg, func(ctx context.Context) error {
		return runDaemonLoop(
			ctx, logger, ringBuf, prog.Stop, svcCfg,
			serverURLDefault, installURLDefault, appName, statusPortDefault, frontendURL,
		)
	})

	if err := svcmgr.Run(prog); err != nil {
		return fmt.Errorf("daemon run: %w", err)
	}

	return nil
}

func runDaemonLoop(
	ctx context.Context,
	logger *slog.Logger,
	ringBuf *localapi.RingBuffer,
	shutdownFn func(),
	svcCfg svcmgr.Config,
	serverURLDefault, installURLDefault, appName, statusPortDefault, frontendURL string,
) error {
	loadEnvFileDefaults(appName)

	api := startLocalAPI(statusPortDefault, svcCfg, ringBuf, shutdownFn, logger)
	defer func() {
		shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutCancel()
		if shutdownErr := api.Shutdown(shutCtx); shutdownErr != nil {
			logger.Error("local API server shutdown failed", slog.String("error", shutdownErr.Error()))
		}
	}()

	cfg, err := loadConfig(serverURLDefault, installURLDefault)
	if err != nil {
		api.SetError(err.Error())

		return fmt.Errorf("load config: %w", err)
	}

	// Register if no credentials exist. The daemon connects WS immediately
	// after registration — linking happens asynchronously over the WS connection.
	api.SetState(localapi.StateRegistering)
	envPath := envfile.EnvFilePath(appName)
	regResult, registered, regErr := autoRegister(ctx, cfg, envPath)
	if regErr != nil {
		api.SetError(regErr.Error())

		return fmt.Errorf("auto-register: %w", regErr)
	}

	return runDaemonSubsystems(ctx, cfg, appName, frontendURL, api, regResult, registered, logger)
}

// startLocalAPI creates and starts the local API server.
func startLocalAPI(
	statusPortDefault string,
	svcCfg svcmgr.Config,
	ringBuf *localapi.RingBuffer,
	shutdownFn func(),
	logger *slog.Logger,
) *localapi.Server {
	statusPort := statusPortDefault
	if envPort := os.Getenv("SAVECRAFT_STATUS_PORT"); envPort != "" {
		statusPort = envPort
	}

	api := localapi.NewServer("localhost:"+statusPort, logger)
	api.SetRingBuffer(ringBuf)
	api.SetShutdownFunc(shutdownFn)
	api.SetRestartFunc(func() error {
		return svcmgr.Control(svcCfg, "restart")
	})
	api.Start()

	return api
}

// trayBinaryExt returns the file extension for binaries on the current platform.
func trayBinaryExt() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// runDaemonSubsystems creates subsystems, wires up repair, and runs the daemon.
func runDaemonSubsystems(
	ctx context.Context,
	cfg *appConfig,
	appName, frontendURL string,
	api *localapi.Server,
	regResult *registerResult,
	newlyRegistered bool,
	logger *slog.Logger,
) error {
	binaryPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}

	cfg.Daemon.BinaryPath = binaryPath

	// Derive tray binary path: same directory, predictable name.
	trayPath := filepath.Join(filepath.Dir(binaryPath), "savecraft-tray"+trayBinaryExt())
	if _, statErr := os.Stat(trayPath); statErr == nil {
		cfg.Daemon.TrayBinaryPath = trayPath
	}

	subsystems, err := createSubsystems(ctx, cfg, appName, logger)
	if err != nil {
		return err
	}
	defer subsystems.close(ctx, logger)

	dmn := daemon.New(cfg.Daemon, subsystems.fsys, subsystems.watcher, subsystems.runner,
		subsystems.ws, subsystems.plugins, subsystems.updater, logger)

	// Start plugin watcher for local dev auto-reload when SAVECRAFT_PLUGIN_DIR is set.
	if cfg.PluginDir != "" {
		reloadCh := make(chan string, 10)
		pw, pwErr := pluginmgr.NewPluginWatcher(cfg.PluginDir, func(gameID string) {
			select {
			case reloadCh <- gameID:
			default:
				logger.Warn("plugin reload channel full, skipping", slog.String("game_id", gameID))
			}
		}, pluginmgr.WithWatcherLogger(logger))
		if pwErr != nil {
			logger.WarnContext(ctx, "failed to start plugin watcher", slog.String("error", pwErr.Error()))
		} else {
			dmn.SetPluginReloadCh(reloadCh)
			defer func() {
				if closeErr := pw.Close(); closeErr != nil {
					logger.ErrorContext(ctx, "close plugin watcher", slog.String("error", closeErr.Error()))
				}
			}()
			logger.InfoContext(ctx, "plugin watcher started", slog.String("plugin_dir", cfg.PluginDir))
		}
	}

	// Set restart function for self-update. On Windows, this spawns the new
	// binary before exit. On Linux, systemd handles restart.
	dmn.SetRestartFunc(selfupdate.RestartDaemon)

	// If newly registered, set the initial link code for display.
	if newlyRegistered && regResult != nil {
		applyRegistration(ctx, cfg, dmn, api, regResult, frontendURL, logger)
	}

	// Set up link callbacks so the daemon notifies the local API of state changes.
	dmn.SetLinkCallbacks(daemon.LinkCallbacks{
		OnLinked: func() {
			api.SetState(localapi.StateRunning)
		},
		OnLinkCode: func(code string, expiresAt time.Time) {
			linkURL := localapi.BuildLinkURL(frontendURL, code)
			api.SetRegistered(code, linkURL, expiresAt.Format(time.RFC3339))
		},
	})

	api.Handle("/status", daemon.StatusHandler(dmn))
	// If we're not newly registered, the server will push SourceLinked on connect
	// if already linked. If not linked, the server will push a fresh link code.
	// Either way, the link callbacks above will update the local API state.
	api.SetState(localapi.StateRunning)

	wireRepairCallback(ctx, dmn, frontendURL, api)
	api.SetUpdatePluginsFunc(dmn.UpdatePlugins)
	api.SetPendingVersionFunc(dmn.PendingVersion)

	logger.InfoContext(ctx, "starting daemon",
		slog.String("server", cfg.ServerURL),
		slog.String("install", cfg.InstallURL),
		slog.String("source_id", cfg.Daemon.SourceID),
		slog.Int("games", len(cfg.Daemon.Games)),
	)

	if runErr := dmn.Run(ctx); runErr != nil {
		return fmt.Errorf("daemon run: %w", runErr)
	}

	return nil
}

// applyRegistration configures the daemon and local API after a fresh registration,
// and launches the tray app. The MSI cannot launch the tray directly because
// InstallExecuteSequence custom actions run inside the msiexec service context
// without access to the user's interactive desktop (Shell_NotifyIcon fails).
// The daemon runs fine headless from that context and spawns the tray here.
// On subsequent logins, both processes start from their HKCU Run keys.
func applyRegistration(
	ctx context.Context,
	cfg *appConfig,
	dmn *daemon.Daemon,
	api *localapi.Server,
	regResult *registerResult,
	frontendURL string,
	logger *slog.Logger,
) {
	linkExpiry, parseErr := time.Parse(time.RFC3339, regResult.LinkCodeExpiresAt)
	if parseErr != nil {
		linkExpiry = time.Now().Add(20 * time.Minute)
	}

	dmn.SetInitialLinkCode(regResult.LinkCode, linkExpiry)

	linkURL := localapi.BuildLinkURL(frontendURL, regResult.LinkCode)
	api.SetRegistered(regResult.LinkCode, linkURL, regResult.LinkCodeExpiresAt)
	logger.InfoContext(ctx, "source registered",
		slog.String("source_uuid", regResult.SourceUUID),
		slog.String("link_url", linkURL),
	)

	if svcmgr.Interactive() {
		fmt.Fprintf(os.Stderr, "\n  Link this source: %s\n\n", linkURL)
	}

	if cfg.Daemon.TrayBinaryPath != "" {
		if trayErr := startTrayProcess(cfg.Daemon.TrayBinaryPath,
			"--link-code", regResult.LinkCode,
			"--link-url", linkURL,
		); trayErr != nil {
			logger.WarnContext(ctx, "launch tray", slog.String("error", trayErr.Error()))
		}
	}
}

// wireRepairCallback sets up the repair callback on the local API server.
// The repair endpoint unlinks the source over WS and waits for a new link code.
func wireRepairCallback(
	ctx context.Context,
	dmn *daemon.Daemon,
	frontendURL string,
	api *localapi.Server,
) {
	api.SetRepairFunc(func(_ context.Context) (string, string, string, error) {
		unlinkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		code, expiresAt, unlinkErr := dmn.RequestUnlink(unlinkCtx)
		if unlinkErr != nil {
			return "", "", "", fmt.Errorf("unlink source: %w", unlinkErr)
		}

		linkURL := localapi.BuildLinkURL(frontendURL, code)
		expiresAtStr := expiresAt.Format(time.RFC3339)

		return code, linkURL, expiresAtStr, nil
	})
}
