// Package watcher wraps fsnotify with debounce and SHA-256 hash deduplication.
package watcher

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/joshsymonds/savecraft.gg/internal/daemon"
)

const defaultDebounce = 500 * time.Millisecond

// Option configures the FSWatcher.
type Option func(*FSWatcher)

// WithDebounceDuration sets the debounce window for coalescing filesystem events.
func WithDebounceDuration(d time.Duration) Option {
	return func(w *FSWatcher) { w.debounce = d }
}

// FSWatcher watches directories for file changes, debouncing rapid events
// and deduplicating based on file content hash.
type FSWatcher struct {
	inner    *fsnotify.Watcher
	events   chan daemon.FileEvent
	debounce time.Duration

	mu      sync.Mutex
	hashes  map[string][sha256.Size]byte // path → last emitted content hash
	timers  map[string]*time.Timer       // path → active debounce timer
	pending map[string]daemon.FileOp     // path → first op in debounce window

	done      chan struct{}
	closeOnce sync.Once
}

// New creates an FSWatcher backed by fsnotify.
func New(opts ...Option) (*FSWatcher, error) {
	inner, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("create fsnotify watcher: %w", err)
	}

	fsw := &FSWatcher{
		inner:    inner,
		events:   make(chan daemon.FileEvent, 100),
		debounce: defaultDebounce,
		hashes:   make(map[string][sha256.Size]byte),
		timers:   make(map[string]*time.Timer),
		pending:  make(map[string]daemon.FileOp),
		done:     make(chan struct{}),
	}

	for _, opt := range opts {
		opt(fsw)
	}

	go fsw.loop()
	return fsw, nil
}

// Add registers a directory for watching.
func (w *FSWatcher) Add(path string) error {
	if err := w.inner.Add(path); err != nil {
		return fmt.Errorf("watch %s: %w", path, err)
	}
	return nil
}

// Remove stops watching a directory and clears associated state (hashes, timers).
func (w *FSWatcher) Remove(path string) error {
	if err := w.inner.Remove(path); err != nil {
		return fmt.Errorf("unwatch %s: %w", path, err)
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	for filePath, timer := range w.timers {
		if filepath.Dir(filePath) == path || filePath == path {
			timer.Stop()
			delete(w.timers, filePath)
			delete(w.pending, filePath)
		}
	}
	for filePath := range w.hashes {
		if filepath.Dir(filePath) == path || filePath == path {
			delete(w.hashes, filePath)
		}
	}

	return nil
}

// Events returns the channel of debounced, deduplicated file events.
func (w *FSWatcher) Events() <-chan daemon.FileEvent { return w.events }

// Close stops the watcher and releases resources.
func (w *FSWatcher) Close() error {
	var err error
	w.closeOnce.Do(func() {
		close(w.done)
		err = w.inner.Close()

		w.mu.Lock()
		for _, t := range w.timers {
			t.Stop()
		}
		w.mu.Unlock()
	})
	if err != nil {
		return fmt.Errorf("close fsnotify watcher: %w", err)
	}
	return nil
}

func (w *FSWatcher) loop() {
	for {
		select {
		case <-w.done:
			return
		case ev, ok := <-w.inner.Events:
			if !ok {
				return
			}
			w.handleFSEvent(ev)
		case _, ok := <-w.inner.Errors:
			if !ok {
				return
			}
		}
	}
}

func (w *FSWatcher) handleFSEvent(ev fsnotify.Event) {
	if ev.Has(fsnotify.Remove) || ev.Has(fsnotify.Rename) {
		w.handleRemove(ev.Name)
		return
	}

	if !ev.Has(fsnotify.Create) && !ev.Has(fsnotify.Write) {
		return
	}

	op := daemon.FileModify
	if ev.Has(fsnotify.Create) {
		op = daemon.FileCreate
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if _, exists := w.pending[ev.Name]; !exists {
		w.pending[ev.Name] = op
	}

	if t, ok := w.timers[ev.Name]; ok {
		t.Reset(w.debounce)
	} else {
		path := ev.Name
		w.timers[path] = time.AfterFunc(w.debounce, func() {
			w.fireDebounced(path)
		})
	}
}

func (w *FSWatcher) handleRemove(path string) {
	w.mu.Lock()
	if t, ok := w.timers[path]; ok {
		t.Stop()
		delete(w.timers, path)
		delete(w.pending, path)
	}
	delete(w.hashes, path)
	w.mu.Unlock()

	select {
	case w.events <- daemon.FileEvent{Path: path, Op: daemon.FileRemove}:
	case <-w.done:
	}
}

func (w *FSWatcher) fireDebounced(path string) {
	w.mu.Lock()
	op := w.pending[path]
	delete(w.timers, path)
	delete(w.pending, path)
	prevHash, seen := w.hashes[path]
	w.mu.Unlock()

	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return
	}

	newHash := sha256.Sum256(data)
	if seen && newHash == prevHash {
		return
	}

	w.mu.Lock()
	w.hashes[path] = newHash
	w.mu.Unlock()

	select {
	case w.events <- daemon.FileEvent{Path: path, Op: op}:
	case <-w.done:
	}
}
