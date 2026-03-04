//go:build !darwin && !windows

package svcmgr

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// UnitFileContent returns the systemd user unit file for the daemon.
// Includes security hardening directives matching install.sh.
func UnitFileContent(cfg Config, exePath string) string {
	configDir := filepath.Join("%h", ".config", cfg.AppName)
	cacheDir := filepath.Join("%h", ".cache", cfg.AppName)
	binDir := filepath.Dir(exePath)

	// Use %h (systemd specifier for $HOME) for portability in user units.
	var buf strings.Builder

	buf.WriteString("[Unit]\n")
	buf.WriteString("Description=" + cfg.Description + "\n")
	buf.WriteString("After=network-online.target\n")
	buf.WriteString("\n")

	buf.WriteString("[Service]\n")
	buf.WriteString("ExecStart=" + exePath + " run\n")
	buf.WriteString("Restart=always\n")
	buf.WriteString("RestartSec=5\n")
	buf.WriteString("EnvironmentFile=-" + configDir + "/env\n")
	buf.WriteString("\n")

	buf.WriteString("ProtectSystem=strict\n")
	buf.WriteString("ProtectHome=read-only\n")
	buf.WriteString("ReadWritePaths=" + configDir + " " + cacheDir + " " + binDir + "\n")
	buf.WriteString("\n")

	buf.WriteString("NoNewPrivileges=yes\n")
	buf.WriteString("PrivateTmp=yes\n")
	buf.WriteString("\n")

	buf.WriteString("RestrictAddressFamilies=AF_INET AF_INET6\n")
	buf.WriteString("\n")

	buf.WriteString("[Install]\n")
	buf.WriteString("WantedBy=default.target\n")

	return buf.String()
}

func unitFilePath(cfg Config) string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}

	return filepath.Join(home, ".config", "systemd", "user", cfg.Name+".service")
}

func install(cfg Config, exePath string, run commandRunner) error {
	unitPath := unitFilePath(cfg)

	if mkErr := os.MkdirAll(filepath.Dir(unitPath), 0o750); mkErr != nil {
		return fmt.Errorf("create systemd unit directory: %w", mkErr)
	}

	content := UnitFileContent(cfg, exePath)
	if writeErr := os.WriteFile(unitPath, []byte(content), 0o600); writeErr != nil {
		return fmt.Errorf("write systemd unit: %w", writeErr)
	}

	if err := systemctlRun(run, "daemon-reload"); err != nil {
		return err
	}

	return systemctlRun(run, "enable", cfg.Name+".service")
}

func uninstall(cfg Config, run commandRunner) error {
	//nolint:errcheck,gosec // best-effort: service may already be disabled/stopped
	systemctlRun(run, "disable", cfg.Name+".service")
	//nolint:errcheck,gosec // best-effort: service may already be stopped
	systemctlRun(run, "stop", cfg.Name+".service")

	unitPath := unitFilePath(cfg)
	if removeErr := os.Remove(unitPath); removeErr != nil && !os.IsNotExist(removeErr) {
		return fmt.Errorf("remove systemd unit: %w", removeErr)
	}

	return systemctlRun(run, "daemon-reload")
}

func serviceStart(cfg Config, run commandRunner) error {
	return systemctlRun(run, "start", cfg.Name+".service")
}

func serviceStop(cfg Config, run commandRunner) error {
	return systemctlRun(run, "stop", cfg.Name+".service")
}

func serviceRestart(cfg Config, run commandRunner) error {
	return systemctlRun(run, "restart", cfg.Name+".service")
}

func systemctlRun(run commandRunner, args ...string) error {
	fullArgs := append([]string{"--user"}, args...)

	out, err := run("systemctl", fullArgs...)
	if err != nil {
		return fmt.Errorf("systemctl %s: %s: %w", strings.Join(args, " "), strings.TrimSpace(string(out)), err)
	}

	return nil
}
