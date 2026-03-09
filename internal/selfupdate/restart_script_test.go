package selfupdate

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildRestartScript_DaemonOnly(t *testing.T) {
	daemonPath := filepath.Join("C:", "Savecraft", "savecraft-daemon.exe")
	script := buildRestartScript(1234, daemonPath, "")

	if !strings.Contains(script, "Get-Process -Id 1234") {
		t.Error("script should wait for PID 1234")
	}
	if !strings.Contains(script, "Wait-Process -Timeout") {
		t.Error("script should include Wait-Process with timeout")
	}
	if !strings.Contains(script, "Stop-Process -Force") {
		t.Error("script should include force-kill fallback")
	}
	if !strings.Contains(script, "-ArgumentList 'run'") {
		t.Error("script should start daemon with 'run' argument")
	}
	// Should NOT contain tray stop when trayPath is empty.
	if strings.Contains(script, "Stop-Process -Name") {
		t.Error("script should NOT include tray stop when trayPath is empty")
	}
}

func TestBuildRestartScript_DaemonAndTray(t *testing.T) {
	daemonPath := filepath.Join("C:", "Savecraft", "savecraft-daemon.exe")
	trayPath := filepath.Join("C:", "Savecraft", "savecraft-tray.exe")
	script := buildRestartScript(5678, daemonPath, trayPath)

	if !strings.Contains(script, "Stop-Process -Name 'savecraft-tray'") {
		t.Errorf("script should kill old tray by process name; got: %s", script)
	}
	if !strings.Contains(script, "Start-Process -FilePath '"+trayPath+"'") {
		t.Errorf("script should start new tray; got: %s", script)
	}

	// Verify ordering: Stop tray comes before Wait daemon comes before Start daemon comes before Start tray.
	stopIdx := strings.Index(script, "Stop-Process -Name")
	waitIdx := strings.Index(script, "Get-Process -Id 5678")
	startDaemonIdx := strings.Index(script, "Start-Process -FilePath '"+daemonPath+"'")
	startTrayIdx := strings.Index(script, "Start-Process -FilePath '"+trayPath+"'")

	if stopIdx >= waitIdx {
		t.Error("tray stop should come before daemon wait")
	}
	if waitIdx >= startDaemonIdx {
		t.Error("daemon wait should come before daemon start")
	}
	if startDaemonIdx >= startTrayIdx {
		t.Error("daemon start should come before tray start")
	}
}

func TestBuildRestartScript_PathsWithSpaces(t *testing.T) {
	daemonPath := filepath.Join("C:", "Program Files", "Savecraft", "savecraft-daemon.exe")
	trayPath := filepath.Join("C:", "Program Files", "Savecraft", "savecraft-tray.exe")
	script := buildRestartScript(42, daemonPath, trayPath)

	if !strings.Contains(script, "'"+daemonPath+"'") {
		t.Errorf("daemon path with spaces should be preserved in single quotes; got: %s", script)
	}
	if !strings.Contains(script, "'"+trayPath+"'") {
		t.Errorf("tray path with spaces should be preserved in single quotes; got: %s", script)
	}
}

func TestBuildRestartScript_PathsWithSingleQuotes(t *testing.T) {
	daemonPath := filepath.Join("C:", "Users", "O'Brien", "savecraft-daemon.exe")
	trayPath := filepath.Join("C:", "Users", "O'Brien", "savecraft-tray.exe")
	script := buildRestartScript(42, daemonPath, trayPath)

	// Single quotes should be doubled for PowerShell escaping.
	escapedDaemon := strings.ReplaceAll(daemonPath, "'", "''")
	escapedTray := strings.ReplaceAll(trayPath, "'", "''")

	if !strings.Contains(script, "'"+escapedDaemon+"'") {
		t.Errorf("daemon path single quotes should be escaped; got: %s", script)
	}
	if !strings.Contains(script, "'"+escapedTray+"'") {
		t.Errorf("tray path single quotes should be escaped; got: %s", script)
	}
}

func TestPsEscape(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"simple", "simple"},
		{"it's", "it''s"},
		{"a'b'c", "a''b''c"},
		{"no quotes", "no quotes"},
	}
	for _, tt := range tests {
		got := psEscape(tt.input)
		if got != tt.want {
			t.Errorf("psEscape(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
