//go:build windows

package selfupdate

import (
	"fmt"
	"os/exec"
	"syscall"
)

// RestartDaemon spawns a new daemon process from binaryPath and detaches it.
// On Windows there is no systemd to restart the process, so we spawn a new
// one in a separate process group before the current process exits.
func RestartDaemon(binaryPath string) error {
	cmd := exec.Command(binaryPath, "run")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("restart daemon: %w", err)
	}
	// Detach: let the child outlive this process.
	_ = cmd.Process.Release()
	return nil
}
