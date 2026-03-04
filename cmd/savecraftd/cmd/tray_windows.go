//go:build windows

package cmd

import (
	_ "embed"
	"log/slog"
	"os/exec"

	"fyne.io/systray"
)

//go:embed assets/icon.png
var trayIconBytes []byte

type trayState int

const (
	trayStateNotPaired trayState = iota
	trayStateDisconnected
	trayStateConnected
)

// trayConfig holds parameters the tray needs for menu actions.
type trayConfig struct {
	frontendURL string
	serverURL   string
	appName     string
	logger      *slog.Logger
}

// setupTray configures the system tray icon and menu.
// Returns a channel to send state updates to.
func setupTray(cfg trayConfig) chan<- trayState {
	systray.SetIcon(trayIconBytes)
	systray.SetTooltip("Savecraft")

	mStatus := systray.AddMenuItem("Starting...", "Daemon status")
	mStatus.Disable()

	systray.AddSeparator()

	mPair := systray.AddMenuItem("Sign in && Pair...", "Pair this device with savecraft.gg")
	mDashboard := systray.AddMenuItem("Open Dashboard", "Open savecraft.gg in your browser")

	systray.AddSeparator()

	mQuit := systray.AddMenuItem("Quit", "Stop Savecraft")

	// Handle menu clicks.
	go func() {
		for {
			select {
			case <-mPair.ClickedCh:
				mPair.Disable()
				if err := showPairDialog(cfg.appName, cfg.serverURL, cfg.logger); err != nil {
					cfg.logger.Error("pair dialog", slog.String("error", err.Error()))
				} else {
					cfg.logger.Info("pairing complete — restart to connect")
					mStatus.SetTitle("Paired — restart to connect")
				}
				mPair.Enable()
			case <-mDashboard.ClickedCh:
				if cfg.frontendURL != "" {
					if err := exec.Command("rundll32", "url.dll,FileProtocolHandler", cfg.frontendURL).Start(); err != nil {
						cfg.logger.Error("open dashboard", slog.String("error", err.Error()))
					}
				}
			case <-mQuit.ClickedCh:
				systray.Quit()
				return
			}
		}
	}()

	// Listen for state updates.
	stateCh := make(chan trayState, 1)
	go func() {
		for state := range stateCh {
			switch state {
			case trayStateNotPaired:
				mStatus.SetTitle("Not paired")
				mStatus.SetTooltip("Run Sign in & Pair to connect this device")
			case trayStateDisconnected:
				mStatus.SetTitle("Disconnected")
				mStatus.SetTooltip("Daemon is not connected to savecraft.gg")
			case trayStateConnected:
				mStatus.SetTitle("Connected")
				mStatus.SetTooltip("Daemon is connected and syncing saves")
			}
		}
	}()

	return stateCh
}
