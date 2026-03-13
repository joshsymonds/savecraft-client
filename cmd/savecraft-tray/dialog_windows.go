//go:build windows

package main

import (
	"fmt"
	"log/slog"
	"runtime"

	webview2 "github.com/jchv/go-webview2"
	"golang.org/x/sys/windows"
)

// showPairingDialog opens a branded WebView2 dialog showing the link code.
// It blocks until the dialog is closed (by user or when paired closes).
// The caller should run this in a goroutine.
//
//   - code:    the link code to display (e.g. "A3F-82K")
//   - linkURL: the URL to open when the user clicks "Link Account"
//   - paired:  closed by the caller when the daemon reaches StateRunning
func showPairingDialog(code, linkURL string, paired <-chan struct{}) error {
	// Pin this goroutine to a single OS thread for the lifetime of the dialog.
	// WebView2 uses COM (STA) and its message pump must run on the same thread
	// that created the window. Without this, Go's scheduler can migrate the
	// goroutine between OS threads, causing SetHtml/Run to silently fail
	// (blank window).
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	html, err := renderDialogHTML(code)
	if err != nil {
		return fmt.Errorf("render dialog HTML: %w", err)
	}

	w := webview2.NewWithOptions(webview2.WebViewOptions{
		AutoFocus: true,
		WindowOptions: webview2.WindowOptions{
			Title:  "Savecraft",
			Width:  420,
			Height: 480,
			Center: true,
		},
	})
	if w == nil {
		return fmt.Errorf("webview2 not available")
	}
	defer w.Destroy()

	// Fixed size, no resize.
	w.SetSize(420, 480, webview2.HintFixed)

	// Bind Go functions callable from JS.
	if err := w.Bind("openLink", func() {
		if browserErr := openBrowser(linkURL); browserErr != nil {
			slog.Error("open browser from dialog", slog.String("error", browserErr.Error()))
		}
	}); err != nil {
		return fmt.Errorf("bind openLink: %w", err)
	}

	if err := w.Bind("closeDialog", func() {
		w.Terminate()
	}); err != nil {
		return fmt.Errorf("bind closeDialog: %w", err)
	}

	w.SetHtml(html)

	// Watch for pairing completion in a background goroutine.
	// dialogDone is closed when w.Run() returns (user dismissed), so the
	// goroutine doesn't leak if the dialog closes before pairing completes.
	dialogDone := make(chan struct{})

	// Make the dialog always-on-top once the message pump starts and the
	// window is visible. Calling SetWindowPos before Run() has no effect
	// because the window isn't shown yet.
	w.Dispatch(func() {
		setTopmost(w)
	})

	go func() {
		select {
		case <-paired:
			w.Dispatch(func() {
				w.Eval("onPaired()")
			})
		case <-dialogDone:
			// Dialog closed before pairing — exit cleanly.
		}
	}()

	w.Run()
	close(dialogDone)

	return nil
}

var procSetWindowPos = windows.NewLazySystemDLL("user32.dll").NewProc("SetWindowPos") //nolint:gochecknoglobals // Windows API proc resolved once

// setTopmost makes the WebView2 window always-on-top using SetWindowPos.
func setTopmost(w webview2.WebView) {
	hwnd := uintptr(w.Window())
	if hwnd == 0 {
		return
	}

	const (
		hwndTopmost = ^uintptr(0) // HWND_TOPMOST = (HWND)-1
		swpNoMove   = 0x0002
		swpNoSize   = 0x0001
	)

	//nolint:errcheck // SetWindowPos failure is non-fatal
	procSetWindowPos.Call(hwnd, hwndTopmost, 0, 0, 0, 0, swpNoMove|swpNoSize)
}
