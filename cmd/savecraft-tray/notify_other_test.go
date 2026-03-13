//go:build !windows

package main

import "testing"

func TestNotifyFirstRunFiresOncePerLifetime(t *testing.T) {
	var calls []string

	original := toastFunc
	toastFunc = func(_, _, clickURL string) error {
		calls = append(calls, clickURL)
		return nil
	}
	t.Cleanup(func() { toastFunc = original })

	app := &trayApp{}
	url := "https://my.savecraft.gg/link/ABC123"
	app.linkURL.Store(&url)

	// First call should fire the toast.
	app.maybeNotifyFirstRun()

	if len(calls) != 1 {
		t.Fatalf("expected 1 toast call, got %d", len(calls))
	}

	if calls[0] != url {
		t.Errorf("expected URL %q, got %q", url, calls[0])
	}

	if !app.notifiedFirstRun {
		t.Error("notifiedFirstRun should be true after first notification")
	}

	// pairedCh must be created so closePairedCh can signal the dialog.
	if app.pairedCh == nil {
		t.Error("pairedCh should be non-nil after maybeNotifyFirstRun")
	}

	// Second call should NOT fire the toast.
	app.maybeNotifyFirstRun()

	if len(calls) != 1 {
		t.Fatalf("expected still 1 toast call after second invocation, got %d", len(calls))
	}
}
