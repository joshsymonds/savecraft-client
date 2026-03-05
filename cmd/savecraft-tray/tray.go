package main

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"fyne.io/systray"

	"github.com/joshsymonds/savecraft.gg/internal/localapi"
)

const pollInterval = 3 * time.Second

// trayApp holds the tray application state.
type trayApp struct {
	client      *localapi.Client
	frontendURL string
	logger      *slog.Logger
	cancel      context.CancelFunc

	// Link Account menu item — created at startup, shown/hidden dynamically.
	// linkURL is accessed from both pollState and handleClicks goroutines.
	mLinkAccount *systray.MenuItem
	linkURL      atomic.Pointer[string]

	// Re-pair menu item — visible only in StateRunning.
	mRepair *systray.MenuItem
}

func (a *trayApp) onReady() {
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel

	systray.SetIcon(iconBytes)
	systray.SetTooltip("Savecraft")

	mStatus := systray.AddMenuItem("Starting...", "Daemon status")
	mStatus.Disable()

	a.mLinkAccount = systray.AddMenuItem("Link Account", "Link this source to your savecraft.gg account")
	a.mLinkAccount.Hide()

	a.mRepair = systray.AddMenuItem("Re-pair", "Re-pair this source with a different account")
	a.mRepair.Hide()

	systray.AddSeparator()

	mCopyLogs := systray.AddMenuItem("Copy Logs", "Copy daemon logs to clipboard")
	mRestart := systray.AddMenuItem("Restart Daemon", "Restart the daemon process")

	systray.AddSeparator()

	mDashboard := systray.AddMenuItem("Open Dashboard", "Open the Savecraft dashboard in your browser")

	systray.AddSeparator()

	mQuit := systray.AddMenuItem("Quit", "Close the tray app")

	// Handle menu clicks.
	go a.handleClicks(mCopyLogs, mRestart, mDashboard, mQuit)

	// Poll daemon state.
	go a.pollState(ctx, mStatus)
}

func (a *trayApp) onExit() {
	if a.cancel != nil {
		a.cancel()
	}
}

func (a *trayApp) handleClicks(
	mCopyLogs, mRestart, mDashboard, mQuit *systray.MenuItem,
) {
	for {
		select {
		case <-a.mLinkAccount.ClickedCh:
			if url := a.linkURL.Load(); url != nil && *url != "" {
				if err := openBrowser(*url); err != nil {
					a.logger.Error("open link", slog.String("error", err.Error()))
				}
			}
		case <-a.mRepair.ClickedCh:
			a.doRepair()
		case <-mCopyLogs.ClickedCh:
			a.doCopyLogs()
		case <-mRestart.ClickedCh:
			a.doRestart()
		case <-mDashboard.ClickedCh:
			if err := openBrowser(a.frontendURL); err != nil {
				a.logger.Error("open dashboard", slog.String("error", err.Error()))
			}
		case <-mQuit.ClickedCh:
			systray.Quit()

			return
		}
	}
}

func (a *trayApp) pollState(ctx context.Context, mStatus *systray.MenuItem) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	// Poll immediately on start, then on each tick.
	a.updateStatus(mStatus)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.updateStatus(mStatus)
		}
	}
}

func (a *trayApp) updateStatus(mStatus *systray.MenuItem) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resp, err := a.client.Boot(ctx)
	if err != nil {
		a.hideLinkAccount()
		mStatus.SetTitle("Daemon offline")
		mStatus.SetTooltip("Cannot reach daemon at localhost")
		systray.SetTooltip("Savecraft — offline")

		return
	}

	// When registered, poll /link for the pairing URL.
	var linkAvailable bool

	if resp.State == localapi.StateRegistered {
		linkAvailable = a.updateLinkAccount(ctx)
		a.mRepair.Hide()
	} else {
		a.hideLinkAccount()

		if resp.State == localapi.StateRunning {
			a.mRepair.Show()
		} else {
			a.mRepair.Hide()
		}
	}

	title := stateTitle(resp.State)
	if linkAvailable {
		title = "Registered — click Link Account"
	}

	mStatus.SetTitle(title)

	if resp.Error != "" {
		mStatus.SetTooltip(resp.Error)
	} else {
		mStatus.SetTooltip("")
	}

	systray.SetTooltip("Savecraft — " + title)
}

func (a *trayApp) updateLinkAccount(ctx context.Context) bool {
	linkResp, status, err := a.client.Link(ctx)
	if err != nil || status != 200 || linkResp.LinkURL == "" {
		a.hideLinkAccount()

		return false
	}

	a.linkURL.Store(&linkResp.LinkURL)
	a.mLinkAccount.Show()

	return true
}

func (a *trayApp) hideLinkAccount() {
	empty := ""
	a.linkURL.Store(&empty)
	a.mLinkAccount.Hide()
}

func stateTitle(state localapi.State) string {
	switch state {
	case localapi.StateStarting:
		return "Starting..."
	case localapi.StateRegistering:
		return "Registering..."
	case localapi.StateRegistered:
		return "Registered (linking)"
	case localapi.StateRunning:
		return "Connected"
	case localapi.StateError:
		return "Error"
	default:
		return string(state)
	}
}

func (a *trayApp) doCopyLogs() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	entries, err := a.client.Logs(ctx)
	if err != nil {
		a.logger.Error("copy logs", slog.String("error", err.Error()))

		return
	}

	text := formatLogEntries(entries)
	if text == "" {
		text = "(no log entries)"
	}

	if clipErr := copyToClipboard(text); clipErr != nil {
		a.logger.Error("clipboard", slog.String("error", clipErr.Error()))
	}
}

func (a *trayApp) doRepair() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if _, err := a.client.Repair(ctx); err != nil {
		a.logger.Error("repair daemon", slog.String("error", err.Error()))
		systray.SetTooltip(fmt.Sprintf("Savecraft — re-pair failed: %v", err))
	}

	// State transitions to StateRegistered — pollState will show "Link Account" automatically.
}

func (a *trayApp) doRestart() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := a.client.Restart(ctx); err != nil {
		a.logger.Error("restart daemon", slog.String("error", err.Error()))

		// Show a brief tooltip indicating the error.
		systray.SetTooltip(fmt.Sprintf("Savecraft — restart failed: %v", err))
	}
}
