package svcmgr

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestProgram_StartStop(t *testing.T) {
	var started atomic.Bool
	var stopped atomic.Bool

	prog := New(Config{
		Name:        "test-daemon",
		DisplayName: "Test Daemon",
		Description: "A test daemon",
	}, func(ctx context.Context) error {
		started.Store(true)
		<-ctx.Done()
		stopped.Store(true)

		return nil
	})

	if err := prog.Start(nil); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Give the goroutine a moment to launch.
	time.Sleep(10 * time.Millisecond)

	if !started.Load() {
		t.Fatal("run func was not called")
	}

	if err := prog.Stop(nil); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Give the goroutine a moment to finish.
	time.Sleep(10 * time.Millisecond)

	if !stopped.Load() {
		t.Fatal("run func did not observe context cancellation")
	}
}

func TestProgram_RunFuncError(t *testing.T) {
	runErr := errors.New("daemon crashed")
	prog := New(Config{
		Name:        "test-daemon",
		DisplayName: "Test Daemon",
		Description: "A test daemon",
	}, func(_ context.Context) error {
		return runErr
	})

	if err := prog.Start(nil); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Wait for the run func to complete and error to propagate.
	time.Sleep(50 * time.Millisecond)

	if prog.Err() == nil {
		t.Fatal("expected error from run func")
	}
	if !errors.Is(prog.Err(), runErr) {
		t.Errorf("err = %v, want %v", prog.Err(), runErr)
	}
}

func TestProgram_ServiceConfig(t *testing.T) {
	prog := New(Config{
		Name:        "savecraft",
		DisplayName: "Savecraft Daemon",
		Description: "Syncs game saves",
	}, func(_ context.Context) error { return nil })

	cfg := prog.ServiceConfig()
	if cfg.Name != "savecraft" {
		t.Errorf("name = %q, want %q", cfg.Name, "savecraft")
	}
	if cfg.DisplayName != "Savecraft Daemon" {
		t.Errorf("displayName = %q, want %q", cfg.DisplayName, "Savecraft Daemon")
	}
	if cfg.Description != "Syncs game saves" {
		t.Errorf("description = %q, want %q", cfg.Description, "Syncs game saves")
	}
	if len(cfg.Arguments) != 1 || cfg.Arguments[0] != "run" {
		t.Errorf("arguments = %v, want [run]", cfg.Arguments)
	}
}

func TestProgram_DoubleStopSafe(t *testing.T) {
	prog := New(Config{
		Name:        "test-daemon",
		DisplayName: "Test",
		Description: "Test",
	}, func(ctx context.Context) error {
		<-ctx.Done()

		return nil
	})

	if err := prog.Start(nil); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if err := prog.Stop(nil); err != nil {
		t.Fatalf("Stop 1: %v", err)
	}

	// Second stop should not panic.
	if err := prog.Stop(nil); err != nil {
		t.Fatalf("Stop 2: %v", err)
	}
}
