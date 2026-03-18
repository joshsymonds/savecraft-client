package pluginmgr

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

const defaultWatcherDebounce = 500 * time.Millisecond

// WatcherOption configures a PluginWatcher.
type WatcherOption func(*PluginWatcher)

// WithWatcherDebounce sets the debounce duration for coalescing filesystem events.
func WithWatcherDebounce(d time.Duration) WatcherOption {
	return func(pw *PluginWatcher) { pw.debounce = d }
}

// WithWatcherLogger sets the logger for the PluginWatcher.
func WithWatcherLogger(l *slog.Logger) WatcherOption {
	return func(pw *PluginWatcher) { pw.logger = l }
}

// PluginWatcher watches a local plugin directory for parser.wasm changes
// and invokes a callback with the affected game ID.
type PluginWatcher struct {
	inner    *fsnotify.Watcher
	localDir string
	callback func(gameID string)
	debounce time.Duration
	logger   *slog.Logger

	mu        sync.Mutex
	timers    map[string]*time.Timer // gameID → active debounce timer
	done      chan struct{}
	closeOnce sync.Once
}

// NewPluginWatcher creates a watcher on localDir that calls callback(gameID)
// whenever a {localDir}/{gameID}/parser.wasm file changes.
func NewPluginWatcher(
	localDir string, callback func(gameID string), opts ...WatcherOption,
) (*PluginWatcher, error) {
	inner, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("create fsnotify watcher: %w", err)
	}

	pw := &PluginWatcher{
		inner:    inner,
		localDir: localDir,
		callback: callback,
		debounce: defaultWatcherDebounce,
		logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		timers:   make(map[string]*time.Timer),
		done:     make(chan struct{}),
	}
	for _, opt := range opts {
		opt(pw)
	}

	// Watch each existing game subdirectory.
	entries, readErr := os.ReadDir(localDir)
	if readErr != nil {
		return nil, errors.Join(
			fmt.Errorf("read plugin dir %s: %w", localDir, readErr),
			inner.Close(),
		)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		subdir := filepath.Join(localDir, entry.Name())
		if watchErr := inner.Add(subdir); watchErr != nil {
			return nil, errors.Join(
				fmt.Errorf("watch %s: %w", subdir, watchErr),
				inner.Close(),
			)
		}
	}

	go pw.loop()
	return pw, nil
}

// Close stops the watcher and releases resources.
func (pw *PluginWatcher) Close() error {
	var closeErr error
	pw.closeOnce.Do(func() {
		close(pw.done)
		if err := pw.inner.Close(); err != nil {
			closeErr = fmt.Errorf("close fsnotify watcher: %w", err)
		}
		pw.mu.Lock()
		for _, t := range pw.timers {
			t.Stop()
		}
		pw.mu.Unlock()
	})
	return closeErr
}

func (pw *PluginWatcher) loop() {
	for {
		select {
		case <-pw.done:
			return
		case ev, ok := <-pw.inner.Events:
			if !ok {
				return
			}
			pw.handleEvent(ev)
		case err, ok := <-pw.inner.Errors:
			if !ok {
				return
			}
			pw.logger.Warn("fsnotify error", slog.String("error", err.Error()))
		}
	}
}

func (pw *PluginWatcher) handleEvent(ev fsnotify.Event) {
	// Only care about Create and Write events.
	if !ev.Has(fsnotify.Create) && !ev.Has(fsnotify.Write) {
		return
	}

	// Only care about parser.wasm files.
	if filepath.Base(ev.Name) != "parser.wasm" {
		return
	}

	// Extract gameID from parent directory: {localDir}/{gameID}/parser.wasm
	gameID := filepath.Base(filepath.Dir(ev.Name))

	pw.mu.Lock()
	defer pw.mu.Unlock()

	if t, ok := pw.timers[gameID]; ok {
		t.Reset(pw.debounce)
	} else {
		gid := gameID
		pw.timers[gid] = time.AfterFunc(pw.debounce, func() {
			pw.mu.Lock()
			delete(pw.timers, gid)
			pw.mu.Unlock()

			select {
			case <-pw.done:
			default:
				pw.callback(gid)
			}
		})
	}
}
