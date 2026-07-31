// Package watcher wraps fsnotify with debounce and SHA-256 hash deduplication.
package watcher

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/joshsymonds/savecraft-client/internal/daemon"
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

	// addWatch registers a single directory with the underlying watch
	// mechanism. Indirected (defaults to inner.Add) purely for testability:
	// AddDirectoryUnit's partial-failure unwind can only be exercised
	// deterministically by injecting a failure for one directory in a
	// multi-directory walk, which forcing a real inotify watch to fail
	// cannot do reliably.
	addWatch func(dir string) error

	mu      sync.Mutex
	hashes  map[string][sha256.Size]byte // path → last emitted content hash
	timers  map[string]*time.Timer       // path → active debounce timer
	pending map[string]daemon.FileOp     // path → first op in debounce window

	// dirUnits maps a directory-unit root to every directory registered
	// with fsnotify beneath it (including the root itself), populated by
	// AddDirectoryUnit and consumed by its Remove counterpart.
	dirUnits map[string][]string

	// dirUnitExcludes remembers each directory-unit root's excludeDirs, so a
	// subdirectory created dynamically after AddDirectoryUnit (e.g. Players/
	// appearing when a fresh world's first player joins) can be filtered the
	// same way collectDirectoryUnitDirs filtered it at add time.
	dirUnitExcludes map[string][]string

	// dirUnitRootOf maps every watched directory beneath a directory-unit
	// root (including the root) back to that root, so a member file's
	// parent directory can be recognized in O(1) during event handling.
	dirUnitRootOf map[string]string

	// dirUnitTimers holds the pending per-root quiescence timer for a
	// directory unit. Layered over the per-file debounce above, it
	// coalesces however many member files changed within the window into
	// one FileEvent for the root, emitted once no further member change
	// arrives within the window.
	dirUnitTimers map[string]*time.Timer

	// dirUnitGenerations counts, per directory-unit root, how many times
	// its quiescence timer has been superseded by a reschedule that arrived
	// after the timer had already fired (Stop returning false — see
	// scheduleDirectoryUnitEvent). fireDirectoryUnitEvent captures the
	// generation in effect when its timer was scheduled and re-checks it
	// after acquiring the lock: a mismatch means a concurrent reschedule
	// raced ahead of this already-fired invocation, so it must not emit a
	// second, stale event for what is now a superseded debounce window.
	dirUnitGenerations map[string]uint64

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
		inner:              inner,
		events:             make(chan daemon.FileEvent, 100),
		debounce:           defaultDebounce,
		hashes:             make(map[string][sha256.Size]byte),
		timers:             make(map[string]*time.Timer),
		pending:            make(map[string]daemon.FileOp),
		dirUnits:           make(map[string][]string),
		dirUnitExcludes:    make(map[string][]string),
		dirUnitRootOf:      make(map[string]string),
		dirUnitTimers:      make(map[string]*time.Timer),
		dirUnitGenerations: make(map[string]uint64),
		done:               make(chan struct{}),
	}

	fsw.addWatch = inner.Add

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

// AddDirectoryUnit recursively watches root and every non-excluded
// subdirectory beneath it (excludeDirs matched case-insensitively by name).
// Unlike Add, a change to any member file beneath root does not produce a
// per-file event: it is coalesced by an additional per-directory quiescence
// window — the same debounce duration as the per-file debounce it layers
// over — into a single FileEvent addressed to root, emitted once no further
// member change arrives within the window. Plain file-unit watching via Add
// is unaffected.
func (w *FSWatcher) AddDirectoryUnit(root string, excludeDirs []string) error {
	dirs, err := collectDirectoryUnitDirs(root, excludeDirs)
	if err != nil {
		return fmt.Errorf("walk directory unit %s: %w", root, err)
	}

	added := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		if addErr := w.addWatch(dir); addErr != nil {
			// Unwind: the directories added before this one are already
			// registered with fsnotify but would otherwise appear in no
			// bookkeeping map (dirUnits/dirUnitRootOf are only populated
			// below, on full success), leaving them watched forever with no
			// way to Remove them.
			for _, prior := range added {
				if removeErr := w.inner.Remove(prior); removeErr != nil {
					continue // best-effort unwind of an already-failing call; nothing actionable
				}
			}
			return fmt.Errorf("watch %s: %w", dir, addErr)
		}
		added = append(added, dir)
	}

	w.mu.Lock()
	// A re-add for the same root (e.g. a rescan) must not leave stale
	// dirUnitRootOf entries for directories that were part of a previous
	// membership but are no longer under this root.
	if prevDirs, ok := w.dirUnits[root]; ok {
		for _, dir := range prevDirs {
			delete(w.dirUnitRootOf, dir)
		}
	}
	w.dirUnits[root] = dirs
	w.dirUnitExcludes[root] = excludeDirs
	for _, dir := range dirs {
		w.dirUnitRootOf[dir] = root
	}
	w.mu.Unlock()
	return nil
}

// collectDirectoryUnitDirs walks root and returns it plus every
// non-excluded subdirectory beneath it.
func collectDirectoryUnitDirs(root string, excludeDirs []string) ([]string, error) {
	var dirs []string
	var walk func(path string) error
	walk = func(path string) error {
		dirs = append(dirs, path)
		entries, err := os.ReadDir(path)
		if err != nil {
			return fmt.Errorf("read dir %s: %w", path, err)
		}
		for _, entry := range entries {
			if !entry.IsDir() || isDirUnitExcluded(entry.Name(), excludeDirs) {
				continue
			}
			if walkErr := walk(filepath.Join(path, entry.Name())); walkErr != nil {
				return walkErr
			}
		}
		return nil
	}
	if err := walk(root); err != nil {
		return nil, err
	}
	return dirs, nil
}

// isDirUnitExcluded reports whether name matches any entry in excludeDirs
// (case-insensitive), mirroring the daemon's exclude-dir semantics.
func isDirUnitExcluded(name string, excludeDirs []string) bool {
	for _, excluded := range excludeDirs {
		if strings.EqualFold(name, excluded) {
			return true
		}
	}
	return false
}

// Remove stops watching a directory and clears associated state (hashes,
// timers). For a path registered via AddDirectoryUnit, every recursively
// added subdirectory is unwatched too.
func (w *FSWatcher) Remove(path string) error {
	w.mu.Lock()
	dirs, isDirUnit := w.dirUnits[path]
	w.mu.Unlock()
	if isDirUnit {
		return w.removeDirectoryUnit(path, dirs)
	}

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

// removeDirectoryUnit unwatches every subdirectory previously registered by
// AddDirectoryUnit for root and clears its per-directory quiescence timer
// and the per-file dedup state of every member beneath it.
func (w *FSWatcher) removeDirectoryUnit(root string, dirs []string) error {
	var firstErr error
	for _, dir := range dirs {
		if err := w.inner.Remove(dir); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("unwatch %s: %w", dir, err)
		}
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	delete(w.dirUnits, root)
	delete(w.dirUnitExcludes, root)
	for _, dir := range dirs {
		delete(w.dirUnitRootOf, dir)
	}
	if t, ok := w.dirUnitTimers[root]; ok {
		t.Stop()
		delete(w.dirUnitTimers, root)
	}
	delete(w.dirUnitGenerations, root)
	for filePath, timer := range w.timers {
		if pathUnderAnyDir(filePath, dirs) {
			timer.Stop()
			delete(w.timers, filePath)
			delete(w.pending, filePath)
		}
	}
	for filePath := range w.hashes {
		if pathUnderAnyDir(filePath, dirs) {
			delete(w.hashes, filePath)
		}
	}

	if firstErr != nil {
		return firstErr
	}
	return nil
}

func pathUnderAnyDir(filePath string, dirs []string) bool {
	for _, dir := range dirs {
		if filepath.Dir(filePath) == dir || filePath == dir {
			return true
		}
	}
	return false
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
		for _, t := range w.dirUnitTimers {
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

	if ev.Has(fsnotify.Create) && w.handleDirUnitSubdirCreate(ev.Name) {
		return
	}

	op := daemon.FileModify
	if ev.Has(fsnotify.Create) {
		op = daemon.FileCreate
	}
	w.scheduleFileDebounce(ev.Name, op)
}

// scheduleFileDebounce (re)starts the per-file debounce timer for path, the
// same coalescing a real fsnotify Create/Write event drives in handleFSEvent.
// Also used to replay a file discovered already present under a
// dynamically-created directory-unit subdirectory (see
// handleDirUnitSubdirCreate) as if its own Create event had just fired: if a
// genuine fsnotify event for the same path arrives shortly after — the watch
// attached just in time after all — it resets this same timer instead of
// scheduling a second, independent one.
func (w *FSWatcher) scheduleFileDebounce(path string, op daemon.FileOp) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if _, exists := w.pending[path]; !exists {
		w.pending[path] = op
	}

	if t, ok := w.timers[path]; ok {
		t.Reset(w.debounce)
	} else {
		w.timers[path] = time.AfterFunc(w.debounce, func() {
			w.fireDebounced(path)
		})
	}
}

// handleDirUnitSubdirCreate checks whether a Create event names a new
// subdirectory appearing under an already-registered directory-unit root
// (e.g. Players/ appearing when a fresh world's first player joins, well
// after AddDirectoryUnit's one-shot walk). If so, it attaches watches to the
// new directory and any children already present beneath it, extends the
// unit's dirUnits/dirUnitRootOf bookkeeping to match, and replays any file
// already sitting under the new directory through the normal per-file
// debounce path (see scheduleFileDebounce) so it still reaches the root's
// quiescence event despite having landed before its own watch could attach.
// It reports false for anything else (a plain file create, or a directory
// outside any directory unit), leaving the caller to fall back to normal
// per-file handling.
func (w *FSWatcher) handleDirUnitSubdirCreate(path string) bool {
	info, statErr := os.Lstat(path)
	if statErr != nil || !info.IsDir() {
		return false
	}

	w.mu.Lock()
	root, isMember := w.dirUnitRootOf[filepath.Dir(path)]
	if !isMember {
		w.mu.Unlock()
		return false
	}
	excludeDirs := w.dirUnitExcludes[root]
	w.mu.Unlock()

	if isDirUnitExcluded(filepath.Base(path), excludeDirs) {
		return true
	}

	dirs, err := collectDirectoryUnitDirs(path, excludeDirs)
	if err != nil {
		return true
	}

	added := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		if addErr := w.addWatch(dir); addErr != nil {
			for _, prior := range added {
				if removeErr := w.inner.Remove(prior); removeErr != nil {
					continue // best-effort unwind of an already-failing call; nothing actionable
				}
			}
			return true
		}
		added = append(added, dir)
	}

	w.mu.Lock()
	w.dirUnits[root] = append(w.dirUnits[root], dirs...)
	for _, dir := range dirs {
		w.dirUnitRootOf[dir] = root
	}
	w.mu.Unlock()

	// A file may have landed inside the new directory before its watch
	// attached above; fsnotify will never report a Create for it, since the
	// watch did not exist yet. Route it through the same per-file debounce a
	// live Create event would use — this also means a genuine fsnotify event
	// that does still arrive for the same path (the watch attached just in
	// time after all) resets this same timer rather than scheduling the
	// root's quiescence a second, independent time.
	for _, existing := range collectExistingFiles(dirs) {
		w.scheduleFileDebounce(existing, daemon.FileCreate)
	}

	return true
}

// collectExistingFiles returns every regular (non-directory) entry directly
// inside each of dirs, without descending further — dirs already enumerates
// every directory in the subtree, so iterating their immediate children
// covers the whole tree.
func collectExistingFiles(dirs []string) []string {
	var files []string
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			files = append(files, filepath.Join(dir, entry.Name()))
		}
	}
	return files
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

	w.emitChange(path, daemon.FileRemove, nil)
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

	w.emitChange(path, op, data)
}

// emitChange delivers a per-file change for path, unless path is a member of
// a directory unit — in that case the change is deferred through the
// directory's per-root quiescence window instead (see AddDirectoryUnit) and
// no per-file event is ever sent for it.
func (w *FSWatcher) emitChange(path string, op daemon.FileOp, data []byte) {
	w.mu.Lock()
	root, isMember := w.dirUnitRootOf[filepath.Dir(path)]
	w.mu.Unlock()

	if !isMember {
		select {
		case w.events <- daemon.FileEvent{Path: path, Op: op, Data: data}:
		case <-w.done:
		}
		return
	}

	w.scheduleDirectoryUnitEvent(root)
}

// scheduleDirectoryUnitEvent (re)starts the per-root quiescence timer for a
// directory unit. When it eventually fires with no further reset, a single
// FileEvent addressed to root is emitted.
//
// Timer.Stop's return value distinguishes the two cases that matter here:
// if it reports the timer was stopped before firing, resetting the same
// timer is enough (the installed closure's captured generation is still
// current). If the timer had already fired — Stop returns false, and that
// invocation may already be blocked acquiring w.mu inside
// fireDirectoryUnitEvent — resetting the same timer cannot un-fire it, and
// simply reusing it would let that in-flight call also emit once its
// Reset-extended deadline elapses, producing two events for what should be
// one coalesced write. Bumping the generation and installing a fresh timer
// instead means the in-flight call's captured generation will mismatch
// against fireDirectoryUnitEvent's check once it gets the lock, so it exits
// without emitting.
func (w *FSWatcher) scheduleDirectoryUnitEvent(root string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if t, ok := w.dirUnitTimers[root]; ok {
		if t.Stop() {
			t.Reset(w.debounce)
			return
		}
	}
	w.dirUnitGenerations[root]++
	gen := w.dirUnitGenerations[root]
	w.dirUnitTimers[root] = time.AfterFunc(w.debounce, func() {
		w.fireDirectoryUnitEvent(root, gen)
	})
}

// fireDirectoryUnitEvent emits root's coalesced event, unless gen (the
// generation captured when this timer was scheduled) has been superseded by
// a later reschedule — see scheduleDirectoryUnitEvent.
func (w *FSWatcher) fireDirectoryUnitEvent(root string, gen uint64) {
	w.mu.Lock()
	if w.dirUnitGenerations[root] != gen {
		w.mu.Unlock()
		return
	}
	delete(w.dirUnitTimers, root)
	w.mu.Unlock()

	select {
	case w.events <- daemon.FileEvent{Path: root, Op: daemon.FileModify}:
	case <-w.done:
	}
}
