package main

import (
	"fmt"
	"testing"
	"time"
)

func TestSupervisorFirstFailureTriggersRestart(t *testing.T) {
	var called int
	s := newSupervisor(func() error {
		called++
		return nil
	})

	s.onDaemonUnreachable()

	if called != 1 {
		t.Fatalf("expected 1 start call, got %d", called)
	}
}

func TestSupervisorBackoffDoublesOnConsecutiveFailures(t *testing.T) {
	var calls []time.Time
	s := newSupervisor(func() error {
		calls = append(calls, time.Now())
		return nil
	})
	// Override clock for deterministic testing.
	now := time.Now()
	s.now = func() time.Time { return now }

	// First failure — immediate restart (backoff is 0 initially, first attempt always fires).
	s.onDaemonUnreachable()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call after first failure, got %d", len(calls))
	}

	// Second failure immediately — should NOT restart (backoff = 1s, no time elapsed).
	s.onDaemonUnreachable()
	if len(calls) != 1 {
		t.Fatalf("expected still 1 call (backoff not elapsed), got %d", len(calls))
	}

	// Advance past 1s backoff.
	now = now.Add(2 * time.Second)
	s.onDaemonUnreachable()
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls after backoff elapsed, got %d", len(calls))
	}

	// Backoff should now be 2s — advancing 1s should NOT trigger.
	now = now.Add(1 * time.Second)
	s.onDaemonUnreachable()
	if len(calls) != 2 {
		t.Fatalf("expected still 2 calls (2s backoff not elapsed), got %d", len(calls))
	}

	// Advance past 2s total.
	now = now.Add(2 * time.Second)
	s.onDaemonUnreachable()
	if len(calls) != 3 {
		t.Fatalf("expected 3 calls after 2s backoff elapsed, got %d", len(calls))
	}
}

func TestSupervisorBackoffCapsAt60s(t *testing.T) {
	s := newSupervisor(func() error { return nil })
	now := time.Now()
	s.now = func() time.Time { return now }

	// Burn through backoff levels: 1, 2, 4, 8, 16, 32, 60, 60...
	for range 20 {
		now = now.Add(61 * time.Second) // always past cap
		s.onDaemonUnreachable()
	}

	if s.currentBackoff > 60*time.Second {
		t.Fatalf("backoff exceeded cap: %v", s.currentBackoff)
	}
}

func TestSupervisorResetsOnSuccess(t *testing.T) {
	var called int
	s := newSupervisor(func() error {
		called++
		return nil
	})
	now := time.Now()
	s.now = func() time.Time { return now }

	// Trigger several failures to build up backoff.
	for range 5 {
		now = now.Add(61 * time.Second)
		s.onDaemonUnreachable()
	}
	called = 0

	// Success resets everything.
	s.onDaemonReachable()

	if s.consecutiveFailures != 0 {
		t.Fatalf("expected 0 failures after reset, got %d", s.consecutiveFailures)
	}
	if s.currentBackoff != 0 {
		t.Fatalf("expected 0 backoff after reset, got %v", s.currentBackoff)
	}

	// Next failure should restart immediately (backoff reset).
	s.onDaemonUnreachable()
	if called != 1 {
		t.Fatalf("expected immediate restart after reset, got %d calls", called)
	}
}

func TestSupervisorToastAfterThreeFailedSpawns(t *testing.T) {
	spawnErr := fmt.Errorf("binary not found")
	var toastCalls int
	s := newSupervisor(func() error {
		return spawnErr
	})
	s.toastFunc = func(_, _, _ string) { toastCalls++ }
	now := time.Now()
	s.now = func() time.Time { return now }

	// Three failed spawns.
	for range 3 {
		now = now.Add(61 * time.Second)
		s.onDaemonUnreachable()
	}

	if toastCalls != 1 {
		t.Fatalf("expected 1 toast after 3 failures, got %d", toastCalls)
	}
}

func TestSupervisorNoToastBeforeThreeFailedSpawns(t *testing.T) {
	spawnErr := fmt.Errorf("binary not found")
	var toastCalls int
	s := newSupervisor(func() error {
		return spawnErr
	})
	s.toastFunc = func(_, _, _ string) { toastCalls++ }
	now := time.Now()
	s.now = func() time.Time { return now }

	// Only two failed spawns.
	for range 2 {
		now = now.Add(61 * time.Second)
		s.onDaemonUnreachable()
	}

	if toastCalls != 0 {
		t.Fatalf("expected no toast before 3 failures, got %d", toastCalls)
	}
}

func TestSupervisorNoRestartWhenDaemonHealthy(t *testing.T) {
	var called int
	s := newSupervisor(func() error {
		called++
		return nil
	})

	// Only reachable calls — never attempt restart.
	for range 10 {
		s.onDaemonReachable()
	}

	if called != 0 {
		t.Fatalf("expected 0 start calls when healthy, got %d", called)
	}
}

func TestSupervisorRestartingTrueAfterSuccessfulSpawn(t *testing.T) {
	s := newSupervisor(func() error { return nil })

	if s.restarting() {
		t.Fatal("should not be restarting before any failure")
	}

	s.onDaemonUnreachable()

	if !s.restarting() {
		t.Fatal("should be restarting after successful spawn")
	}
}

func TestSupervisorRestartingFalseAfterFailedSpawn(t *testing.T) {
	s := newSupervisor(func() error { return fmt.Errorf("not found") })

	s.onDaemonUnreachable()

	if s.restarting() {
		t.Fatal("should not be restarting after failed spawn")
	}
}

func TestSupervisorRestartingResetsOnReachable(t *testing.T) {
	s := newSupervisor(func() error { return nil })

	s.onDaemonUnreachable()
	if !s.restarting() {
		t.Fatal("should be restarting after spawn")
	}

	s.onDaemonReachable()
	if s.restarting() {
		t.Fatal("should not be restarting after daemon responds")
	}
}

func TestSupervisorToastResetsAfterSuccess(t *testing.T) {
	spawnErr := fmt.Errorf("binary not found")
	var toastCalls int
	s := newSupervisor(func() error {
		return spawnErr
	})
	s.toastFunc = func(_, _, _ string) { toastCalls++ }
	now := time.Now()
	s.now = func() time.Time { return now }

	// Three failures → toast.
	for range 3 {
		now = now.Add(61 * time.Second)
		s.onDaemonUnreachable()
	}
	if toastCalls != 1 {
		t.Fatalf("expected 1 toast, got %d", toastCalls)
	}

	// Daemon comes back.
	s.onDaemonReachable()

	// Three more failures → second toast.
	for range 3 {
		now = now.Add(61 * time.Second)
		s.onDaemonUnreachable()
	}
	if toastCalls != 2 {
		t.Fatalf("expected 2 toasts after reset+failure cycle, got %d", toastCalls)
	}
}
