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
	"github.com/joshsymonds/savecraft.gg/internal/signing"
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
	if loadErr := runner.LoadPlugin(ctx, "echo", wasm, nil); loadErr != nil {
		t.Fatalf("load plugin: %v", loadErr)
	}

	var statuses []string
	onStatus := func(msg string) {
		statuses = append(statuses, msg)
	}

	input := []byte("Hammerdin\nLevel 89 Paladin")
	state, runErr := runner.Run(ctx, "echo", "test.sav", input, onStatus)
	if runErr != nil {
		t.Fatalf("run: %v", runErr)
	}

	if state.Identity.SaveName != "Hammerdin" {
		t.Errorf("save_name = %q, want Hammerdin", state.Identity.SaveName)
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
	if loadErr := runner.LoadPlugin(ctx, "echo", wasm, nil); loadErr != nil {
		t.Fatalf("load plugin: %v", loadErr)
	}

	state, runErr := runner.Run(ctx, "echo", "test.sav", []byte{}, nil)
	if runErr != nil {
		t.Fatalf("run: %v", runErr)
	}

	if state.Identity.SaveName != "unnamed" {
		t.Errorf("save_name = %q, want unnamed", state.Identity.SaveName)
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
	if loadErr := runner.LoadPlugin(ctx, "echo", wasm, nil); loadErr != nil {
		t.Fatalf("load plugin: %v", loadErr)
	}

	errc := make(chan error, 3)
	for _, name := range []string{"Alice", "Bob", "Charlie"} {
		go func() {
			state, runErr := runner.Run(ctx, "echo", "test.sav", []byte(name), nil)
			if runErr != nil {
				errc <- runErr
				return
			}
			if state.Identity.SaveName != name {
				errc <- fmt.Errorf(
					"save_name = %q, want %q",
					state.Identity.SaveName,
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
	if loadErr := runner.LoadPlugin(ctx, "error", wasm, nil); loadErr != nil {
		t.Fatalf("load plugin: %v", loadErr)
	}

	_, runErr := runner.Run(ctx, "error", "test.sav", []byte("anything"), nil)
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

	_, runErr := runner.Run(ctx, "nonexistent", "test.sav", []byte("data"), nil)
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

	loadErr := runner.LoadPlugin(ctx, "bad", []byte("not valid wasm"), nil)
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
	if loadErr := runner.LoadPlugin(ctx, "noop", wasm, nil); loadErr != nil {
		t.Fatalf("load plugin: %v", loadErr)
	}

	_, runErr := runner.Run(ctx, "noop", "test.sav", []byte("data"), nil)
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
	if loadErr := runner.LoadPlugin(ctx, "crash", wasm, nil); loadErr != nil {
		t.Fatalf("load plugin: %v", loadErr)
	}

	_, runErr := runner.Run(ctx, "crash", "test.sav", []byte("data"), nil)
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
			`{"type":"result","identity":{"saveName":"Test","gameId":"t"},"summary":"Test","sections":{}}` + "\n",
	)

	state, err := runner.parsePluginOutput(input, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state == nil {
		t.Fatal("expected result")
	}
	if state.Identity.SaveName != "Test" {
		t.Errorf("name = %q, want Test", state.Identity.SaveName)
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

func TestLoadPlugin_VerificationSuccess(t *testing.T) {
	ctx := context.Background()
	pub, priv, err := signing.GenerateKeypair()
	if err != nil {
		t.Fatalf("generate keypair: %v", err)
	}

	runner, err := NewWazeroRunner(ctx, WithVerifier(pub))
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	defer runner.Close(ctx)

	wasm := buildPlugin(t, pluginPath("echo"))
	sig := signing.Sign(priv, wasm)

	if loadErr := runner.LoadPlugin(ctx, "echo", wasm, sig); loadErr != nil {
		t.Fatalf("load plugin with valid sig: %v", loadErr)
	}
}

func TestLoadPlugin_VerificationFailure_Tampered(t *testing.T) {
	ctx := context.Background()
	pub, priv, err := signing.GenerateKeypair()
	if err != nil {
		t.Fatalf("generate keypair: %v", err)
	}

	runner, err := NewWazeroRunner(ctx, WithVerifier(pub))
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	defer runner.Close(ctx)

	wasm := buildPlugin(t, pluginPath("echo"))
	sig := signing.Sign(priv, wasm)

	// Flip a byte in the wasm.
	tampered := make([]byte, len(wasm))
	copy(tampered, wasm)
	tampered[0] ^= 0xff

	loadErr := runner.LoadPlugin(ctx, "echo", tampered, sig)
	if loadErr == nil {
		t.Fatal("expected error for tampered wasm")
	}
	if !strings.Contains(loadErr.Error(), "verify plugin") {
		t.Errorf("error = %q, want to contain 'verify plugin'", loadErr)
	}
}

func TestLoadPlugin_VerificationFailure_NilSig(t *testing.T) {
	ctx := context.Background()
	pub, _, err := signing.GenerateKeypair()
	if err != nil {
		t.Fatalf("generate keypair: %v", err)
	}

	runner, err := NewWazeroRunner(ctx, WithVerifier(pub))
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	defer runner.Close(ctx)

	wasm := buildPlugin(t, pluginPath("echo"))

	loadErr := runner.LoadPlugin(ctx, "echo", wasm, nil)
	if loadErr == nil {
		t.Fatal("expected error for nil sig with verifier")
	}
	if !strings.Contains(loadErr.Error(), "verify plugin") {
		t.Errorf("error = %q, want to contain 'verify plugin'", loadErr)
	}
}

func TestLoadPlugin_NoVerifier_NilSigOk(t *testing.T) {
	ctx := context.Background()

	runner, err := NewWazeroRunner(ctx)
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	defer runner.Close(ctx)

	wasm := buildPlugin(t, pluginPath("echo"))

	if loadErr := runner.LoadPlugin(ctx, "echo", wasm, nil); loadErr != nil {
		t.Fatalf("load plugin without verifier: %v", loadErr)
	}
}
