// Package svcmgr provides cross-platform daemon service management.
// It handles service installation (systemd, launchd, registry Run key),
// lifecycle (start/stop), and interactive signal handling.
package svcmgr

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"os/signal"

	"golang.org/x/term"
)

const actionRestart = "restart"

// Config holds the service identity used for OS registration.
type Config struct {
	Name        string // OS service name, e.g. "savecraft-daemon".
	DisplayName string // Human-readable name, e.g. "Savecraft Daemon".
	Description string // Service description for OS registration.
	AppName     string // Base app name for paths, e.g. "savecraft".
}

// RunFunc is the daemon's main loop. It receives a context that is
// canceled when the service is asked to stop.
type RunFunc func(ctx context.Context) error

// commandRunner executes an external command and returns its combined output.
// The default uses exec.Command; tests inject a fake to capture calls.
type commandRunner func(name string, args ...string) ([]byte, error)

func defaultRunner(name string, args ...string) ([]byte, error) {
	//nolint:gosec // G204: args controlled by platform backends, not user input.
	cmd := exec.CommandContext(context.Background(), name, args...)

	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("%s: %w", name, err)
	}

	return out, nil
}

// Run executes the daemon's main loop in the calling goroutine, passing it a
// context that is canceled when a shutdown signal arrives. It returns whatever
// the RunFunc returns — immediately, the moment the RunFunc returns, whether
// that is because a signal canceled the context (graceful, typically nil) or
// because the RunFunc terminated on its own (e.g. a fatal error).
//
// Running the work in the caller's goroutine — rather than launching it and
// waiting separately on the signal channel — is deliberate: it makes process
// lifetime equal to the RunFunc's lifetime. A fatal error therefore always
// reaches the process exit code, so the OS service manager's restart policy
// (e.g. systemd Restart=on-failure) actually fires. A supervisor that waited
// only on a signal would leave the process alive-but-inert after the RunFunc
// died, silently defeating that restart contract.
func Run(run RunFunc) error {
	ctx, cancel := signal.NotifyContext(context.Background(), shutdownSignals()...)
	defer cancel()

	return run(ctx)
}

// Interactive reports whether stderr is connected to a terminal.
// Use this to decide whether to print human-readable messages.
func Interactive() bool {
	fd := os.Stderr.Fd()
	if fd > math.MaxInt {
		return false
	}
	return term.IsTerminal(int(fd))
}

// UninstallPaths holds all filesystem paths to remove during uninstall.
type UninstallPaths struct {
	ConfigDir string // e.g. ~/.config/savecraft
	CacheDir  string // e.g. ~/.cache/savecraft
	DataDir   string // e.g. ~/.local/share/savecraft (plugin cache parent)
	LogDir    string // e.g. ~/Library/Logs/savecraft (macOS only; empty on others)
	Binary    string // path to the running binary
}

// Control dispatches a service management action to the platform backend.
// Supported actions: "install", "start", "stop", "restart".
func Control(cfg Config, action string) error {
	return control(cfg, action, defaultRunner)
}

// Uninstall completely removes the daemon: stops/removes the OS service,
// deletes config/cache/data/log directories, and removes the binary itself.
// Each step is best-effort — failures are printed but do not abort.
func Uninstall(cfg Config, paths UninstallPaths, output io.Writer) error {
	return doUninstall(cfg, paths, output, defaultRunner)
}

func doUninstall(cfg Config, paths UninstallPaths, output io.Writer, run commandRunner) error {
	// Step 1: Remove OS service registration (best-effort).
	if err := uninstall(cfg, run); err != nil {
		fmt.Fprintf(output, "  warning: remove service: %v\n", err)
	} else {
		fmt.Fprintf(output, "  Removed OS service %s\n", cfg.Name)
	}

	// Step 2: Remove directories.
	for _, dir := range []struct{ label, path string }{
		{"config", paths.ConfigDir},
		{"cache", paths.CacheDir},
		{"data", paths.DataDir},
		{"log", paths.LogDir},
	} {
		if dir.path == "" {
			continue
		}

		if err := os.RemoveAll(dir.path); err != nil {
			fmt.Fprintf(output, "  warning: remove %s dir: %v\n", dir.label, err)
		} else {
			fmt.Fprintf(output, "  Removed %s\n", dir.path)
		}
	}

	// Step 3: Remove binary (last — we're running from it).
	if paths.Binary != "" {
		if err := os.Remove(paths.Binary); err != nil && !os.IsNotExist(err) {
			fmt.Fprintf(output, "  warning: remove binary: %v\n", err)
		} else if err == nil {
			fmt.Fprintf(output, "  Removed %s\n", paths.Binary)
		}
	}

	return nil
}

func control(cfg Config, action string, run commandRunner) error {
	switch action {
	case "install":
		exePath, err := os.Executable()
		if err != nil {
			return fmt.Errorf("get executable path: %w", err)
		}

		return install(cfg, exePath, run)
	case "start":
		return serviceStart(cfg, run)
	case "stop":
		return serviceStop(cfg, run)
	case actionRestart:
		return serviceRestart(cfg, run)
	default:
		return fmt.Errorf("unknown service action: %s", action)
	}
}
