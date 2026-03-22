package main

import (
	"log/slog"
	"time"
)

const (
	supervisorBackoffBase = 1 * time.Second
	supervisorBackoffMax  = 60 * time.Second
	supervisorBackoffMult = 2
	supervisorToastAfter  = 3 // consecutive failed spawns before toast
)

// supervisor tracks daemon health and auto-restarts it when unreachable.
type supervisor struct {
	startDaemon func() error
	toastFunc   func(title, body, clickURL string)
	now         func() time.Time
	logger      *slog.Logger

	consecutiveFailures int
	consecutiveSpawnErr int
	currentBackoff      time.Duration
	lastAttempt         time.Time
	toasted             bool
	spawned             bool
}

func newSupervisor(startDaemon func() error) *supervisor {
	return &supervisor{
		startDaemon: startDaemon,
		toastFunc:   func(_, _, _ string) {}, // no-op default
		now:         time.Now,
	}
}

// onDaemonUnreachable is called when the /boot poll fails.
// It attempts to restart the daemon with exponential backoff.
func (s *supervisor) onDaemonUnreachable() {
	s.consecutiveFailures++
	now := s.now()

	// First failure always attempts immediately.
	if s.consecutiveFailures == 1 {
		s.tryStart(now)
		return
	}

	// Subsequent failures respect backoff.
	if now.Sub(s.lastAttempt) >= s.currentBackoff {
		s.tryStart(now)
	}
}

// onDaemonReachable is called when /boot succeeds. Resets all supervisor state.
func (s *supervisor) onDaemonReachable() {
	s.consecutiveFailures = 0
	s.consecutiveSpawnErr = 0
	s.currentBackoff = 0
	s.lastAttempt = time.Time{}
	s.toasted = false
	s.spawned = false
}

// restarting reports whether the supervisor has successfully spawned the daemon
// but hasn't yet received a successful /boot response. Used by the tray to
// show "Starting..." instead of "Offline".
func (s *supervisor) restarting() bool {
	return s.spawned
}

func (s *supervisor) tryStart(now time.Time) {
	s.lastAttempt = now

	if err := s.startDaemon(); err != nil {
		s.consecutiveSpawnErr++
		s.spawned = false

		if s.logger != nil {
			s.logger.Error("supervisor: start daemon failed", slog.String("error", err.Error()))
		}

		if s.consecutiveSpawnErr >= supervisorToastAfter && !s.toasted {
			s.toasted = true
			s.toastFunc("Savecraft", "Daemon failed to start after multiple attempts", "")
		}
	} else {
		s.consecutiveSpawnErr = 0
		s.toasted = false
		s.spawned = true
		if s.logger != nil {
			s.logger.Info("supervisor: daemon restart initiated")
		}
	}

	// Advance backoff for next attempt.
	if s.currentBackoff == 0 {
		s.currentBackoff = supervisorBackoffBase
	} else {
		s.currentBackoff *= supervisorBackoffMult
	}

	if s.currentBackoff > supervisorBackoffMax {
		s.currentBackoff = supervisorBackoffMax
	}
}
