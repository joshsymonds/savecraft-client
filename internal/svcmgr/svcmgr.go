// Package svcmgr wraps kardianos/service to manage the daemon as an OS service.
package svcmgr

import (
	"context"
	"sync"

	"github.com/kardianos/service"
)

// Config holds the service identity used for OS registration.
type Config struct {
	Name        string
	DisplayName string
	Description string
}

// RunFunc is the daemon's main loop. It receives a context that is
// canceled when the service is asked to stop.
type RunFunc func(ctx context.Context) error

// Program implements service.Interface and manages the daemon lifecycle.
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
// Called by the service manager or directly in interactive mode.
//
//nolint:unparam // implements service.Interface — error return required by contract
func (p *Program) Start(_ service.Service) error {
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel

	go func() {
		if err := p.run(ctx); err != nil {
			p.mu.Lock()
			p.err = err
			p.mu.Unlock()
		}
	}()

	return nil
}

// Stop cancels the context to signal the run function to shut down.
// Safe to call multiple times.
//
//nolint:unparam // implements service.Interface — error return required by contract
func (p *Program) Stop(_ service.Service) error {
	p.once.Do(func() {
		if p.cancel != nil {
			p.cancel()
		}
	})

	return nil
}

// Err returns the error from the run function, if any.
func (p *Program) Err() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.err
}

// ServiceConfig returns the kardianos/service Config for OS registration.
// Arguments is set to ["run"] so the service manager invokes the run subcommand.
func (p *Program) ServiceConfig() *service.Config {
	return &service.Config{
		Name:        p.cfg.Name,
		DisplayName: p.cfg.DisplayName,
		Description: p.cfg.Description,
		Arguments:   []string{"run"},
	}
}
