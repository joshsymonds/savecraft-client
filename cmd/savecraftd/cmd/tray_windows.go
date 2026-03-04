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

	mLink := systray.AddMenuItem("Link Device...", "Open savecraft.gg to link this device")
	mDashboard := systray.AddMenuItem("Open Dashboard", "Open savecraft.gg in your browser")

	systray.AddSeparator()

	mQuit := systray.AddMenuItem("Quit", "Stop Savecraft")

	// Handle menu clicks.
	go func() {
		for {
			select {
			case <-mLink.ClickedCh:
				if cfg.frontendURL != "" {
					if err := exec.Command("rundll32", "url.dll,FileProtocolHandler", cfg.frontendURL+"/setup").Start(); err != nil {
						cfg.logger.Error("open link page", slog.String("error", err.Error()))
					}
				}
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
				mStatus.SetTitle("Not linked")
				mStatus.SetTooltip("Enter your link code at savecraft.gg/setup")
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
