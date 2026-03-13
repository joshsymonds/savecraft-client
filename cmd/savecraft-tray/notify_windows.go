//go:build windows

package main

import "log/slog"

// dialogFunc is the function used to show the pairing dialog.
// Replaced in tests to avoid WebView2 dependencies.
var dialogFunc = showPairingDialog //nolint:gochecknoglobals // test injection point

// notifyFirstRun opens a branded WebView2 dialog on Windows. If WebView2 is
// unavailable, it falls back to a toast notification.
// pairedCh is already created by maybeNotifyFirstRun before this is called.
func (a *trayApp) notifyFirstRun(linkURL string) {
	code := a.linkCode.Load()
	if code == nil || *code == "" {
		return
	}

	go func() {
		if err := dialogFunc(*code, linkURL, a.pairedCh); err != nil {
			a.logger.Error("pairing dialog", slog.String("error", err.Error()))

			// Fall back to toast if WebView2 is unavailable.
			if toastErr := toastFunc(
				"Savecraft installed!",
				"Click to connect your account.",
				linkURL,
			); toastErr != nil {
				a.logger.Error("toast fallback", slog.String("error", toastErr.Error()))
			}
		}
	}()
}
