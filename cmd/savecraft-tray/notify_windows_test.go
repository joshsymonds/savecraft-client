//go:build windows

package main

import (
	"log/slog"
	"testing"
	"time"
)

func TestNotifyFirstRunFiresOncePerLifetime(t *testing.T) {
	type dialogCall struct {
		code    string
		linkURL string
	}

	var calls []dialogCall

	// Replace dialog function with a recorder that returns immediately.
	originalDialog := dialogFunc
	dialogFunc = func(code, linkURL string, _ <-chan struct{}) error {
		calls = append(calls, dialogCall{code, linkURL})
		return nil
	}
	t.Cleanup(func() { dialogFunc = originalDialog })

	app := &trayApp{logger: slog.Default()}
	url := "https://my.savecraft.gg/link/ABC123"
	code := "ABC-123"
	app.linkURL.Store(&url)
	app.linkCode.Store(&code)

	// First call should launch the dialog.
	app.maybeNotifyFirstRun()

	// Dialog runs in a goroutine — give it a moment to execute.
	time.Sleep(50 * time.Millisecond)

	if len(calls) != 1 {
		t.Fatalf("expected 1 dialog call, got %d", len(calls))
	}

	if calls[0].code != code {
		t.Errorf("expected code %q, got %q", code, calls[0].code)
	}

	if calls[0].linkURL != url {
		t.Errorf("expected URL %q, got %q", url, calls[0].linkURL)
	}

	if !app.notifiedFirstRun {
		t.Error("notifiedFirstRun should be true after first notification")
	}

	// pairedCh must be created so closePairedCh can signal the dialog.
	if app.pairedCh == nil {
		t.Error("pairedCh should be non-nil after maybeNotifyFirstRun")
	}

	// Second call should NOT fire.
	app.maybeNotifyFirstRun()

	time.Sleep(50 * time.Millisecond)

	if len(calls) != 1 {
		t.Fatalf("expected still 1 dialog call after second invocation, got %d", len(calls))
	}
}

func TestNotifyFirstRunSkipsWithoutCode(t *testing.T) {
	var calls int

	originalDialog := dialogFunc
	dialogFunc = func(_, _ string, _ <-chan struct{}) error {
		calls++
		return nil
	}
	t.Cleanup(func() { dialogFunc = originalDialog })

	app := &trayApp{logger: slog.Default()}
	url := "https://my.savecraft.gg/link/ABC123"
	app.linkURL.Store(&url)
	// linkCode not set — dialog should not fire.

	app.maybeNotifyFirstRun()

	time.Sleep(50 * time.Millisecond)

	if calls != 0 {
		t.Fatalf("expected 0 dialog calls without link code, got %d", calls)
	}
}
