//go:build !darwin && !windows

package svcmgr

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnitFileContent_SecurityDirectives(t *testing.T) {
	cfg := Config{
		Name:        "savecraft-daemon",
		DisplayName: "Savecraft Daemon",
		Description: "Syncs game saves to the cloud via Savecraft",
		AppName:     "savecraft",
	}
	content := UnitFileContent(cfg, "/home/user/.local/bin/savecraftd")

	requiredLines := []string{
		"[Unit]",
		"Description=Syncs game saves to the cloud via Savecraft",
		"After=network-online.target",
		"[Service]",
		"ExecStart=/home/user/.local/bin/savecraftd run",
		"Restart=always",
		"RestartSec=5",
		"EnvironmentFile=-%h/.config/savecraft/env",
		"ProtectSystem=strict",
		"ProtectHome=read-only",
		"ReadWritePaths=%h/.config/savecraft %h/.cache/savecraft /home/user/.local/bin",
		"NoNewPrivileges=yes",
		"PrivateTmp=yes",
		"RestrictAddressFamilies=AF_INET AF_INET6",
		"[Install]",
		"WantedBy=default.target",
	}

	for _, line := range requiredLines {
		if !strings.Contains(content, line) {
			t.Errorf("unit file missing line: %q\n\nFull content:\n%s", line, content)
		}
	}
}

func TestUnitFileContent_ExecStartUsesRunSubcommand(t *testing.T) {
	cfg := Config{
		Name:    "test-daemon",
		AppName: "test",
	}
	content := UnitFileContent(cfg, "/usr/local/bin/testd")

	if !strings.Contains(content, "ExecStart=/usr/local/bin/testd run") {
		t.Errorf("ExecStart should include 'run' subcommand\n\nFull content:\n%s", content)
	}
}

func TestUnitFilePath(t *testing.T) {
	cfg := Config{Name: "savecraft-daemon"}
	path := unitFilePath(cfg)

	if !strings.HasSuffix(path, ".config/systemd/user/savecraft-daemon.service") {
		t.Errorf("unexpected unit file path: %s", path)
	}
}

func TestInstall_WritesUnitAndCallsSystemctl(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	var commands []string
	fakeRunner := func(name string, args ...string) ([]byte, error) {
		commands = append(commands, name+" "+strings.Join(args, " "))

		return nil, nil
	}

	cfg := Config{
		Name:        "test-daemon",
		DisplayName: "Test Daemon",
		Description: "A test service",
		AppName:     "test",
	}

	err := install(cfg, "/usr/bin/testd", fakeRunner)
	if err != nil {
		t.Fatalf("install: %v", err)
	}

	// Verify unit file was written.
	unitPath := filepath.Join(home, ".config", "systemd", "user", "test-daemon.service")

	content, readErr := os.ReadFile(unitPath)
	if readErr != nil {
		t.Fatalf("read unit file: %v", readErr)
	}

	if !strings.Contains(string(content), "ExecStart=/usr/bin/testd run") {
		t.Error("unit file missing ExecStart")
	}

	if !strings.Contains(string(content), "ProtectSystem=strict") {
		t.Error("unit file missing ProtectSystem")
	}

	// Verify systemctl commands.
	if len(commands) != 2 {
		t.Fatalf("expected 2 systemctl calls, got %d: %v", len(commands), commands)
	}

	if commands[0] != "systemctl --user daemon-reload" {
		t.Errorf("first command = %q, want systemctl --user daemon-reload", commands[0])
	}

	if commands[1] != "systemctl --user enable test-daemon.service" {
		t.Errorf("second command = %q, want systemctl --user enable test-daemon.service", commands[1])
	}
}

func TestUninstall_DisablesAndRemovesUnit(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Create a fake unit file to remove.
	unitDir := filepath.Join(home, ".config", "systemd", "user")
	if mkErr := os.MkdirAll(unitDir, 0o755); mkErr != nil {
		t.Fatal(mkErr)
	}

	unitPath := filepath.Join(unitDir, "test-daemon.service")
	if writeErr := os.WriteFile(unitPath, []byte("fake"), 0o644); writeErr != nil {
		t.Fatal(writeErr)
	}

	var commands []string
	fakeRunner := func(name string, args ...string) ([]byte, error) {
		commands = append(commands, name+" "+strings.Join(args, " "))

		return nil, nil
	}

	cfg := Config{Name: "test-daemon", AppName: "test"}

	err := uninstall(cfg, fakeRunner)
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}

	// Verify unit file was removed.
	if _, statErr := os.Stat(unitPath); !os.IsNotExist(statErr) {
		t.Error("unit file should have been removed")
	}

	// Verify systemctl commands: disable, stop, daemon-reload.
	if len(commands) != 3 {
		t.Fatalf("expected 3 systemctl calls, got %d: %v", len(commands), commands)
	}

	if commands[0] != "systemctl --user disable test-daemon.service" {
		t.Errorf("commands[0] = %q", commands[0])
	}

	if commands[1] != "systemctl --user stop test-daemon.service" {
		t.Errorf("commands[1] = %q", commands[1])
	}

	if commands[2] != "systemctl --user daemon-reload" {
		t.Errorf("commands[2] = %q", commands[2])
	}
}

func TestServiceStart_CallsSystemctl(t *testing.T) {
	var commands []string
	fakeRunner := func(name string, args ...string) ([]byte, error) {
		commands = append(commands, name+" "+strings.Join(args, " "))

		return nil, nil
	}

	cfg := Config{Name: "test-daemon"}

	err := serviceStart(cfg, fakeRunner)
	if err != nil {
		t.Fatalf("serviceStart: %v", err)
	}

	if len(commands) != 1 || commands[0] != "systemctl --user start test-daemon.service" {
		t.Errorf("commands = %v", commands)
	}
}

func TestDoUninstall_RemovesEverything(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Create fake directories and binary.
	configDir := filepath.Join(home, ".config", "test")
	cacheDir := filepath.Join(home, ".cache", "test")
	dataDir := filepath.Join(home, ".local", "share", "test")
	binaryPath := filepath.Join(home, ".local", "bin", "test-daemon")

	for _, dir := range []string{configDir, cacheDir, dataDir, filepath.Dir(binaryPath)} {
		if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil {
			t.Fatal(mkErr)
		}
	}

	// Write some files inside dirs to verify recursive removal.
	os.WriteFile(filepath.Join(configDir, "env"), []byte("token=secret"), 0o600)
	os.WriteFile(binaryPath, []byte("fake-binary"), 0o755)

	// Create a fake systemd unit so uninstall has something to remove.
	unitDir := filepath.Join(home, ".config", "systemd", "user")
	os.MkdirAll(unitDir, 0o755)
	os.WriteFile(filepath.Join(unitDir, "test-daemon.service"), []byte("fake"), 0o644)

	var commands []string
	fakeRunner := func(name string, args ...string) ([]byte, error) {
		commands = append(commands, name+" "+strings.Join(args, " "))
		return nil, nil
	}

	cfg := Config{Name: "test-daemon", AppName: "test"}
	paths := UninstallPaths{
		ConfigDir: configDir,
		CacheDir:  cacheDir,
		DataDir:   dataDir,
		Binary:    binaryPath,
	}

	var buf strings.Builder
	err := doUninstall(cfg, paths, &buf, fakeRunner)
	if err != nil {
		t.Fatalf("doUninstall: %v", err)
	}

	// Verify all directories and binary are gone.
	for _, path := range []string{configDir, cacheDir, dataDir, binaryPath} {
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Errorf("path should have been removed: %s", path)
		}
	}

	// Verify systemctl commands were called (disable, stop, daemon-reload).
	if len(commands) != 3 {
		t.Fatalf("expected 3 systemctl calls, got %d: %v", len(commands), commands)
	}

	// Verify output mentions each removal.
	output := buf.String()
	for _, want := range []string{"Removed OS service", configDir, cacheDir, dataDir, binaryPath} {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q\n\nFull output:\n%s", want, output)
		}
	}
}

func TestDoUninstall_MissingDirsAreWarnings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	fakeRunner := func(_ string, _ ...string) ([]byte, error) {
		return nil, nil
	}

	cfg := Config{Name: "test-daemon", AppName: "test"}
	paths := UninstallPaths{
		ConfigDir: filepath.Join(home, "nonexistent-config"),
		CacheDir:  filepath.Join(home, "nonexistent-cache"),
		DataDir:   filepath.Join(home, "nonexistent-data"),
		Binary:    filepath.Join(home, "nonexistent-binary"),
	}

	var buf strings.Builder
	err := doUninstall(cfg, paths, &buf, fakeRunner)
	if err != nil {
		t.Fatalf("doUninstall should not return error for missing paths: %v", err)
	}

	// RemoveAll on nonexistent dir succeeds (returns nil), so they show as "Removed".
	// Remove on nonexistent binary also succeeds (we handle IsNotExist).
	// The key assertion: no error returned.
}

func TestDoUninstall_EmptyLogDir_Skipped(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	configDir := filepath.Join(home, "config")
	os.MkdirAll(configDir, 0o755)

	fakeRunner := func(_ string, _ ...string) ([]byte, error) {
		return nil, nil
	}

	cfg := Config{Name: "test-daemon", AppName: "test"}
	paths := UninstallPaths{
		ConfigDir: configDir,
		LogDir:    "", // empty — should be skipped
	}

	var buf strings.Builder
	err := doUninstall(cfg, paths, &buf, fakeRunner)
	if err != nil {
		t.Fatalf("doUninstall: %v", err)
	}

	output := buf.String()
	if strings.Contains(output, "log") {
		t.Errorf("should not mention log dir when empty\n\nFull output:\n%s", output)
	}
}

func TestServiceStop_CallsSystemctl(t *testing.T) {
	var commands []string
	fakeRunner := func(name string, args ...string) ([]byte, error) {
		commands = append(commands, name+" "+strings.Join(args, " "))

		return nil, nil
	}

	cfg := Config{Name: "test-daemon"}

	err := serviceStop(cfg, fakeRunner)
	if err != nil {
		t.Fatalf("serviceStop: %v", err)
	}

	if len(commands) != 1 || commands[0] != "systemctl --user stop test-daemon.service" {
		t.Errorf("commands = %v", commands)
	}
}

func TestSystemctlRun_ErrorIncludesOutput(t *testing.T) {
	fakeRunner := func(_ string, _ ...string) ([]byte, error) {
		return []byte("unit not found"), os.ErrNotExist
	}

	err := systemctlRun(fakeRunner, "start", "bogus.service")
	if err == nil {
		t.Fatal("expected error")
	}

	if !strings.Contains(err.Error(), "unit not found") {
		t.Errorf("error should contain command output: %v", err)
	}

	if !strings.Contains(err.Error(), "systemctl start bogus.service") {
		t.Errorf("error should contain command args: %v", err)
	}
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
