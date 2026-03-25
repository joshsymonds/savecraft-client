// Package main is the entry point for the Savecraft tray application.
// It displays daemon status in the system tray and provides menu items
// for copying logs, restarting the daemon, and opening the dashboard.
package main

import (
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"strings"

	"fyne.io/systray"

	"github.com/joshsymonds/savecraft.gg/internal/localapi"
)

// iconBytes is set per-platform: .ico on Windows, .png elsewhere.
// See icon_windows.go and icon_other.go.

// Overridden via ldflags at build time for staging/production.
var (
	defaultStatusPort  = "9182"
	defaultFrontendURL = "https://my.savecraft.gg"
)

func main() {
	release, singleErr := acquireSingleton()
	if singleErr != nil {
		os.Exit(0)
	}
	defer release()

	port := os.Getenv("SAVECRAFT_STATUS_PORT")
	if port == "" {
		port = defaultStatusPort
	}

	frontendURL := os.Getenv("SAVECRAFT_FRONTEND_URL")
	if frontendURL == "" {
		frontendURL = defaultFrontendURL
	}

	// Parse --link-code and --link-url flags for immediate dialog on first run.
	// The daemon passes these when launching the tray after a fresh registration
	// so the pairing dialog appears instantly without waiting for a poll cycle.
	linkCode, linkURL := parseLinkFlags()

	client := localapi.NewClient("http://localhost:" + port)

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	sup := newSupervisor(buildStartDaemonFunc(logger))
	sup.toastFunc = func(title, body, clickURL string) {
		_ = showToast(title, body, clickURL)
	}
	sup.logger = logger

	app := &trayApp{
		client:          client,
		frontendURL:     frontendURL,
		logger:          logger,
		sup:             sup,
		initialLinkCode: linkCode,
		initialLinkURL:  linkURL,
	}

	systray.Run(app.onReady, app.onExit)
}

// parseLinkFlags extracts --link-code and --link-url from os.Args.
// Uses manual parsing to avoid pulling in a flags library for two args.
func parseLinkFlags() (code, url string) {
	return parseLinkArgs(os.Args[1:])
}

// parseLinkArgs extracts --link-code and --link-url from the given args slice.
func parseLinkArgs(args []string) (code, url string) {
	for i, arg := range args {
		switch {
		case arg == "--link-code" && i+1 < len(args):
			code = args[i+1]
		case strings.HasPrefix(arg, "--link-code="):
			code = strings.TrimPrefix(arg, "--link-code=")
		case arg == "--link-url" && i+1 < len(args):
			url = args[i+1]
		case strings.HasPrefix(arg, "--link-url="):
			url = strings.TrimPrefix(arg, "--link-url=")
		}
	}

	return code, url
}

// openBrowser opens a URL in the user's default browser.
func openBrowser(url string) error {
	var args []string

	switch runtime.GOOS {
	case "darwin":
		args = []string{"open", url}
	case "windows":
		args = []string{"rundll32", "url.dll,FileProtocolHandler", url}
	default:
		args = []string{"xdg-open", url}
	}

	return runCommand(args[0], args[1:]...)
}

// copyToClipboard copies text to the system clipboard.
func copyToClipboard(text string) error {
	var args []string

	switch runtime.GOOS {
	case "darwin":
		args = []string{"pbcopy"}
	case "windows":
		args = []string{"clip"}
	default:
		if os.Getenv("WAYLAND_DISPLAY") != "" {
			args = []string{"wl-copy"}
		} else {
			args = []string{"xclip", "-selection", "clipboard"}
		}
	}

	return runCommandWithStdin(args[0], text, args[1:]...)
}

// formatLogEntries formats log entries as human-readable text for clipboard.
func formatLogEntries(entries []localapi.LogEntry) string {
	var buf strings.Builder

	for _, e := range entries {
		fmt.Fprintf(&buf, "[%s] %s %s", e.Time, e.Level, e.Message)

		for k, v := range e.Attrs {
			fmt.Fprintf(&buf, " %s=%v", k, v)
		}

		buf.WriteByte('\n')
	}

	return buf.String()
}
