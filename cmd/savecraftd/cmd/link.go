package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/joshsymonds/savecraft.gg/internal/localapi"
	"github.com/joshsymonds/savecraft.gg/internal/regclient"
)

// refreshThreshold is how close to expiry we refresh the link code.
const refreshThreshold = 2 * time.Minute

// waitForLink polls the server until the source is linked to a user account.
// It sets the local API state to StateRegistered with the initial link code,
// auto-refreshes the code when it nears expiry, and transitions to
// StateRunning when linking completes. Both the boot flow and repair
// endpoint use this function.
func waitForLink(
	ctx context.Context,
	serverURL, authToken, frontendURL string,
	api *localapi.Server,
	linkCode, expiresAt string,
	pollInterval time.Duration,
	logger *slog.Logger,
) error {
	linkURL := localapi.BuildLinkURL(frontendURL, linkCode)
	api.SetRegistered(linkCode, linkURL, expiresAt)

	expiry, parseErr := time.Parse(time.RFC3339, expiresAt)
	if parseErr != nil {
		// If we can't parse the expiry, refresh immediately on next tick.
		expiry = time.Now()
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for link: %w", ctx.Err())
		case <-ticker.C:
			linkCode, expiry = maybeRefreshCode(ctx, serverURL, authToken, frontendURL, api, linkCode, expiry, logger)

			if pollLinked(ctx, serverURL, authToken, logger) {
				api.SetState(localapi.StateRunning)
				logger.InfoContext(ctx, "source linked to user")

				return nil
			}
		}
	}
}

// maybeRefreshCode refreshes the link code if it's near expiry.
func maybeRefreshCode(
	ctx context.Context,
	serverURL, authToken, frontendURL string,
	api *localapi.Server,
	linkCode string, expiry time.Time,
	logger *slog.Logger,
) (string, time.Time) {
	if time.Until(expiry) >= refreshThreshold {
		return linkCode, expiry
	}

	refreshed, err := regclient.RefreshLinkCode(ctx, serverURL, authToken)
	if err != nil {
		logger.WarnContext(ctx, "failed to refresh link code", slog.String("error", err.Error()))

		return linkCode, expiry
	}

	linkCode = refreshed.LinkCode
	linkURL := localapi.BuildLinkURL(frontendURL, refreshed.LinkCode)

	newExpiry, pErr := time.Parse(time.RFC3339, refreshed.ExpiresAt)
	if pErr == nil {
		expiry = newExpiry
	}

	api.SetRegistered(linkCode, linkURL, refreshed.ExpiresAt)
	logger.InfoContext(ctx, "refreshed link code",
		slog.String("link_code", linkCode),
		slog.String("expires_at", refreshed.ExpiresAt),
	)

	return linkCode, expiry
}

// pollLinked checks whether the source has been linked to a user.
func pollLinked(ctx context.Context, serverURL, authToken string, logger *slog.Logger) bool {
	status, err := regclient.Status(ctx, serverURL, authToken)
	if err != nil {
		logger.WarnContext(ctx, "failed to check source status", slog.String("error", err.Error()))

		return false
	}

	return status.Linked
}
