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
