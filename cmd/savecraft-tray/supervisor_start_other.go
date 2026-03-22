//go:build !windows

package main

import (
	"log/slog"

	"github.com/joshsymonds/savecraft.gg/internal/svcmgr"
)

// buildStartDaemonFunc returns a function that starts the daemon via the
// platform service manager (systemd on Linux, launchd on macOS).
// These platforms already have native restart-on-crash, so this is a
// fallback for cases where the service manager didn't catch it.
func buildStartDaemonFunc(logger *slog.Logger) func() error {
	return func() error {
		logger.Info("supervisor: requesting daemon start via service manager")

		return svcmgr.Control(svcmgr.Config{
			Name:        "savecraft-daemon",
			DisplayName: "Savecraft Daemon",
			Description: "Syncs game saves to the cloud via Savecraft",
			AppName:     "savecraft",
		}, "start")
	}
}
