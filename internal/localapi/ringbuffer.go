package localapi

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// DefaultBufferSize is the default number of log entries to retain.
const DefaultBufferSize = 1000

// LogEntry is a single captured log line.
type LogEntry struct {
	Time    string         `json:"time"`
	Level   string         `json:"level"`
	Message string         `json:"msg"`
	Attrs   map[string]any `json:"attrs,omitempty"`
}

// ringState holds the shared mutable state of a ring buffer.
type ringState struct {
	mu      sync.RWMutex
	entries []LogEntry
	size    int
	pos     int
	full    bool
}

// RingBuffer is a slog.Handler that captures log entries in a
// fixed-size circular buffer while delegating to an inner handler.
type RingBuffer struct {
	state *ringState
	inner slog.Handler
}

// NewRingBuffer creates a ring buffer wrapping inner, retaining up to size entries.
func NewRingBuffer(size int, inner slog.Handler) *RingBuffer {
	return &RingBuffer{
		state: &ringState{
			entries: make([]LogEntry, size),
			size:    size,
		},
		inner: inner,
	}
}

// Enabled reports whether the handler handles records at the given level.
func (rb *RingBuffer) Enabled(ctx context.Context, level slog.Level) bool {
	return rb.inner.Enabled(ctx, level)
}

// Handle captures the record in the ring buffer and delegates to the inner handler.
func (rb *RingBuffer) Handle(ctx context.Context, record slog.Record) error {
	entry := LogEntry{
		Time:    record.Time.Format(time.RFC3339),
		Level:   record.Level.String(),
		Message: record.Message,
	}

	if record.NumAttrs() > 0 {
		entry.Attrs = make(map[string]any, record.NumAttrs())
		record.Attrs(func(a slog.Attr) bool {
			entry.Attrs[a.Key] = a.Value.Any()
			return true
		})
	}

	st := rb.state
	st.mu.Lock()
	st.entries[st.pos] = entry
	st.pos = (st.pos + 1) % st.size
	if st.pos == 0 && !st.full {
		st.full = true
	}
	st.mu.Unlock()

	if err := rb.inner.Handle(ctx, record); err != nil {
		return fmt.Errorf("inner handler: %w", err)
	}

	return nil
}

// WithAttrs returns a new handler with the given attributes, sharing the same buffer.
func (rb *RingBuffer) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &RingBuffer{
		state: rb.state,
		inner: rb.inner.WithAttrs(attrs),
	}
}

// WithGroup returns a new handler with the given group name, sharing the same buffer.
func (rb *RingBuffer) WithGroup(name string) slog.Handler {
	return &RingBuffer{
		state: rb.state,
		inner: rb.inner.WithGroup(name),
	}
}

// Entries returns a copy of all captured log entries in chronological order.
func (rb *RingBuffer) Entries() []LogEntry {
	st := rb.state
	st.mu.RLock()
	defer st.mu.RUnlock()

	if !st.full {
		result := make([]LogEntry, st.pos)
		copy(result, st.entries[:st.pos])

		return result
	}

	result := make([]LogEntry, st.size)
	copy(result, st.entries[st.pos:])
	copy(result[st.size-st.pos:], st.entries[:st.pos])

	return result
}
