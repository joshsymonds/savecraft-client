//go:build !windows

package selfupdate

// RestartDaemon is a no-op on non-Windows platforms.
// On Linux, systemd's Restart=always handles restart after os.Exit(0).
func RestartDaemon(_, _ string) error {
	return nil
}
