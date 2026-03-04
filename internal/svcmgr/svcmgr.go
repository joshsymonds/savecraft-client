// Package svcmgr provides cross-platform daemon service management.
// It handles service installation (systemd, launchd, registry Run key),
// lifecycle (start/stop), and interactive signal handling.
package svcmgr

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"sync"

	"golang.org/x/term"
)

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

// Program manages the daemon lifecycle via context cancellation.
type Program struct {
	cfg    Config
	run    RunFunc
	cancel context.CancelFunc
	once   sync.Once
	mu     sync.Mutex
	err    error
}

// New creates a Program that will execute run when started.
func New(cfg Config, run RunFunc) *Program {
	return &Program{
		cfg: cfg,
		run: run,
	}
}

// Start launches the run function in a background goroutine.
func (p *Program) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel

	go func() {
		if err := p.run(ctx); err != nil {
			p.mu.Lock()
			p.err = err
			p.mu.Unlock()
		}
	}()
}

// Stop cancels the context to signal the run function to shut down.
// Safe to call multiple times.
func (p *Program) Stop() {
	p.once.Do(func() {
		if p.cancel != nil {
			p.cancel()
		}
	})
}

// Err returns the error from the run function, if any.
func (p *Program) Err() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.err
}

// Run starts the program, waits for a shutdown signal, then stops it.
// Returns the error from the run function, if any.
func Run(prog *Program) error {
	prog.Start()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, shutdownSignals()...)
	<-sig
	signal.Stop(sig)

	prog.Stop()

	return prog.Err()
}

// Interactive reports whether stderr is connected to a terminal.
// Use this to decide whether to print human-readable messages.
func Interactive() bool {
	//nolint:gosec // G115: fd fits in int on all supported platforms.
	return term.IsTerminal(int(os.Stderr.Fd()))
}

// Control dispatches a service management action to the platform backend.
// Supported actions: "install", "uninstall", "start", "stop".
func Control(cfg Config, action string) error {
	return control(cfg, action, defaultRunner)
}

func control(cfg Config, action string, run commandRunner) error {
	switch action {
	case "install":
		exePath, err := os.Executable()
		if err != nil {
			return fmt.Errorf("get executable path: %w", err)
		}

		return install(cfg, exePath, run)
	case "uninstall":
		return uninstall(cfg, run)
	case "start":
		return serviceStart(cfg, run)
	case "stop":
		return serviceStop(cfg, run)
	default:
		return fmt.Errorf("unknown service action: %s", action)
	}
}
