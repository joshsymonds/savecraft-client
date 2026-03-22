//go:build windows

package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

const daemonBinaryName = "savecraft-daemon.exe"

// buildStartDaemonFunc returns a function that spawns the daemon as a detached
// process. The daemon binary is located relative to the tray binary (same directory).
func buildStartDaemonFunc(logger *slog.Logger) func() error {
	return func() error {
		trayPath, err := os.Executable()
		if err != nil {
			return fmt.Errorf("resolve tray path: %w", err)
		}

		daemonPath := filepath.Join(filepath.Dir(trayPath), daemonBinaryName)
		if _, statErr := os.Stat(daemonPath); statErr != nil {
			return fmt.Errorf("daemon binary not found: %w", statErr)
		}

		logger.Info("supervisor: spawning daemon", slog.String("path", daemonPath))

		cmd := exec.Command(daemonPath, "run")
		cmd.SysProcAttr = &syscall.SysProcAttr{
			CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
		}

		if startErr := cmd.Start(); startErr != nil {
			return fmt.Errorf("start daemon: %w", startErr)
		}

		_ = cmd.Process.Release()

		return nil
	}
}
