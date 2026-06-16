package svcmgr

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestRun_PropagatesRunFuncError is the regression guard for the wedge bug:
// when the RunFunc returns on its own, Run must return that error promptly
// WITHOUT waiting for an OS signal. The old Program-based Run blocked on the
// signal channel forever, leaving the process alive-but-inert and defeating
// systemd's Restart=on-failure.
func TestRun_PropagatesRunFuncError(t *testing.T) {
	runErr := errors.New("run failed")

	done := make(chan error, 1)
	go func() {
		done <- Run(func(_ context.Context) error {
			return runErr
		})
	}()

	select {
	case err := <-done:
		if !errors.Is(err, runErr) {
			t.Errorf("Run err = %v, want %v", err, runErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after the RunFunc errored — it waited for a signal (the wedge)")
	}
}

// TestRun_ReturnsNilImmediately covers the clean self-termination path
// (e.g. self-update restart): RunFunc returns nil on its own, Run returns nil,
// the process exits zero and systemd does NOT restart it.
func TestRun_ReturnsNilImmediately(t *testing.T) {
	done := make(chan error, 1)
	go func() {
		done <- Run(func(_ context.Context) error {
			return nil
		})
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run err = %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after the RunFunc returned nil")
	}
}

func TestControl_UnknownAction(t *testing.T) {
	cfg := Config{
		Name:        "test-daemon",
		DisplayName: "Test",
		Description: "Test",
		AppName:     "test",
	}

	err := Control(cfg, "bogus")
	if err == nil {
		t.Fatal("expected error for unknown action")
	}

	want := "unknown service action: bogus"
	if err.Error() != want {
		t.Errorf("err = %q, want %q", err.Error(), want)
	}
}
