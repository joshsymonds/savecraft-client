//go:build !windows

package svcmgr

import (
	"context"
	"errors"
	"syscall"
	"testing"
	"time"
)

func TestRun_ShutdownOnSignal(t *testing.T) {
	prog := New(Config{}, func(ctx context.Context) error {
		<-ctx.Done()

		return nil
	})

	go func() {
		time.Sleep(50 * time.Millisecond)
		syscall.Kill(syscall.Getpid(), syscall.SIGINT)
	}()

	err := Run(prog)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestRun_PropagatesRunFuncError(t *testing.T) {
	runErr := errors.New("run failed")
	prog := New(Config{}, func(_ context.Context) error {
		return runErr
	})

	go func() {
		// Wait for the run func to complete before signaling.
		time.Sleep(50 * time.Millisecond)
		syscall.Kill(syscall.Getpid(), syscall.SIGINT)
	}()

	err := Run(prog)
	if !errors.Is(err, runErr) {
		t.Errorf("Run err = %v, want %v", err, runErr)
	}
}
