//go:build windows

package selfupdate

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// RestartDaemon spawns a detached PowerShell process that waits for the current
// daemon to exit, then starts the new daemon (and optionally the tray app).
// On Windows there is no systemd, so we use Wait-Process for event-based process
// exit detection before launching the replacement.
func RestartDaemon(daemonPath, trayPath string) error {
	script := buildRestartScript(os.Getpid(), daemonPath, trayPath)

	cmd := exec.Command("powershell.exe", "-NoProfile", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("restart daemon: %w", err)
	}
	// Detach: let the PowerShell process outlive this daemon.
	_ = cmd.Process.Release()
	return nil
}
