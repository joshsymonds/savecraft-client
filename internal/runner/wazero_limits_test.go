package runner

import (
	"context"
	"testing"
	"time"
)

// echoStillWorks proves the runner (and thus the long-lived daemon) survives a
// hostile plugin: a normal plugin must still parse on the same runner.
func echoStillWorks(ctx context.Context, t *testing.T, runner *WazeroRunner) {
	t.Helper()
	wasm := buildPlugin(t, pluginPath("echo"))
	if err := runner.LoadPlugin(ctx, "echo", wasm, nil); err != nil {
		t.Fatalf("load echo after hostile plugin: %v", err)
	}
	state, err := runner.Run(ctx, "echo", "test.sav", []byte("Alice\nx"), nil)
	if err != nil {
		t.Fatalf("echo run after hostile plugin: %v", err)
	}
	if state == nil || state.Identity.SaveName != "Alice" {
		t.Fatalf("echo produced unexpected state: %+v", state)
	}
}

func TestWazeroRunner_InfiniteLoop_TimesOut(t *testing.T) {
	ctx := context.Background()

	runner, err := NewWazeroRunner(ctx, WithParseTimeout(1*time.Second))
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	defer runner.Close(ctx)

	wasm := buildPlugin(t, pluginPath("loop"))
	if err := runner.LoadPlugin(ctx, "loop", wasm, nil); err != nil {
		t.Fatalf("load loop: %v", err)
	}

	start := time.Now()
	_, runErr := runner.Run(ctx, "loop", "x.sav", []byte("data"), nil)
	elapsed := time.Since(start)

	if runErr == nil {
		t.Fatal("expected an error from a non-terminating plugin")
	}
	if elapsed > 15*time.Second {
		t.Fatalf("Run took %v — timeout did not unwind the guest", elapsed)
	}

	// Daemon must remain usable for a subsequent parse.
	echoStillWorks(ctx, t, runner)
}

func TestWazeroRunner_MemoryHog_FailsGracefully(t *testing.T) {
	ctx := context.Background()

	// 2048 pages = 128 MiB: comfortably fits the tiny echo plugin + Go wasm
	// runtime, far below what the hog plugin allocates.
	runner, err := NewWazeroRunner(ctx, WithMemoryLimitPages(2048))
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	defer runner.Close(ctx)

	wasm := buildPlugin(t, pluginPath("hog"))
	if err := runner.LoadPlugin(ctx, "hog", wasm, nil); err != nil {
		t.Fatalf("load hog: %v", err)
	}

	_, runErr := runner.Run(ctx, "hog", "x.sav", []byte("data"), nil)
	if runErr == nil {
		t.Fatal("expected an error when plugin exceeds the memory cap")
	}

	// The over-allocation must not have killed the daemon.
	echoStillWorks(ctx, t, runner)
}

func TestWazeroRunner_ContextCancel_AbortsPromptly(t *testing.T) {
	ctx := context.Background()

	// Long parse timeout so it's the parent-context cancel — not the timeout —
	// that aborts the run.
	runner, err := NewWazeroRunner(ctx, WithParseTimeout(60*time.Second))
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	defer runner.Close(ctx)

	wasm := buildPlugin(t, pluginPath("loop"))
	if err := runner.LoadPlugin(ctx, "loop", wasm, nil); err != nil {
		t.Fatalf("load loop: %v", err)
	}

	runCtx, cancel := context.WithCancel(ctx)
	go func() {
		time.Sleep(1 * time.Second)
		cancel()
	}()

	start := time.Now()
	_, runErr := runner.Run(runCtx, "loop", "x.sav", []byte("data"), nil)
	elapsed := time.Since(start)

	if runErr == nil {
		t.Fatal("expected an error when parent context is canceled")
	}
	if elapsed > 15*time.Second {
		t.Fatalf("Run took %v after cancel — guest was not unwound", elapsed)
	}
}
