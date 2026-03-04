//go:build windows

package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"fyne.io/systray"
	"github.com/spf13/cobra"

	"github.com/joshsymonds/savecraft.gg/internal/daemon"
	"github.com/joshsymonds/savecraft.gg/internal/envfile"
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
			trayStatus <- trayStateDisconnected
			return
		}

		// First-boot: if no auth token, self-register as a device.
		envPath := envfile.EnvFilePath(appName)
		regResult, regErr := autoRegister(cfg, envPath)
		if regErr != nil {
			logger.Error("auto-register", slog.String("error", regErr.Error()))
			trayStatus <- trayStateDisconnected
			return
		}
		if regResult != nil {
			logger.Info("device registered",
				slog.String("device_uuid", regResult.DeviceUUID),
				slog.String("link_code", regResult.LinkCode),
			)
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

			statusPort := statusPortDefault
			if envPort := os.Getenv("SAVECRAFT_STATUS_PORT"); envPort != "" {
				statusPort = envPort
			}

			statusSrv := &http.Server{
				Addr:    "localhost:" + statusPort,
				Handler: daemon.StatusHandler(dmn),
			}
			go func() {
				if err := statusSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					logger.Error("status server failed", slog.String("error", err.Error()))
				}
			}()
			defer func() {
				shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer shutCancel()
				if shutdownErr := statusSrv.Shutdown(shutCtx); shutdownErr != nil {
					logger.Error("status server shutdown failed", slog.String("error", shutdownErr.Error()))
				}
			}()

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
	}

	systray.Run(onReady, onQuit)

	select {
	case err := <-errCh:
		return err
	default:
		return nil
	}
}
