//go:build windows

package svcmgr

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows/registry"
)

const runKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`

//nolint:unparam // commandRunner parameter required by cross-platform interface
func install(cfg Config, exePath string, _ commandRunner) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("open registry Run key: %w", err)
	}
	defer key.Close()

	// Quote the exe path and append "run" subcommand.
	value := `"` + exePath + `" run`
	if setErr := key.SetStringValue(cfg.DisplayName, value); setErr != nil {
		return fmt.Errorf("set registry Run value: %w", setErr)
	}

	return nil
}

//nolint:unparam // commandRunner parameter required by cross-platform interface
func uninstall(cfg Config, _ commandRunner) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return nil // Key doesn't exist, nothing to uninstall.
	}

	if err != nil {
		return fmt.Errorf("open registry Run key: %w", err)
	}
	defer key.Close()

	if delErr := key.DeleteValue(cfg.DisplayName); delErr != nil && !errors.Is(delErr, registry.ErrNotExist) {
		return fmt.Errorf("delete registry Run value: %w", delErr)
	}

	return nil
}

// startProcess spawns the daemon as a detached process.
// Package-level variable for testability — tests swap this to avoid
// actually launching a process.
var startProcess = func(exePath string) error {
	cmd := exec.Command(exePath, "run")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}

	if startErr := cmd.Start(); startErr != nil {
		return fmt.Errorf("start daemon process: %w", startErr)
	}

	// Release the process handle — this is a fire-and-forget launch.
	// Without Release(), the handle leaks until the Go process exits.
	_ = cmd.Process.Release()

	return nil
}

//nolint:unparam // commandRunner parameter required by cross-platform interface
func serviceStart(_ Config, _ commandRunner) error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}

	return startProcess(exePath)
}

func serviceStop(cfg Config, run commandRunner) error {
	binaryName := cfg.AppName + "-daemon.exe"

	out, err := run("taskkill", "/IM", binaryName, "/F")
	if err != nil {
		return fmt.Errorf("taskkill %s: %s: %w", binaryName, string(out), err)
	}

	return nil
}

//nolint:unparam // commandRunner parameter required by cross-platform interface
func serviceRestart(_ Config, _ commandRunner) error {
	return fmt.Errorf("restart not supported on Windows; stop and start the daemon manually")
}
