//go:build !windows

package svcmgr

import (
	"context"
	"syscall"
	"testing"
	"time"
)

// TestRun_ShutdownOnSignal covers the graceful path: an OS shutdown signal
// cancels the context passed to the RunFunc, the RunFunc returns nil, and Run
// returns nil (process exits zero).
func TestRun_ShutdownOnSignal(t *testing.T) {
	started := make(chan struct{})
	done := make(chan error, 1)

	go func() {
		done <- Run(func(ctx context.Context) error {
			// NotifyContext registers the signal handler before invoking the
			// RunFunc, so by the time started is closed the handler is live.
			close(started)
			<-ctx.Done()

			return nil
		})
	}()

	<-started
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGINT); err != nil {
		t.Fatalf("kill: %v", err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after shutdown signal")
	}
}

// TestControl_DispatchesServiceActions covers control's dispatch to the
// service-lifecycle backends. start/stop/restart only invoke the injected
// command runner (no filesystem or process side effects), so a fake runner
// exercises the full dispatch path safely.
func TestControl_DispatchesServiceActions(t *testing.T) {
	cfg := Config{Name: "test-daemon", DisplayName: "Test", Description: "Test", AppName: "test"}

	for _, action := range []string{"start", "stop", "restart"} {
		var calls int
		fakeRunner := func(_ string, _ ...string) ([]byte, error) {
			calls++

			return nil, nil
		}

		if err := control(cfg, action, fakeRunner); err != nil {
			t.Errorf("control(%q): %v", action, err)
		}
		if calls == 0 {
			t.Errorf("control(%q): command runner was never invoked", action)
		}
	}
}

// TestDefaultRunner covers both branches of the default command runner: a
// command that succeeds and one that does not exist.
func TestDefaultRunner(t *testing.T) {
	if _, err := defaultRunner("true"); err != nil {
		t.Errorf("defaultRunner(true): %v", err)
	}
	if _, err := defaultRunner("savecraft-no-such-binary-xyz"); err == nil {
		t.Error("defaultRunner(nonexistent): expected an error, got nil")
	}
}
