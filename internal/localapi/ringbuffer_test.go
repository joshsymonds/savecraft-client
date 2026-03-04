package localapi

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"
)

func TestRingBuffer_CapturesEntries(t *testing.T) {
	inner := slog.NewJSONHandler(discardWriter{}, nil)
	rb := NewRingBuffer(10, inner)
	logger := slog.New(rb)

	logger.Info("hello")
	logger.Warn("world")

	entries := rb.Entries()
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	if entries[0].Message != "hello" {
		t.Errorf("entries[0].msg = %q, want %q", entries[0].Message, "hello")
	}
	if entries[0].Level != "INFO" {
		t.Errorf("entries[0].level = %q, want %q", entries[0].Level, "INFO")
	}
	if entries[1].Message != "world" {
		t.Errorf("entries[1].msg = %q, want %q", entries[1].Message, "world")
	}
	if entries[1].Level != "WARN" {
		t.Errorf("entries[1].level = %q, want %q", entries[1].Level, "WARN")
	}
}

func TestRingBuffer_CapturesAttrs(t *testing.T) {
	inner := slog.NewJSONHandler(discardWriter{}, nil)
	rb := NewRingBuffer(10, inner)
	logger := slog.New(rb)

	logger.Info("test", slog.String("key", "value"), slog.Int("count", 42))

	entries := rb.Entries()
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if entries[0].Attrs["key"] != "value" {
		t.Errorf("attrs[key] = %v, want %q", entries[0].Attrs["key"], "value")
	}
	if entries[0].Attrs["count"] != int64(42) {
		t.Errorf("attrs[count] = %v (%T), want 42", entries[0].Attrs["count"], entries[0].Attrs["count"])
	}
}

func TestRingBuffer_Overflow(t *testing.T) {
	inner := slog.NewJSONHandler(discardWriter{}, nil)
	rb := NewRingBuffer(3, inner)
	logger := slog.New(rb)

	logger.Info("a")
	logger.Info("b")
	logger.Info("c")
	logger.Info("d")
	logger.Info("e")

	entries := rb.Entries()
	if len(entries) != 3 {
		t.Fatalf("entries = %d, want 3", len(entries))
	}
	// Should have c, d, e (oldest dropped)
	if entries[0].Message != "c" {
		t.Errorf("entries[0] = %q, want %q", entries[0].Message, "c")
	}
	if entries[1].Message != "d" {
		t.Errorf("entries[1] = %q, want %q", entries[1].Message, "d")
	}
	if entries[2].Message != "e" {
		t.Errorf("entries[2] = %q, want %q", entries[2].Message, "e")
	}
}

func TestRingBuffer_EmptyEntries(t *testing.T) {
	inner := slog.NewJSONHandler(discardWriter{}, nil)
	rb := NewRingBuffer(10, inner)

	entries := rb.Entries()
	if len(entries) != 0 {
		t.Fatalf("entries = %d, want 0", len(entries))
	}
}

func TestRingBuffer_ExactCapacity(t *testing.T) {
	inner := slog.NewJSONHandler(discardWriter{}, nil)
	rb := NewRingBuffer(3, inner)
	logger := slog.New(rb)

	logger.Info("a")
	logger.Info("b")
	logger.Info("c")

	entries := rb.Entries()
	if len(entries) != 3 {
		t.Fatalf("entries = %d, want 3", len(entries))
	}
	if entries[0].Message != "a" {
		t.Errorf("entries[0] = %q, want %q", entries[0].Message, "a")
	}
	if entries[2].Message != "c" {
		t.Errorf("entries[2] = %q, want %q", entries[2].Message, "c")
	}
}

func TestRingBuffer_ConcurrentAccess(t *testing.T) {
	inner := slog.NewJSONHandler(discardWriter{}, nil)
	rb := NewRingBuffer(100, inner)
	logger := slog.New(rb)

	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				logger.Info("msg")
				rb.Entries()
			}
		}()
	}
	wg.Wait()

	entries := rb.Entries()
	if len(entries) != 100 {
		t.Errorf("entries = %d, want 100 (buffer full from 500 writes)", len(entries))
	}
}

func TestRingBuffer_Enabled(t *testing.T) {
	inner := slog.NewJSONHandler(discardWriter{}, &slog.HandlerOptions{
		Level: slog.LevelWarn,
	})
	rb := NewRingBuffer(10, inner)

	if rb.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("should not be enabled for INFO when inner is WARN")
	}
	if !rb.Enabled(context.Background(), slog.LevelWarn) {
		t.Error("should be enabled for WARN")
	}
}

func TestRingBuffer_HasTimestamp(t *testing.T) {
	inner := slog.NewJSONHandler(discardWriter{}, nil)
	rb := NewRingBuffer(10, inner)
	logger := slog.New(rb)

	logger.Info("test")

	entries := rb.Entries()
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}

	// Verify the timestamp parses as RFC3339.
	if _, err := time.Parse(time.RFC3339, entries[0].Time); err != nil {
		t.Errorf("time %q is not valid RFC3339: %v", entries[0].Time, err)
	}
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }
