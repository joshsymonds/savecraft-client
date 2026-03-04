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

	prog.Start()

	// Give the goroutine a moment to launch.
	time.Sleep(10 * time.Millisecond)

	if !started.Load() {
		t.Fatal("run func was not called")
	}

	prog.Stop()
	prog.Wait()

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

	prog.Start()
	prog.Wait()

	if prog.Err() == nil {
		t.Fatal("expected error from run func")
	}
	if !errors.Is(prog.Err(), runErr) {
		t.Errorf("err = %v, want %v", prog.Err(), runErr)
	}
}

func TestProgram_DoubleStopSafe(_ *testing.T) {
	prog := New(Config{
		Name:        "test-daemon",
		DisplayName: "Test",
		Description: "Test",
	}, func(ctx context.Context) error {
		<-ctx.Done()

		return nil
	})

	prog.Start()

	prog.Stop()

	// Second stop should not panic.
	prog.Stop()
}

func TestProgram_Wait(_ *testing.T) {
	done := make(chan struct{})
	prog := New(Config{}, func(_ context.Context) error {
		<-done

		return nil
	})

	prog.Start()

	// Wait should block until the goroutine completes.
	close(done)
	prog.Wait()

	// If we get here, Wait returned after the goroutine finished.
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
