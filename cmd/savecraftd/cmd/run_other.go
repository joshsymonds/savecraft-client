//go:build !windows

package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/joshsymonds/savecraft.gg/internal/daemon"
	"github.com/joshsymonds/savecraft.gg/internal/envfile"
	"github.com/joshsymonds/savecraft.gg/internal/localapi"
)

func buildRunFunc(
	serverURLDefault, installURLDefault, appName, statusPortDefault, frontendURL string,
) func(cmd *cobra.Command, args []string) error {
	return func(_ *cobra.Command, _ []string) error {
		return runDaemon(serverURLDefault, installURLDefault, appName, statusPortDefault, frontendURL)
	}
}

func runDaemon(serverURLDefault, installURLDefault, appName, statusPortDefault, frontendURL string) error {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	loadEnvFileDefaults(appName)

	// Start the local API server early so /boot and /link are available
	// before registration completes. The daemon /status route is added later.
	statusPort := statusPortDefault
	if envPort := os.Getenv("SAVECRAFT_STATUS_PORT"); envPort != "" {
		statusPort = envPort
	}

	api := localapi.NewServer("localhost:"+statusPort, logger)
	api.Start()
	defer func() {
		shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutCancel()
		if shutdownErr := api.Shutdown(shutCtx); shutdownErr != nil {
			logger.Error("local API server shutdown failed", slog.String("error", shutdownErr.Error()))
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	cfg, err := loadConfig(serverURLDefault, installURLDefault)
	if err != nil {
		api.SetError(err.Error())

		return fmt.Errorf("load config: %w", err)
	}

	// First-boot: if no auth token, self-register as a device.
	api.SetState(localapi.StateRegistering)

	envPath := envfile.EnvFilePath(appName)
	regResult, regErr := autoRegister(ctx, cfg, envPath)
	if regErr != nil {
		api.SetError(regErr.Error())

		return fmt.Errorf("auto-register: %w", regErr)
	}

	if regResult != nil {
		linkURL := localapi.BuildLinkURL(frontendURL, regResult.LinkCode)
		api.SetRegistered(regResult.LinkCode, linkURL, regResult.LinkCodeExpiresAt)
		logger.Info("device registered",
			slog.String("device_uuid", regResult.DeviceUUID),
			slog.String("link_code", regResult.LinkCode),
			slog.String("link_url", linkURL),
		)
		fmt.Fprintf(os.Stderr, "\n  Link this device: %s\n\n", linkURL)
	} else {
		api.SetState(localapi.StateRunning)
	}

	binaryPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}

	cfg.Daemon.BinaryPath = binaryPath

	subsystems, err := createSubsystems(ctx, cfg, appName, logger)
	if err != nil {
		return err
	}
	defer subsystems.close(ctx, logger)

	dmn := daemon.New(cfg.Daemon, subsystems.fsys, subsystems.watcher, subsystems.runner,
		subsystems.pusher, subsystems.ws, subsystems.plugins, subsystems.updater, logger)

	// Add daemon status to the local API server.
	api.Handle("/status", daemon.StatusHandler(dmn))
	api.SetState(localapi.StateRunning)

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
