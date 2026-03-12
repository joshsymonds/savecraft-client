package main

import "testing"

func TestToastFiresOncePerProcessLifetime(t *testing.T) {
	var calls []string

	// Replace toast function with a recorder.
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
		t.Error("notifiedFirstRun should be true after first toast")
	}

	// Second call should NOT fire the toast.
	app.maybeNotifyFirstRun()

	if len(calls) != 1 {
		t.Fatalf("expected still 1 toast call after second invocation, got %d", len(calls))
	}
}

func TestToastSkipsWhenNoURL(t *testing.T) {
	var calls int

	original := toastFunc
	toastFunc = func(_, _, _ string) error {
		calls++
		return nil
	}
	t.Cleanup(func() { toastFunc = original })

	app := &trayApp{}

	// No URL stored — should not fire.
	app.maybeNotifyFirstRun()

	if calls != 0 {
		t.Fatalf("expected 0 toast calls with no URL, got %d", calls)
	}

	if app.notifiedFirstRun {
		t.Error("notifiedFirstRun should remain false when no URL")
	}

	// Empty URL — should not fire either.
	empty := ""
	app.linkURL.Store(&empty)
	app.maybeNotifyFirstRun()

	if calls != 0 {
		t.Fatalf("expected 0 toast calls with empty URL, got %d", calls)
	}
}

func TestToastSkipsWhenAlreadyNotified(t *testing.T) {
	var calls int

	original := toastFunc
	toastFunc = func(_, _, _ string) error {
		calls++
		return nil
	}
	t.Cleanup(func() { toastFunc = original })

	app := &trayApp{notifiedFirstRun: true}
	url := "https://my.savecraft.gg/link/ABC123"
	app.linkURL.Store(&url)

	app.maybeNotifyFirstRun()

	if calls != 0 {
		t.Fatalf("expected 0 toast calls when already notified, got %d", calls)
	}
}
