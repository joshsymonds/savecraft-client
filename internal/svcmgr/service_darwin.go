//go:build darwin

package svcmgr

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PlistContent returns the launchd plist XML for the daemon agent.
func PlistContent(cfg Config, exePath string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}

	logDir := filepath.Join(home, "Library", "Logs", cfg.AppName)

	var buf strings.Builder

	buf.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	buf.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	buf.WriteString(`<plist version="1.0">` + "\n")
	buf.WriteString("<dict>\n")

	buf.WriteString("    <key>Label</key>\n")
	buf.WriteString("    <string>" + cfg.Name + "</string>\n")

	buf.WriteString("    <key>ProgramArguments</key>\n")
	buf.WriteString("    <array>\n")
	buf.WriteString("        <string>" + exePath + "</string>\n")
	buf.WriteString("        <string>run</string>\n")
	buf.WriteString("    </array>\n")

	buf.WriteString("    <key>KeepAlive</key>\n")
	buf.WriteString("    <true/>\n")

	buf.WriteString("    <key>RunAtLoad</key>\n")
	buf.WriteString("    <true/>\n")

	buf.WriteString("    <key>StandardOutPath</key>\n")
	buf.WriteString("    <string>" + filepath.Join(logDir, "stdout.log") + "</string>\n")

	buf.WriteString("    <key>StandardErrorPath</key>\n")
	buf.WriteString("    <string>" + filepath.Join(logDir, "stderr.log") + "</string>\n")

	buf.WriteString("</dict>\n")
	buf.WriteString("</plist>\n")

	return buf.String()
}

func plistPath(cfg Config) string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}

	return filepath.Join(home, "Library", "LaunchAgents", cfg.Name+".plist")
}

func install(cfg Config, exePath string, run commandRunner) error {
	plist := plistPath(cfg)

	if mkErr := os.MkdirAll(filepath.Dir(plist), 0o750); mkErr != nil {
		return fmt.Errorf("create LaunchAgents directory: %w", mkErr)
	}

	content := PlistContent(cfg, exePath)
	if writeErr := os.WriteFile(plist, []byte(content), 0o600); writeErr != nil {
		return fmt.Errorf("write launchd plist: %w", writeErr)
	}

	return launchctlRun(run, "load", "-w", plist)
}

func uninstall(cfg Config, run commandRunner) error {
	plist := plistPath(cfg)

	//nolint:errcheck,gosec // best-effort: plist may already be unloaded
	launchctlRun(run, "unload", plist)

	if removeErr := os.Remove(plist); removeErr != nil && !os.IsNotExist(removeErr) {
		return fmt.Errorf("remove launchd plist: %w", removeErr)
	}

	return nil
}

func serviceStart(cfg Config, run commandRunner) error {
	return launchctlRun(run, "start", cfg.Name)
}

func serviceStop(cfg Config, run commandRunner) error {
	return launchctlRun(run, "stop", cfg.Name)
}

func serviceRestart(cfg Config, run commandRunner) error {
	if err := serviceStop(cfg, run); err != nil {
		return err
	}

	return serviceStart(cfg, run)
}

func launchctlRun(run commandRunner, args ...string) error {
	out, err := run("launchctl", args...)
	if err != nil {
		return fmt.Errorf("launchctl %s: %s: %w", strings.Join(args, " "), strings.TrimSpace(string(out)), err)
	}

	return nil
}
