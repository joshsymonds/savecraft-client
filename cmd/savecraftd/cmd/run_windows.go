//go:build windows

package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"fyne.io/systray"
	"github.com/spf13/cobra"

	"github.com/joshsymonds/savecraft.gg/internal/daemon"
	"github.com/joshsymonds/savecraft.gg/internal/envfile"
	"github.com/joshsymonds/savecraft.gg/internal/localapi"
)

func buildRunFunc(
	serverURLDefault, installURLDefault, appName, statusPortDefault, frontendURL string,
) func(cmd *cobra.Command, args []string) error {
	return func(_ *cobra.Command, _ []string) error {
		return runDaemonWithTray(serverURLDefault, installURLDefault, appName, statusPortDefault, frontendURL)
	}
}

func runDaemonWithTray(serverURLDefault, installURLDefault, appName, statusPortDefault, frontendURL string) error {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	loadEnvFileDefaults(appName)

	// Start the local API server early so /boot and /link are available
	// before registration completes.
	statusPort := statusPortDefault
	if envPort := os.Getenv("SAVECRAFT_STATUS_PORT"); envPort != "" {
		statusPort = envPort
	}

	api := localapi.NewServer("localhost:"+statusPort, logger)
	api.Start()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle OS signals — cancel context and quit tray.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		select {
		case <-sigCh:
			cancel()
			systray.Quit()
		case <-ctx.Done():
		}
		signal.Stop(sigCh)
	}()

	errCh := make(chan error, 1)

	onReady := func() {
		trayStatus := setupTray(trayConfig{
			frontendURL: frontendURL,
			serverURL:   serverURLDefault,
			appName:     appName,
			logger:      logger,
		})

		cfg, err := loadConfig(serverURLDefault, installURLDefault)
		if err != nil {
			logger.Error("load config", slog.String("error", err.Error()))
			api.SetError(err.Error())
			trayStatus <- trayStateDisconnected
			return
		}

		// First-boot: if no auth token, self-register as a device.
		api.SetState(localapi.StateRegistering)

		envPath := envfile.EnvFilePath(appName)
		regResult, regErr := autoRegister(ctx, cfg, envPath)
		if regErr != nil {
			logger.Error("auto-register", slog.String("error", regErr.Error()))
			api.SetError(regErr.Error())
			trayStatus <- trayStateDisconnected
			return
		}
		if regResult != nil {
			linkURL := localapi.BuildLinkURL(frontendURL, regResult.LinkCode)
			api.SetRegistered(regResult.LinkCode, linkURL, regResult.LinkCodeExpiresAt)
			logger.Info("device registered",
				slog.String("device_uuid", regResult.DeviceUUID),
				slog.String("link_code", regResult.LinkCode),
				slog.String("link_url", linkURL),
			)
		} else {
			api.SetState(localapi.StateRunning)
		}

		binaryPath, err := os.Executable()
		if err != nil {
			logger.Error("get executable path", slog.String("error", err.Error()))
			trayStatus <- trayStateDisconnected
			return
		}
		cfg.Daemon.BinaryPath = binaryPath

		// Start daemon in background goroutine.
		go func() {
			subs, err := createSubsystems(ctx, cfg, appName, logger)
			if err != nil {
				logger.Error("create subsystems", slog.String("error", err.Error()))
				trayStatus <- trayStateDisconnected
				return
			}
			defer subs.close(ctx, logger)

			dmn := daemon.New(cfg.Daemon, subs.fsys, subs.watcher, subs.runner,
				subs.pusher, subs.ws, subs.plugins, subs.updater, logger)

			api.Handle("/status", daemon.StatusHandler(dmn))
			api.SetState(localapi.StateRunning)

			logger.Info("starting daemon",
				slog.String("server", cfg.ServerURL),
				slog.String("install", cfg.InstallURL),
				slog.String("device_id", cfg.Daemon.DeviceID),
				slog.Int("games", len(cfg.Daemon.Games)),
			)

			trayStatus <- trayStateConnected

			var runErr error
			if e := dmn.Run(ctx); e != nil {
				runErr = fmt.Errorf("daemon run: %w", e)
			}
			errCh <- runErr

			systray.Quit()
		}()
	}

	onQuit := func() {
		cancel()

		shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutCancel()
		if shutdownErr := api.Shutdown(shutCtx); shutdownErr != nil {
			logger.Error("local API server shutdown failed", slog.String("error", shutdownErr.Error()))
		}
	}

	systray.Run(onReady, onQuit)

	select {
	case err := <-errCh:
		return err
	default:
		return nil
	}
}
