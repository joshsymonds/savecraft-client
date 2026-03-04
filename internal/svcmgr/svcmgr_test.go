package svcmgr

import (
	"context"
	"errors"
	"sync/atomic"
	"syscall"
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

func TestControl_Restart(t *testing.T) {
	var called bool
	fake := func(name string, args ...string) ([]byte, error) {
		called = true
		if name != "systemctl" {
			t.Errorf("name = %q, want systemctl", name)
		}
		// Expect: systemctl --user restart test-daemon.service
		wantArgs := []string{"--user", "restart", "test-daemon.service"}
		for i, want := range wantArgs {
			if i >= len(args) || args[i] != want {
				t.Errorf("args[%d] = %q, want %q", i, args[i], want)
			}
		}

		return nil, nil
	}

	cfg := Config{Name: "test-daemon"}
	if err := control(cfg, "restart", fake); err != nil {
		t.Fatalf("control restart: %v", err)
	}
	if !called {
		t.Fatal("command runner was not called")
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
