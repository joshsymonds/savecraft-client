package selfupdate

import (
	"fmt"
	"path/filepath"
	"strings"
)

// restartTimeoutSeconds is how long Wait-Process will wait for the old daemon
// to exit before falling back to Stop-Process (force-kill).
const restartTimeoutSeconds = 30

// buildRestartScript constructs the PowerShell command string that:
// 1. Kills the old tray process (if trayPath is provided)
// 2. Waits for the old daemon PID to exit (event-based, with timeout)
// 3. Force-kills the old daemon if it hasn't exited after the timeout
// 4. Starts the new daemon
// 5. Starts the new tray (if trayPath is provided).
func buildRestartScript(pid int, daemonPath, trayPath string) string {
	escapedDaemon := psEscape(daemonPath)

	// Wait for old daemon to exit, then start the new one.
	// If Wait-Process times out, force-kill the old PID to free the port.
	script := fmt.Sprintf(
		"$p = Get-Process -Id %d -ErrorAction SilentlyContinue; "+
			"if ($p) { $p | Wait-Process -Timeout %d -ErrorAction SilentlyContinue; "+
			"if (!$p.HasExited) { $p | Stop-Process -Force -ErrorAction SilentlyContinue } }; "+
			"Start-Process -FilePath '%s' -ArgumentList 'run'",
		pid, restartTimeoutSeconds, escapedDaemon,
	)

	if trayPath != "" {
		trayName := strings.TrimSuffix(filepath.Base(trayPath), filepath.Ext(trayPath))
		escapedTray := psEscape(trayPath)
		script = fmt.Sprintf(
			"Stop-Process -Name '%s' -Force -ErrorAction SilentlyContinue; %s; Start-Process -FilePath '%s'",
			psEscape(trayName), script, escapedTray,
		)
	}

	return script
}

// psEscape escapes a string for embedding inside PowerShell single-quoted strings.
// Single quotes are escaped by doubling them (”).
func psEscape(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}
