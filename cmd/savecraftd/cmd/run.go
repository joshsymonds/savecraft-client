package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/joshsymonds/savecraft.gg/internal/daemon"
	"github.com/joshsymonds/savecraft.gg/internal/envfile"
	"github.com/joshsymonds/savecraft.gg/internal/localapi"
	"github.com/joshsymonds/savecraft.gg/internal/regclient"
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

	// Start the local API server early so /boot, /link, /shutdown, and /restart
	// are available before registration completes. The /status route is added later.
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

	// First-boot: if no auth token, self-register as a source.
	api.SetState(localapi.StateRegistering)

	envPath := envfile.EnvFilePath(appName)
	regResult, registered, regErr := autoRegister(ctx, cfg, envPath)
	if regErr != nil {
		api.SetError(regErr.Error())

		return fmt.Errorf("auto-register: %w", regErr)
	}

	if registered {
		linkURL := localapi.BuildLinkURL(frontendURL, regResult.LinkCode)
		logger.InfoContext(ctx, "source registered",
			slog.String("source_uuid", regResult.SourceUUID),
			slog.String("link_code", regResult.LinkCode),
			slog.String("link_url", linkURL),
		)

		if svcmgr.Interactive() {
			fmt.Fprintf(os.Stderr, "\n  Link this source: %s\n\n", linkURL)
		}

		// Wait for the user to link the source via the web UI.
		// waitForLink sets StateRegistered, polls for linking, auto-refreshes codes,
		// and transitions to StateRunning when linked.
		if linkErr := waitForLink(ctx, cfg.ServerURL, cfg.AuthToken, frontendURL,
			api, regResult.LinkCode, regResult.LinkCodeExpiresAt,
			5*time.Second, logger); linkErr != nil {
			return fmt.Errorf("wait for link: %w", linkErr)
		}
	} else {
		// Token exists — check if source is actually linked.
		status, statusErr := regclient.Status(ctx, cfg.ServerURL, cfg.AuthToken)
		if statusErr != nil {
			logger.WarnContext(ctx, "could not check source status, assuming linked",
				slog.String("error", statusErr.Error()))
			api.SetState(localapi.StateRunning)
		} else if !status.Linked {
			// Source exists but is not linked to a user — enter the linking flow.
			logger.InfoContext(ctx, "source exists but is not linked, waiting for link")

			linkCode := status.LinkCode
			expiresAt := status.LinkCodeExpiresAt

			// If no active code, generate one.
			if linkCode == "" {
				refreshed, refreshErr := regclient.RefreshLinkCode(ctx, cfg.ServerURL, cfg.AuthToken)
				if refreshErr != nil {
					api.SetError(refreshErr.Error())
					return fmt.Errorf("generate link code for unlinked source: %w", refreshErr)
				}

				linkCode = refreshed.LinkCode
				expiresAt = refreshed.ExpiresAt
			}

			linkURL := localapi.BuildLinkURL(frontendURL, linkCode)
			if svcmgr.Interactive() {
				fmt.Fprintf(os.Stderr, "\n  Link this source: %s\n\n", linkURL)
			}

			if linkErr := waitForLink(ctx, cfg.ServerURL, cfg.AuthToken, frontendURL,
				api, linkCode, expiresAt,
				5*time.Second, logger); linkErr != nil {
				return fmt.Errorf("wait for link: %w", linkErr)
			}
		} else {
			api.SetState(localapi.StateRunning)
		}
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

	// Set up repair callback — unlinks the source and starts a background
	// waitForLink goroutine. Cancels any previous repair goroutine.
	var repairCancel context.CancelFunc
	api.SetRepairFunc(func(_ context.Context) (string, string, string, error) {
		// Cancel any in-flight repair goroutine.
		if repairCancel != nil {
			repairCancel()
		}

		result, unlinkErr := regclient.Unlink(ctx, cfg.ServerURL, cfg.AuthToken)
		if unlinkErr != nil {
			return "", "", "", fmt.Errorf("unlink source: %w", unlinkErr)
		}

		linkURL := localapi.BuildLinkURL(frontendURL, result.LinkCode)

		// Start background goroutine to poll for re-linking.
		repairCtx, cancel := context.WithCancel(ctx)
		repairCancel = cancel

		go func() {
			if linkErr := waitForLink(repairCtx, cfg.ServerURL, cfg.AuthToken, frontendURL,
				api, result.LinkCode, result.ExpiresAt,
				5*time.Second, logger); linkErr != nil && repairCtx.Err() == nil {
				logger.ErrorContext(repairCtx, "repair wait-for-link failed",
					slog.String("error", linkErr.Error()))
			}
		}()

		return result.LinkCode, linkURL, result.ExpiresAt, nil
	})

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
