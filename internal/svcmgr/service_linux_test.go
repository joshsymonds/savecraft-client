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
