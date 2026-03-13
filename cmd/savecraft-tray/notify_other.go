//go:build !windows

package main

import "log/slog"

// notifyFirstRun fires a native toast notification on non-Windows platforms.
func (a *trayApp) notifyFirstRun(linkURL string) {
	if err := toastFunc(
		"Savecraft installed!",
		"Click to connect your account.",
		linkURL,
	); err != nil {
		a.logger.Error("toast notification", slog.String("error", err.Error()))
	}
}
