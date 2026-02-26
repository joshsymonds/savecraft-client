package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joshsymonds/savecraft.gg/internal/daemon"
)

func buildPlugin(t *testing.T, pluginDir string) []byte {
	t.Helper()

	tmpDir := t.TempDir()
	wasmPath := filepath.Join(tmpDir, "plugin.wasm")

	cmd := exec.Command("go", "build", "-o", wasmPath, ".")
	cmd.Dir = pluginDir
	cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm")

	output, buildErr := cmd.CombinedOutput()
	if buildErr != nil {
		t.Fatalf("build plugin in %s: %v\n%s", pluginDir, buildErr, output)
	}

	wasm, readErr := os.ReadFile(wasmPath)
	if readErr != nil {
		t.Fatalf("read compiled wasm: %v", readErr)
	}
	return wasm
}

func pluginPath(name string) string {
	return filepath.Join("..", "..", "plugins", name)
}

func TestWazeroRunner_EchoPlugin(t *testing.T) {
	ctx := context.Background()

	runner, err := NewWazeroRunner(ctx)
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	defer runner.Close(ctx)

	wasm := buildPlugin(t, pluginPath("echo"))
	if loadErr := runner.LoadPlugin(ctx, "echo", wasm); loadErr != nil {
		t.Fatalf("load plugin: %v", loadErr)
	}

	var statuses []string
	onStatus := func(msg string) {
		statuses = append(statuses, msg)
	}

	input := []byte("Hammerdin\nLevel 89 Paladin")
	state, runErr := runner.Run(ctx, "echo", input, onStatus)
	if runErr != nil {
		t.Fatalf("run: %v", runErr)
	}

	if state.Identity.CharacterName != "Hammerdin" {
		t.Errorf("character_name = %q, want Hammerdin", state.Identity.CharacterName)
	}
	if state.Identity.GameID != "echo" {
		t.Errorf("game_id = %q, want echo", state.Identity.GameID)
	}
	if state.Summary != "Hammerdin" {
		t.Errorf("summary = %q, want Hammerdin", state.Summary)
	}
	if _, ok := state.Sections["content"]; !ok {
		t.Fatal("missing 'content' section")
	}

	if len(statuses) != 1 {
		t.Fatalf("got %d status messages, want 1", len(statuses))
	}
	if statuses[0] != "Read 26 bytes" {
		t.Errorf("status = %q, want 'Read 26 bytes'", statuses[0])
	}
}

func TestWazeroRunner_EchoPlugin_EmptyInput(t *testing.T) {
	ctx := context.Background()

	runner, err := NewWazeroRunner(ctx)
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	defer runner.Close(ctx)

	wasm := buildPlugin(t, pluginPath("echo"))
	if loadErr := runner.LoadPlugin(ctx, "echo", wasm); loadErr != nil {
		t.Fatalf("load plugin: %v", loadErr)
	}

	state, runErr := runner.Run(ctx, "echo", []byte{}, nil)
	if runErr != nil {
		t.Fatalf("run: %v", runErr)
	}

	if state.Identity.CharacterName != "unnamed" {
		t.Errorf("character_name = %q, want unnamed", state.Identity.CharacterName)
	}
}

func TestWazeroRunner_EchoPlugin_ConcurrentRuns(t *testing.T) {
	ctx := context.Background()

	runner, err := NewWazeroRunner(ctx)
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	defer runner.Close(ctx)

	wasm := buildPlugin(t, pluginPath("echo"))
	if loadErr := runner.LoadPlugin(ctx, "echo", wasm); loadErr != nil {
		t.Fatalf("load plugin: %v", loadErr)
	}

	errc := make(chan error, 3)
	for _, name := range []string{"Alice", "Bob", "Charlie"} {
		go func() {
			state, runErr := runner.Run(ctx, "echo", []byte(name), nil)
			if runErr != nil {
				errc <- runErr
				return
			}
			if state.Identity.CharacterName != name {
				errc <- fmt.Errorf(
					"character_name = %q, want %q",
					state.Identity.CharacterName,
					name,
				)
				return
			}
			errc <- nil
		}()
	}

	for range 3 {
		if chanErr := <-errc; chanErr != nil {
			t.Error(chanErr)
		}
	}
}

func TestWazeroRunner_ErrorPlugin(t *testing.T) {
	ctx := context.Background()

	runner, err := NewWazeroRunner(ctx)
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	defer runner.Close(ctx)

	wasm := buildPlugin(t, pluginPath("error"))
	if loadErr := runner.LoadPlugin(ctx, "error", wasm); loadErr != nil {
		t.Fatalf("load plugin: %v", loadErr)
	}

	_, runErr := runner.Run(ctx, "error", []byte("anything"), nil)
	if runErr == nil {
		t.Fatal("expected error from error plugin")
	}

	var pluginErr *daemon.PluginError
	if !errors.As(runErr, &pluginErr) {
		t.Fatalf("expected *daemon.PluginError, got %T: %v", runErr, runErr)
	}
	if pluginErr.Type != "corrupt_file" {
		t.Errorf("error type = %q, want corrupt_file", pluginErr.Type)
	}
	if pluginErr.Message != "test error from error plugin" {
		t.Errorf("message = %q", pluginErr.Message)
	}
}

func TestWazeroRunner_NoPlugin(t *testing.T) {
	ctx := context.Background()

	runner, err := NewWazeroRunner(ctx)
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	defer runner.Close(ctx)

	_, runErr := runner.Run(ctx, "nonexistent", []byte("data"), nil)
	if runErr == nil {
		t.Error("expected error for missing plugin")
	}
}

func TestWazeroRunner_InvalidWasm(t *testing.T) {
	ctx := context.Background()

	runner, err := NewWazeroRunner(ctx)
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	defer runner.Close(ctx)

	loadErr := runner.LoadPlugin(ctx, "bad", []byte("not valid wasm"))
	if loadErr == nil {
		t.Error("expected error for invalid wasm bytes")
	}
}

func TestWazeroRunner_NoopPlugin_NoResult(t *testing.T) {
	ctx := context.Background()

	runner, err := NewWazeroRunner(ctx)
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	defer runner.Close(ctx)

	wasm := buildPlugin(t, pluginPath("noop"))
	if loadErr := runner.LoadPlugin(ctx, "noop", wasm); loadErr != nil {
		t.Fatalf("load plugin: %v", loadErr)
	}

	_, runErr := runner.Run(ctx, "noop", []byte("data"), nil)
	if runErr == nil {
		t.Fatal("expected error for plugin with no result")
	}
	if !strings.Contains(runErr.Error(), "no result") {
		t.Errorf("error = %q, want to contain 'no result'", runErr.Error())
	}
}

func TestWazeroRunner_CrashPlugin_NonZeroExit(t *testing.T) {
	ctx := context.Background()

	runner, err := NewWazeroRunner(ctx)
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	defer runner.Close(ctx)

	wasm := buildPlugin(t, pluginPath("crash"))
	if loadErr := runner.LoadPlugin(ctx, "crash", wasm); loadErr != nil {
		t.Fatalf("load plugin: %v", loadErr)
	}

	_, runErr := runner.Run(ctx, "crash", []byte("data"), nil)
	if runErr == nil {
		t.Fatal("expected error from crashing plugin")
	}
	if !strings.Contains(runErr.Error(), "plugin execution failed") {
		t.Errorf("error = %q, want to contain 'plugin execution failed'", runErr.Error())
	}
	if !strings.Contains(runErr.Error(), "something went wrong") {
		t.Errorf("error = %q, want to contain stderr output", runErr.Error())
	}
}

func TestParsePluginOutput_SkipsInvalidJSON(t *testing.T) {
	runner := &WazeroRunner{}

	input := strings.NewReader(
		"not json\n" +
			`{"type":"result","identity":{"characterName":"Test","gameId":"t"},"summary":"Test","sections":{}}` + "\n",
	)

	state, err := runner.parsePluginOutput(input, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state == nil {
		t.Fatal("expected result")
	}
	if state.Identity.CharacterName != "Test" {
		t.Errorf("name = %q, want Test", state.Identity.CharacterName)
	}
}

func TestParsePluginOutput_EmptyInput(t *testing.T) {
	runner := &WazeroRunner{}

	state, err := runner.parsePluginOutput(strings.NewReader(""), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state != nil {
		t.Errorf("expected nil state, got %+v", state)
	}
}

func TestParsePluginOutput_StatusForwarding(t *testing.T) {
	runner := &WazeroRunner{}

	var statuses []string
	input := strings.NewReader(
		`{"type":"status","message":"step 1"}` + "\n" +
			`{"type":"status","message":"step 2"}` + "\n",
	)

	state, err := runner.parsePluginOutput(input, func(msg string) {
		statuses = append(statuses, msg)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state != nil {
		t.Errorf("expected nil state")
	}
	if len(statuses) != 2 {
		t.Fatalf("got %d statuses, want 2", len(statuses))
	}
	if statuses[0] != "step 1" || statuses[1] != "step 2" {
		t.Errorf("statuses = %v", statuses)
	}
}
