// Package main is the entry point for the Savecraft tray application.
// It displays daemon status in the system tray and provides menu items
// for copying logs, restarting the daemon, and opening the dashboard.
package main

import (
	_ "embed"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"strings"

	"fyne.io/systray"

	"github.com/joshsymonds/savecraft.gg/internal/localapi"
)

//go:embed assets/icon.png
var iconBytes []byte

const defaultStatusPort = "9182"

func main() {
	port := os.Getenv("SAVECRAFT_STATUS_PORT")
	if port == "" {
		port = defaultStatusPort
	}

	frontendURL := os.Getenv("SAVECRAFT_FRONTEND_URL")
	if frontendURL == "" {
		frontendURL = "https://savecraft.gg"
	}

	client := localapi.NewClient("http://localhost:" + port)

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	app := &trayApp{
		client:      client,
		frontendURL: frontendURL,
		logger:      logger,
	}

	systray.Run(app.onReady, app.onExit)
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
		args = []string{"xclip", "-selection", "clipboard"}
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
