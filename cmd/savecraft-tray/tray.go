package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/systray"

	"github.com/joshsymonds/savecraft.gg/internal/localapi"
)

const pollInterval = 3 * time.Second

// toastFunc is the function used to show toast notifications.
// Replaced in tests to avoid shell dependencies.
var toastFunc = showToast //nolint:gochecknoglobals // test injection point

// trayApp holds the tray application state.
type trayApp struct {
	client      *localapi.Client
	frontendURL string
	logger      *slog.Logger
	cancel      context.CancelFunc
	sup         *supervisor

	// Link Account menu item — created at startup, shown/hidden dynamically.
	// linkURL and linkCode are accessed from both pollState and handleClicks goroutines.
	mLinkAccount *systray.MenuItem
	linkURL      atomic.Pointer[string]
	linkCode     atomic.Pointer[string]

	// Re-pair menu item — visible only in StateRunning.
	mRepair *systray.MenuItem

	// Restart/upgrade menu item — text changes when an update is pending.
	mRestart *systray.MenuItem

	// initialLinkCode and initialLinkURL are passed via CLI flags when the
	// daemon launches the tray after a fresh registration. When set, the
	// pairing dialog opens immediately without waiting for a poll cycle.
	initialLinkCode string
	initialLinkURL  string

	// notifiedFirstRun prevents repeated toast notifications within a single
	// tray process lifetime. Set to true after the first toast fires.
	notifiedFirstRun bool

	// pairedCh is created when the pairing dialog opens and closed when
	// StateRunning is detected, signaling the dialog to auto-close.
	pairedMu sync.Mutex
	pairedCh chan struct{}
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
	a.mRestart = systray.AddMenuItem("Restart Daemon", "Restart the daemon process")
	mUpdatePlugins := systray.AddMenuItem("Check for Plugin Updates", "Download the latest plugin versions")

	systray.AddSeparator()

	mDashboard := systray.AddMenuItem("Open Dashboard", "Open the Savecraft dashboard in your browser")

	systray.AddSeparator()

	mQuit := systray.AddMenuItem("Quit", "Close the tray app")

	// If launched with --link-code/--link-url, show the pairing dialog
	// immediately without waiting for a poll cycle.
	if a.initialLinkCode != "" && a.initialLinkURL != "" {
		a.linkCode.Store(&a.initialLinkCode)
		a.linkURL.Store(&a.initialLinkURL)
		a.mLinkAccount.Show()
		a.maybeNotifyFirstRun()
	}

	// Handle menu clicks.
	go a.handleClicks(mCopyLogs, mUpdatePlugins, mDashboard, mQuit)

	// Poll daemon state.
	go a.pollState(ctx, mStatus)
}

func (a *trayApp) onExit() {
	if a.cancel != nil {
		a.cancel()
	}
}

func (a *trayApp) handleClicks(
	mCopyLogs, mUpdatePlugins, mDashboard, mQuit *systray.MenuItem,
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
		case <-a.mRestart.ClickedCh:
			a.doRestart()
		case <-mUpdatePlugins.ClickedCh:
			a.doUpdatePlugins()
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

		if a.sup != nil {
			a.sup.onDaemonUnreachable()
		}

		if a.sup != nil && a.sup.restarting() {
			mStatus.SetTitle("Starting...")
			mStatus.SetTooltip("Restarting daemon")
			systray.SetTooltip("Savecraft — starting")
		} else {
			mStatus.SetTitle("Offline")
			mStatus.SetTooltip("Cannot reach daemon at localhost")
			systray.SetTooltip("Savecraft — offline")
		}

		return
	}

	if a.sup != nil {
		a.sup.onDaemonReachable()
	}

	// When registered, poll /link for the pairing URL.
	var linkAvailable bool

	if resp.State == localapi.StateRegistered {
		linkAvailable = a.updateLinkAccount(ctx)
		a.mRepair.Hide()

		// Fire a one-shot toast notification on first detection.
		if linkAvailable {
			a.maybeNotifyFirstRun()
		}
	} else {
		a.hideLinkAccount()

		if resp.State == localapi.StateRunning {
			a.mRepair.Show()
			a.closePairedCh()
		} else {
			a.mRepair.Hide()
		}
	}

	// Update restart menu item text based on pending update.
	if resp.PendingVersion != "" {
		a.mRestart.SetTitle(fmt.Sprintf("Upgrade to v%s", resp.PendingVersion))
	} else {
		a.mRestart.SetTitle("Restart Daemon")
	}

	title := stateTitle(resp.State)
	if linkAvailable {
		title = "Ready to link — click Link Account"
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
	a.linkCode.Store(&linkResp.LinkCode)
	a.mLinkAccount.Show()

	return true
}

// maybeNotifyFirstRun fires a first-run notification on the first call where
// linkURL is set. On Windows, opens a branded WebView2 dialog; on other
// platforms, fires a native toast notification. Subsequent calls are no-ops.
func (a *trayApp) maybeNotifyFirstRun() {
	if a.notifiedFirstRun {
		return
	}

	url := a.linkURL.Load()
	if url == nil || *url == "" {
		return
	}

	// Create pairedCh BEFORE setting notifiedFirstRun, so closePairedCh
	// (called from pollState on StateRunning) always finds the channel
	// if notifiedFirstRun is true.
	a.pairedMu.Lock()
	a.pairedCh = make(chan struct{})
	a.pairedMu.Unlock()

	a.notifiedFirstRun = true
	a.notifyFirstRun(*url)
}

var emptyStr string //nolint:gochecknoglobals // reused sentinel avoids allocation per poll cycle

func (a *trayApp) hideLinkAccount() {
	a.linkURL.Store(&emptyStr)
	a.linkCode.Store(&emptyStr)
	a.mLinkAccount.Hide()
}

// closePairedCh signals the pairing dialog (if open) to auto-close.
func (a *trayApp) closePairedCh() {
	a.pairedMu.Lock()
	defer a.pairedMu.Unlock()

	if a.pairedCh != nil {
		close(a.pairedCh)
		a.pairedCh = nil
	}
}

func stateTitle(state localapi.State) string {
	switch state {
	case localapi.StateStarting:
		return "Starting..."
	case localapi.StateRegistering:
		return "Connecting..."
	case localapi.StateRegistered:
		return "Ready to link"
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

func (a *trayApp) doUpdatePlugins() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	resp, err := a.client.UpdatePlugins(ctx)
	if err != nil {
		a.logger.Error("update plugins", slog.String("error", err.Error()))
		systray.SetTooltip(fmt.Sprintf("Savecraft — plugin update failed: %v", err))

		return
	}

	if len(resp.Updated) == 0 {
		systray.SetTooltip("Savecraft — all plugins up to date")
	} else {
		systray.SetTooltip(fmt.Sprintf("Savecraft — updated plugins: %s", strings.Join(resp.Updated, ", ")))
	}
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
