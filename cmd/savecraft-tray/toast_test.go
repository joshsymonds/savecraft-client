package main

import "testing"

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
