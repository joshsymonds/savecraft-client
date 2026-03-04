//go:build windows

package svcmgr

import (
	"os"
	"strings"
	"testing"

	"golang.org/x/sys/windows/registry"
)

const testDisplayName = "Savecraft Test Daemon"

// cleanupRegistryValue removes the test registry value, ignoring errors.
func cleanupRegistryValue(t *testing.T) {
	t.Helper()

	key, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return
	}
	defer key.Close()

	_ = key.DeleteValue(testDisplayName)
}

func TestInstall_CreatesRegistryValue(t *testing.T) {
	t.Cleanup(func() { cleanupRegistryValue(t) })

	cfg := Config{
		Name:        "savecraft-test-daemon",
		DisplayName: testDisplayName,
		Description: "Test service",
		AppName:     "savecraft-test",
	}
	exePath := `C:\Program Files\Savecraft\savecraftd.exe`

	err := install(cfg, exePath, nil)
	if err != nil {
		t.Fatalf("install: %v", err)
	}

	// Read back and verify.
	key, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		t.Fatalf("open registry key for read: %v", err)
	}
	defer key.Close()

	value, _, err := key.GetStringValue(testDisplayName)
	if err != nil {
		t.Fatalf("get registry value: %v", err)
	}

	want := `"C:\Program Files\Savecraft\savecraftd.exe" run`
	if value != want {
		t.Errorf("registry value = %q, want %q", value, want)
	}
}

func TestUninstall_RemovesRegistryValue(t *testing.T) {
	t.Cleanup(func() { cleanupRegistryValue(t) })

	cfg := Config{
		Name:        "savecraft-test-daemon",
		DisplayName: testDisplayName,
		AppName:     "savecraft-test",
	}

	// Pre-create the value.
	key, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		t.Fatalf("open registry key: %v", err)
	}

	if setErr := key.SetStringValue(testDisplayName, "test-value"); setErr != nil {
		key.Close()
		t.Fatalf("set registry value: %v", setErr)
	}

	key.Close()

	// Uninstall should remove it.
	if err := uninstall(cfg, nil); err != nil {
		t.Fatalf("uninstall: %v", err)
	}

	// Verify it's gone.
	readKey, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		t.Fatalf("open registry key for read: %v", err)
	}
	defer readKey.Close()

	_, _, err = readKey.GetStringValue(testDisplayName)
	if err != registry.ErrNotExist {
		t.Errorf("expected ErrNotExist after uninstall, got %v", err)
	}
}

func TestUninstall_IdempotentWhenMissing(t *testing.T) {
	// Ensure value doesn't exist.
	cleanupRegistryValue(t)

	cfg := Config{
		Name:        "savecraft-test-daemon",
		DisplayName: testDisplayName,
		AppName:     "savecraft-test",
	}

	if err := uninstall(cfg, nil); err != nil {
		t.Fatalf("uninstall (idempotent): %v", err)
	}
}

func TestServiceStop_CallsTaskkill(t *testing.T) {
	var commands []string
	fakeRunner := func(name string, args ...string) ([]byte, error) {
		commands = append(commands, name+" "+strings.Join(args, " "))

		return nil, nil
	}

	cfg := Config{AppName: "savecraft-test"}

	if err := serviceStop(cfg, fakeRunner); err != nil {
		t.Fatalf("serviceStop: %v", err)
	}

	if len(commands) != 1 {
		t.Fatalf("expected 1 command, got %d: %v", len(commands), commands)
	}

	want := "taskkill /IM savecraft-test-daemon.exe /F"
	if commands[0] != want {
		t.Errorf("command = %q, want %q", commands[0], want)
	}
}

func TestServiceStop_PropagatesError(t *testing.T) {
	fakeRunner := func(_ string, _ ...string) ([]byte, error) {
		return []byte("process not found"), os.ErrProcessDone
	}

	cfg := Config{AppName: "savecraft-test"}

	err := serviceStop(cfg, fakeRunner)
	if err == nil {
		t.Fatal("expected error")
	}

	if !strings.Contains(err.Error(), "process not found") {
		t.Errorf("error should contain command output: %v", err)
	}
}

func TestServiceStart_CallsStartProcess(t *testing.T) {
	var captured string

	old := startProcess
	startProcess = func(exePath string) error {
		captured = exePath

		return nil
	}

	t.Cleanup(func() { startProcess = old })

	if err := serviceStart(Config{}, nil); err != nil {
		t.Fatalf("serviceStart: %v", err)
	}

	if captured == "" {
		t.Error("startProcess was not called")
	}
}

func TestServiceRestart_NotSupported(t *testing.T) {
	err := serviceRestart(Config{}, nil)
	if err == nil {
		t.Fatal("expected error")
	}

	if !strings.Contains(err.Error(), "not supported") {
		t.Errorf("error = %q, want to contain 'not supported'", err.Error())
	}
}

func TestShutdownSignals_InterruptOnly(t *testing.T) {
	signals := shutdownSignals()

	if len(signals) != 1 {
		t.Fatalf("expected 1 signal, got %d", len(signals))
	}

	if signals[0] != os.Interrupt {
		t.Errorf("signal = %v, want os.Interrupt", signals[0])
	}
}
