package cmd

import (
	"context"
	"errors"
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

	if linkErr := ensureLinked(ctx, cfg, appName, frontendURL, api, logger); linkErr != nil {
		return linkErr
	}

	return runDaemonSubsystems(ctx, cfg, appName, frontendURL, api, logger)
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

// ensureLinked handles registration and the wait-for-link flow at boot.
func ensureLinked(
	ctx context.Context,
	cfg *appConfig,
	appName, frontendURL string,
	api *localapi.Server,
	logger *slog.Logger,
) error {
	api.SetState(localapi.StateRegistering)

	envPath := envfile.EnvFilePath(appName)
	regResult, registered, regErr := autoRegister(ctx, cfg, envPath)
	if regErr != nil {
		api.SetError(regErr.Error())

		return fmt.Errorf("auto-register: %w", regErr)
	}

	if registered {
		return handleNewRegistration(ctx, cfg, frontendURL, api, regResult, logger)
	}

	return handleExistingSource(ctx, cfg, appName, frontendURL, api, logger)
}

// handleNewRegistration handles the first-boot case where the source was just registered.
func handleNewRegistration(
	ctx context.Context,
	cfg *appConfig,
	frontendURL string,
	api *localapi.Server,
	regResult *regclient.RegisterResult,
	logger *slog.Logger,
) error {
	linkURL := localapi.BuildLinkURL(frontendURL, regResult.LinkCode)
	logger.InfoContext(ctx, "source registered",
		slog.String("source_uuid", regResult.SourceUUID),
		slog.String("link_code", regResult.LinkCode),
		slog.String("link_url", linkURL),
	)

	if svcmgr.Interactive() {
		fmt.Fprintf(os.Stderr, "\n  Link this source: %s\n\n", linkURL)
	}

	if linkErr := waitForLink(ctx, cfg.ServerURL, cfg.AuthToken, frontendURL,
		api, regResult.LinkCode, regResult.LinkCodeExpiresAt,
		5*time.Second, logger); linkErr != nil {
		return fmt.Errorf("wait for link: %w", linkErr)
	}

	return nil
}

// handleExistingSource checks link status for a source with an existing token.
func handleExistingSource(
	ctx context.Context,
	cfg *appConfig,
	appName, frontendURL string,
	api *localapi.Server,
	logger *slog.Logger,
) error {
	status, statusErr := regclient.Status(ctx, cfg.ServerURL, cfg.AuthToken)

	switch {
	case errors.Is(statusErr, regclient.ErrSourceNotFound):
		logger.WarnContext(ctx, "source not found on server, re-registering")

		return reRegister(ctx, cfg, appName, frontendURL, api, logger)
	case statusErr != nil:
		logger.WarnContext(ctx, "could not check source status, assuming linked",
			slog.String("error", statusErr.Error()))
		api.SetState(localapi.StateRunning)
	case !status.Linked:
		if err := handleUnlinkedSource(ctx, cfg, frontendURL, api, status, logger); err != nil {
			return err
		}
	default:
		api.SetState(localapi.StateRunning)
	}

	return nil
}

// reRegister clears stale credentials and performs a fresh registration.
// This handles the case where the server-side database was reset.
func reRegister(
	ctx context.Context,
	cfg *appConfig,
	appName, frontendURL string,
	api *localapi.Server,
	logger *slog.Logger,
) error {
	cfg.AuthToken = ""
	cfg.Daemon.AuthToken = ""

	envPath := envfile.EnvFilePath(appName)
	regResult, registered, regErr := autoRegister(ctx, cfg, envPath)

	if regErr != nil {
		api.SetError(regErr.Error())

		return fmt.Errorf("re-register after source-not-found: %w", regErr)
	}

	if !registered {
		return fmt.Errorf("re-register: expected registration but token was still set")
	}

	return handleNewRegistration(ctx, cfg, frontendURL, api, regResult, logger)
}

// handleUnlinkedSource enters the wait-for-link flow for an existing but unlinked source.
func handleUnlinkedSource(
	ctx context.Context,
	cfg *appConfig,
	frontendURL string,
	api *localapi.Server,
	status *regclient.StatusResult,
	logger *slog.Logger,
) error {
	logger.InfoContext(ctx, "source exists but is not linked, waiting for link")

	linkCode := status.LinkCode
	expiresAt := status.LinkCodeExpiresAt

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

	return nil
}

// runDaemonSubsystems creates subsystems, wires up repair, and runs the daemon.
func runDaemonSubsystems(
	ctx context.Context,
	cfg *appConfig,
	appName, frontendURL string,
	api *localapi.Server,
	logger *slog.Logger,
) error {
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

	api.Handle("/status", daemon.StatusHandler(dmn))
	api.SetState(localapi.StateRunning)

	wireRepairCallback(ctx, cfg, frontendURL, api, logger)

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

// wireRepairCallback sets up the repair callback on the local API server.
func wireRepairCallback(
	ctx context.Context,
	cfg *appConfig,
	frontendURL string,
	api *localapi.Server,
	logger *slog.Logger,
) {
	var repairCancel context.CancelFunc

	api.SetRepairFunc(func(_ context.Context) (string, string, string, error) {
		if repairCancel != nil {
			repairCancel()
		}

		result, unlinkErr := regclient.Unlink(ctx, cfg.ServerURL, cfg.AuthToken)
		if unlinkErr != nil {
			return "", "", "", fmt.Errorf("unlink source: %w", unlinkErr)
		}

		linkURL := localapi.BuildLinkURL(frontendURL, result.LinkCode)

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
}
