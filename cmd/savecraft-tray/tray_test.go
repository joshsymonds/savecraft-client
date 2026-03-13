package main

import "testing"

func TestClosePairedChClosesChannel(t *testing.T) {
	app := &trayApp{}
	ch := make(chan struct{})
	app.pairedCh = ch

	app.closePairedCh()

	// Channel should be closed — receiving should succeed immediately.
	select {
	case <-ch:
		// expected
	default:
		t.Fatal("pairedCh should be closed after closePairedCh")
	}

	if app.pairedCh != nil {
		t.Fatal("pairedCh field should be nil after closePairedCh")
	}
}

func TestClosePairedChIdempotent(_ *testing.T) {
	app := &trayApp{}
	app.pairedCh = make(chan struct{})

	app.closePairedCh()
	// Second call must not panic.
	app.closePairedCh()
}

func TestClosePairedChNilSafe(_ *testing.T) {
	app := &trayApp{}
	// pairedCh is nil by default — must not panic.
	app.closePairedCh()
}
