package watcher

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/joshsymonds/savecraft-client/internal/daemon"
)

const testDebounce = 50 * time.Millisecond

// waitForEvent blocks until an event arrives or times out.
func waitForEvent(t *testing.T, ch <-chan daemon.FileEvent) daemon.FileEvent {
	t.Helper()
	select {
	case ev := <-ch:
		return ev
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event")
		return daemon.FileEvent{}
	}
}

// expectNoEvent asserts no event arrives within a reasonable window.
func expectNoEvent(t *testing.T, ch <-chan daemon.FileEvent) {
	t.Helper()
	select {
	case ev := <-ch:
		t.Fatalf("unexpected event: path=%s op=%d", ev.Path, ev.Op)
	case <-time.After(testDebounce * 4):
	}
}

func newTestWatcher(t *testing.T) (*FSWatcher, string) {
	t.Helper()
	dir := t.TempDir()

	w, err := New(WithDebounceDuration(testDebounce))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if addErr := w.Add(dir); addErr != nil {
		t.Fatalf("Add: %v", addErr)
	}

	t.Cleanup(func() { w.Close() })
	return w, dir
}

func TestFileCreate_EmitsEvent(t *testing.T) {
	w, dir := newTestWatcher(t)
	path := filepath.Join(dir, "save.d2s")

	os.WriteFile(path, []byte("save data"), 0o644)

	ev := waitForEvent(t, w.Events())
	if ev.Path != path {
		t.Errorf("path = %q, want %q", ev.Path, path)
	}
	if ev.Op != daemon.FileCreate {
		t.Errorf("op = %d, want FileCreate (%d)", ev.Op, daemon.FileCreate)
	}
}

func TestFileCreate_EventContainsData(t *testing.T) {
	w, dir := newTestWatcher(t)
	path := filepath.Join(dir, "save.d2s")

	content := []byte("save file contents for data test")
	os.WriteFile(path, content, 0o644)

	ev := waitForEvent(t, w.Events())
	if ev.Data == nil {
		t.Fatal("ev.Data is nil, want file contents")
	}
	if !bytes.Equal(ev.Data, content) {
		t.Errorf("ev.Data = %q, want %q", ev.Data, content)
	}
}

func TestFileModify_EmitsEvent(t *testing.T) {
	w, dir := newTestWatcher(t)
	path := filepath.Join(dir, "save.d2s")

	os.WriteFile(path, []byte("v1"), 0o644)
	waitForEvent(t, w.Events()) // consume create event

	os.WriteFile(path, []byte("v2"), 0o644)
	ev := waitForEvent(t, w.Events())
	if ev.Path != path {
		t.Errorf("path = %q, want %q", ev.Path, path)
	}
}

func TestDebounce_CoalescesRapidWrites(t *testing.T) {
	w, dir := newTestWatcher(t)
	path := filepath.Join(dir, "save.d2s")

	// Write 10 times in rapid succession — should produce only 1 event.
	for i := range 10 {
		os.WriteFile(path, []byte{byte(i)}, 0o644)
		time.Sleep(5 * time.Millisecond)
	}

	ev := waitForEvent(t, w.Events())
	if ev.Path != path {
		t.Errorf("path = %q, want %q", ev.Path, path)
	}

	// No second event should arrive.
	expectNoEvent(t, w.Events())
}

func TestHashDedup_SkipsUnchangedContent(t *testing.T) {
	w, dir := newTestWatcher(t)
	path := filepath.Join(dir, "save.d2s")

	os.WriteFile(path, []byte("same content"), 0o644)
	waitForEvent(t, w.Events()) // first event

	// Write identical content — should NOT emit.
	os.WriteFile(path, []byte("same content"), 0o644)
	expectNoEvent(t, w.Events())
}

func TestHashDedup_EmitsOnContentChange(t *testing.T) {
	w, dir := newTestWatcher(t)
	path := filepath.Join(dir, "save.d2s")

	os.WriteFile(path, []byte("version 1"), 0o644)
	waitForEvent(t, w.Events())

	os.WriteFile(path, []byte("version 2"), 0o644)
	ev := waitForEvent(t, w.Events())
	if ev.Path != path {
		t.Errorf("path = %q, want %q", ev.Path, path)
	}
}

func TestFileRemove_EmitsEvent(t *testing.T) {
	w, dir := newTestWatcher(t)
	path := filepath.Join(dir, "save.d2s")

	os.WriteFile(path, []byte("data"), 0o644)
	waitForEvent(t, w.Events()) // consume create event

	os.Remove(path)
	ev := waitForEvent(t, w.Events())
	if ev.Path != path {
		t.Errorf("path = %q, want %q", ev.Path, path)
	}
	if ev.Op != daemon.FileRemove {
		t.Errorf("op = %d, want FileRemove (%d)", ev.Op, daemon.FileRemove)
	}
}

func TestFileRemove_ClearsHash(t *testing.T) {
	w, dir := newTestWatcher(t)
	path := filepath.Join(dir, "save.d2s")

	os.WriteFile(path, []byte("data"), 0o644)
	waitForEvent(t, w.Events()) // create

	os.Remove(path)
	waitForEvent(t, w.Events()) // remove

	// Recreate with same content — should emit because hash was cleared.
	os.WriteFile(path, []byte("data"), 0o644)
	ev := waitForEvent(t, w.Events())
	if ev.Path != path {
		t.Errorf("path = %q, want %q", ev.Path, path)
	}
}

func TestMultipleFiles_IndependentEvents(t *testing.T) {
	w, dir := newTestWatcher(t)
	pathA := filepath.Join(dir, "charA.d2s")
	pathB := filepath.Join(dir, "charB.d2s")

	os.WriteFile(pathA, []byte("char A"), 0o644)
	os.WriteFile(pathB, []byte("char B"), 0o644)

	got := make(map[string]bool)
	ev1 := waitForEvent(t, w.Events())
	got[ev1.Path] = true
	ev2 := waitForEvent(t, w.Events())
	got[ev2.Path] = true

	if !got[pathA] {
		t.Errorf("missing event for %s", pathA)
	}
	if !got[pathB] {
		t.Errorf("missing event for %s", pathB)
	}
}

func TestClose_StopsEventEmission(t *testing.T) {
	w, dir := newTestWatcher(t)
	w.Close()

	path := filepath.Join(dir, "save.d2s")
	os.WriteFile(path, []byte("data"), 0o644)

	expectNoEvent(t, w.Events())
}

func TestRemove_StopsEvents(t *testing.T) {
	w, dir := newTestWatcher(t)

	if err := w.Remove(dir); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	path := filepath.Join(dir, "save.d2s")
	os.WriteFile(path, []byte("data"), 0o644)

	expectNoEvent(t, w.Events())
}

func TestRemove_ClearsState(t *testing.T) {
	w, dir := newTestWatcher(t)

	// Write a file so there's hash state.
	path := filepath.Join(dir, "save.d2s")
	os.WriteFile(path, []byte("data"), 0o644)
	waitForEvent(t, w.Events())

	if err := w.Remove(dir); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	// Re-add the directory. The same file should emit again since hash state was cleared.
	if addErr := w.Add(dir); addErr != nil {
		t.Fatalf("Add after Remove: %v", addErr)
	}

	os.WriteFile(path, []byte("data"), 0o644)
	ev := waitForEvent(t, w.Events())
	if ev.Path != path {
		t.Errorf("path = %s, want %s", ev.Path, path)
	}
}

func TestRemove_NonexistentPath(t *testing.T) {
	w, _ := newTestWatcher(t)

	err := w.Remove("/nonexistent/path")
	if err == nil {
		t.Fatal("expected error for unwatched path")
	}
}

func TestClose_Idempotent(t *testing.T) {
	w, _ := newTestWatcher(t)

	if err := w.Close(); err != nil {
		t.Errorf("first Close: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

// --- Directory-unit tests ---

func newDirectoryUnitWatcher(t *testing.T, excludeDirs []string) (*FSWatcher, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Players"), 0o755); err != nil {
		t.Fatalf("mkdir Players: %v", err)
	}
	for _, ex := range excludeDirs {
		if err := os.MkdirAll(filepath.Join(root, ex), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", ex, err)
		}
	}

	w, err := New(WithDebounceDuration(testDebounce))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := w.AddDirectoryUnit(root, excludeDirs); err != nil {
		t.Fatalf("AddDirectoryUnit: %v", err)
	}
	t.Cleanup(func() { w.Close() })
	return w, root
}

// TestAddDirectoryUnit_NestedWriteRoutesToRoot confirms recursive watching:
// a write to a file nested under a nested subdirectory (Players/) produces
// one coalesced FileEvent addressed to the directory-unit root, not the
// nested subdirectory — the routing the daemon relies on to treat the whole
// directory as a single save unit.
func TestAddDirectoryUnit_NestedWriteRoutesToRoot(t *testing.T) {
	w, root := newDirectoryUnitWatcher(t, nil)

	path := filepath.Join(root, "Players", "player1.sav")
	os.WriteFile(path, []byte("player data"), 0o644)

	ev := waitForEvent(t, w.Events())
	if ev.Path != root {
		t.Errorf("event path = %q, want directory-unit root %q", ev.Path, root)
	}
	if ev.Op != daemon.FileModify {
		t.Errorf("op = %d, want FileModify (%d)", ev.Op, daemon.FileModify)
	}
	if ev.Data != nil {
		t.Errorf("data = %v, want nil (daemon rebuilds its own snapshot)", ev.Data)
	}
}

// TestAddDirectoryUnit_Quiescence_CoalescesMultipleMemberWrites writes to
// three distinct member files within the debounce window and expects exactly
// one coalesced root event after everything settles — the per-directory
// quiescence layered over the existing per-file debounce.
func TestAddDirectoryUnit_Quiescence_CoalescesMultipleMemberWrites(t *testing.T) {
	w, root := newDirectoryUnitWatcher(t, nil)

	os.WriteFile(filepath.Join(root, "Level.sav"), []byte("level"), 0o644)
	time.Sleep(testDebounce / 3)
	os.WriteFile(filepath.Join(root, "LevelMeta.sav"), []byte("meta"), 0o644)
	time.Sleep(testDebounce / 3)
	os.WriteFile(filepath.Join(root, "Players", "player1.sav"), []byte("player"), 0o644)

	ev := waitForEvent(t, w.Events())
	if ev.Path != root {
		t.Errorf("event path = %q, want %q", ev.Path, root)
	}

	expectNoEvent(t, w.Events())
}

// TestAddDirectoryUnit_ExcludedSubdirectory_NoEvents confirms an excluded
// subdirectory (e.g. a backup folder) is never watched: writes inside it
// produce no root event and no per-file event.
func TestAddDirectoryUnit_ExcludedSubdirectory_NoEvents(t *testing.T) {
	w, root := newDirectoryUnitWatcher(t, []string{"backup"})

	os.WriteFile(filepath.Join(root, "backup", "Level.sav.bak"), []byte("old"), 0o644)

	expectNoEvent(t, w.Events())
}

// TestAddDirectoryUnit_MemberWrite_SkipsPerFileHashRead confirms the P4 fix:
// a directory-unit member write goes straight to the root's quiescence
// scheduling without a per-file read+hash. Per-file SHA-256 dedup no longer
// applies to directory-unit members (unlike a plain file-unit path — see
// TestHashDedup_SkipsUnchangedContent, unaffected by this change): rewriting
// a member with byte-identical content still produces a new root event,
// because the daemon now deduplicates directory-unit content at the
// aggregate level instead (dispatchDirectoryUnit's dirUnitSnapshotHashes).
// It also proves w.hashes is never populated for a directory-unit member
// path, confirming the read+hash step was actually skipped, not just that
// dedup happened to not trigger.
func TestAddDirectoryUnit_MemberWrite_SkipsPerFileHashRead(t *testing.T) {
	w, root := newDirectoryUnitWatcher(t, nil)
	path := filepath.Join(root, "Level.sav")

	os.WriteFile(path, []byte("same content"), 0o644)
	waitForEvent(t, w.Events())

	w.mu.Lock()
	_, hashed := w.hashes[path]
	w.mu.Unlock()
	if hashed {
		t.Errorf(
			"w.hashes contains directory-unit member %q; per-file read+hash must be skipped for dir-unit members",
			path,
		)
	}

	os.WriteFile(path, []byte("same content"), 0o644)
	ev := waitForEvent(t, w.Events())
	if ev.Path != root {
		t.Errorf("event path = %q, want %q", ev.Path, root)
	}
}

// TestAddDirectoryUnit_Remove_StopsEvents confirms Remove on a
// directory-unit root unwatches every recursively-added subdirectory, not
// just the root itself.
func TestAddDirectoryUnit_Remove_StopsEvents(t *testing.T) {
	w, root := newDirectoryUnitWatcher(t, nil)

	if err := w.Remove(root); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	os.WriteFile(filepath.Join(root, "Players", "player1.sav"), []byte("data"), 0o644)
	os.WriteFile(filepath.Join(root, "Level.sav"), []byte("data"), 0o644)

	expectNoEvent(t, w.Events())
}

// TestAddDirectoryUnit_PartialAddFailure_UnwindsPriorWatches proves a
// mid-walk failure unwinds the directories added before it: without the
// unwind, root (added successfully before the injected failure on its
// child) would stay registered with fsnotify forever with no bookkeeping
// entry to ever Remove it through, so a write there would leak a spurious
// event despite AddDirectoryUnit having returned an error.
func TestAddDirectoryUnit_PartialAddFailure_UnwindsPriorWatches(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Players"), 0o755); err != nil {
		t.Fatalf("mkdir Players: %v", err)
	}

	w, err := New(WithDebounceDuration(testDebounce))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { w.Close() })

	realAdd := w.addWatch
	calls := 0
	w.addWatch = func(dir string) error {
		calls++
		if calls == 2 {
			return fmt.Errorf("simulated watch failure")
		}
		return realAdd(dir)
	}

	if err := w.AddDirectoryUnit(root, nil); err == nil {
		t.Fatal("expected error from simulated partial failure")
	}

	os.WriteFile(filepath.Join(root, "Level.sav"), []byte("data"), 0o644)
	expectNoEvent(t, w.Events())
}

// TestAddDirectoryUnit_ReAdd_ClearsStaleDirUnitRootOf proves a re-add for
// the same root (as happens on a rescan) clears the previous membership
// first: a directory present in the old membership but absent from the new
// one must not linger in dirUnitRootOf, unreleasable by any future Remove.
func TestAddDirectoryUnit_ReAdd_ClearsStaleDirUnitRootOf(t *testing.T) {
	root := t.TempDir()
	staleDir := filepath.Join(root, "OldSlot")
	if err := os.MkdirAll(staleDir, 0o755); err != nil {
		t.Fatalf("mkdir OldSlot: %v", err)
	}

	w, err := New(WithDebounceDuration(testDebounce))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { w.Close() })

	if err := w.AddDirectoryUnit(root, nil); err != nil {
		t.Fatalf("first AddDirectoryUnit: %v", err)
	}

	// Simulate a rescan where OldSlot no longer exists under root.
	if err := os.RemoveAll(staleDir); err != nil {
		t.Fatalf("remove OldSlot: %v", err)
	}
	if err := w.AddDirectoryUnit(root, nil); err != nil {
		t.Fatalf("second AddDirectoryUnit: %v", err)
	}

	w.mu.Lock()
	_, stale := w.dirUnitRootOf[staleDir]
	w.mu.Unlock()
	if stale {
		t.Error("dirUnitRootOf retains a stale entry for a directory no longer present under the re-added root")
	}
}

// TestAddDirectoryUnit_ReAdd_ReleasesDroppedDirWatch proves the Q4 fix: when
// a re-add's new membership drops a directory that is STILL PRESENT on disk
// (unlike TestAddDirectoryUnit_ReAdd_ClearsStaleDirUnitRootOf above, which
// removes it from disk first), AddDirectoryUnit must release its underlying
// fsnotify watch, not just its bookkeeping — otherwise the watch leaks
// forever and a write there keeps silently producing events for a directory
// the daemon no longer considers part of the unit.
func TestAddDirectoryUnit_ReAdd_ReleasesDroppedDirWatch(t *testing.T) {
	root := t.TempDir()
	droppedDir := filepath.Join(root, "OldSlot")
	if err := os.MkdirAll(droppedDir, 0o755); err != nil {
		t.Fatalf("mkdir OldSlot: %v", err)
	}

	w, err := New(WithDebounceDuration(testDebounce))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { w.Close() })

	if err := w.AddDirectoryUnit(root, nil); err != nil {
		t.Fatalf("first AddDirectoryUnit: %v", err)
	}

	// Simulate a rescan whose new membership no longer includes OldSlot,
	// even though it remains on disk (e.g. excluded by a config change).
	if err := w.AddDirectoryUnit(root, []string{"OldSlot"}); err != nil {
		t.Fatalf("second AddDirectoryUnit: %v", err)
	}

	os.WriteFile(filepath.Join(droppedDir, "stale.sav"), []byte("data"), 0o644)
	expectNoEvent(t, w.Events())
}

// --- Dynamic subdirectory creation under a directory-unit root ---

// TestAddDirectoryUnit_DynamicSubdirCreate_WatchesAndRoutesToRoot covers the
// gap this task closes: a subdirectory (e.g. Players/) appearing under a
// directory-unit root AFTER AddDirectoryUnit's one-shot walk must still be
// watched, and a member file written inside it must produce exactly one
// coalesced root event — not silence until the next full rescan.
func TestAddDirectoryUnit_DynamicSubdirCreate_WatchesAndRoutesToRoot(t *testing.T) {
	root := t.TempDir()

	w, err := New(WithDebounceDuration(testDebounce))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { w.Close() })

	if err := w.AddDirectoryUnit(root, nil); err != nil {
		t.Fatalf("AddDirectoryUnit: %v", err)
	}

	playersDir := filepath.Join(root, "Players")
	if err := os.MkdirAll(playersDir, 0o755); err != nil {
		t.Fatalf("mkdir Players: %v", err)
	}
	// Give the watch time to attach before writing into the new directory —
	// mirrors the real race the fix must close (files can land before the
	// watch on the freshly created directory is registered).
	time.Sleep(testDebounce / 2)
	os.WriteFile(filepath.Join(playersDir, "player1.sav"), []byte("player data"), 0o644)

	ev := waitForEvent(t, w.Events())
	if ev.Path != root {
		t.Errorf("event path = %q, want directory-unit root %q", ev.Path, root)
	}
	if ev.Op != daemon.FileModify {
		t.Errorf("op = %d, want FileModify (%d)", ev.Op, daemon.FileModify)
	}

	expectNoEvent(t, w.Events())
}

// TestAddDirectoryUnit_DynamicSubdirCreate_AlreadyPresentChildRoutesToRoot
// proves a file that lands inside the new subdirectory before the watch
// attaches is still picked up: the root event fires once the directory
// quiesces, driving the daemon's on-disk resnapshot that would otherwise
// miss it entirely until the next rescan.
func TestAddDirectoryUnit_DynamicSubdirCreate_AlreadyPresentChildRoutesToRoot(t *testing.T) {
	root := t.TempDir()

	w, err := New(WithDebounceDuration(testDebounce))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { w.Close() })

	if err := w.AddDirectoryUnit(root, nil); err != nil {
		t.Fatalf("AddDirectoryUnit: %v", err)
	}

	playersDir := filepath.Join(root, "Players")
	if err := os.MkdirAll(playersDir, 0o755); err != nil {
		t.Fatalf("mkdir Players: %v", err)
	}
	// Write immediately, racing the watch attachment — no sleep.
	os.WriteFile(filepath.Join(playersDir, "player1.sav"), []byte("player data"), 0o644)

	ev := waitForEvent(t, w.Events())
	if ev.Path != root {
		t.Errorf("event path = %q, want directory-unit root %q", ev.Path, root)
	}
}

// TestAddDirectoryUnit_DynamicExcludedSubdirCreate_NoEvents confirms a
// subdirectory created dynamically that matches the unit's excludeDirs list
// (e.g. a backup folder appearing later) is never watched: writes inside it
// never surface, dynamically or otherwise.
func TestAddDirectoryUnit_DynamicExcludedSubdirCreate_NoEvents(t *testing.T) {
	root := t.TempDir()

	w, err := New(WithDebounceDuration(testDebounce))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { w.Close() })

	if err := w.AddDirectoryUnit(root, []string{"backup"}); err != nil {
		t.Fatalf("AddDirectoryUnit: %v", err)
	}

	backupDir := filepath.Join(root, "backup")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatalf("mkdir backup: %v", err)
	}
	time.Sleep(testDebounce / 2)
	os.WriteFile(filepath.Join(backupDir, "Level.sav.bak"), []byte("old"), 0o644)

	expectNoEvent(t, w.Events())
}

// TestAddDirectoryUnit_Remove_ReleasesDynamicallyAddedSubdir proves Remove(root)
// releases watches added dynamically after AddDirectoryUnit, not just the
// directories present at add time.
func TestAddDirectoryUnit_Remove_ReleasesDynamicallyAddedSubdir(t *testing.T) {
	root := t.TempDir()

	w, err := New(WithDebounceDuration(testDebounce))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { w.Close() })

	if err := w.AddDirectoryUnit(root, nil); err != nil {
		t.Fatalf("AddDirectoryUnit: %v", err)
	}

	playersDir := filepath.Join(root, "Players")
	if err := os.MkdirAll(playersDir, 0o755); err != nil {
		t.Fatalf("mkdir Players: %v", err)
	}
	time.Sleep(testDebounce / 2)

	if err := w.Remove(root); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	os.WriteFile(filepath.Join(playersDir, "player1.sav"), []byte("data"), 0o644)
	expectNoEvent(t, w.Events())
}

// TestAddDirectoryUnit_DynamicSubdirCreate_DuplicateEventNoDuplicateEntry
// proves the S5a dedup: a duplicate Create for a subdirectory already
// registered under this unit (e.g. a redundant fsnotify event) must not add
// a second dirUnits/dirUnitRootOf entry for it.
func TestAddDirectoryUnit_DynamicSubdirCreate_DuplicateEventNoDuplicateEntry(t *testing.T) {
	root := t.TempDir()

	w, err := New(WithDebounceDuration(testDebounce))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { w.Close() })

	if err := w.AddDirectoryUnit(root, nil); err != nil {
		t.Fatalf("AddDirectoryUnit: %v", err)
	}

	instancesDir := filepath.Join(root, "Instances")
	if err := os.MkdirAll(instancesDir, 0o755); err != nil {
		t.Fatalf("mkdir Instances: %v", err)
	}
	time.Sleep(testDebounce / 2) // let the real fsnotify Create attach the watch

	w.mu.Lock()
	before := len(w.dirUnits[root])
	w.mu.Unlock()

	// Simulate a duplicate Create event for the same, already-registered
	// subdirectory. handleDirUnitSubdirCreate is exercised directly since a
	// second real fsnotify Create for an existing directory isn't reliably
	// reproducible across platforms.
	if ok := w.handleDirUnitSubdirCreate(instancesDir); !ok {
		t.Fatal("handleDirUnitSubdirCreate returned false for an already-registered directory-unit subdirectory")
	}

	w.mu.Lock()
	after := len(w.dirUnits[root])
	dupCount := 0
	for _, d := range w.dirUnits[root] {
		if d == instancesDir {
			dupCount++
		}
	}
	w.mu.Unlock()

	if after != before {
		t.Errorf("dirUnits[root] grew from %d to %d entries after a duplicate Create", before, after)
	}
	if dupCount != 1 {
		t.Errorf("dirUnits[root] contains %q %d times, want 1 (dedup)", instancesDir, dupCount)
	}
}

// TestAddDirectoryUnit_SubdirRemoved_ClearsOwnBookkeeping proves the S5b
// fix: removing a directory-unit subdirectory from disk must clear its own
// dirUnitRootOf/dirUnits entry (not just member-file state beneath it) —
// otherwise a later recreation of the same path is silently treated as
// "already watched" by the S5a dedup and never re-added, which the final
// recreate-and-check step below proves.
func TestAddDirectoryUnit_SubdirRemoved_ClearsOwnBookkeeping(t *testing.T) {
	root := t.TempDir()
	instancesDir := filepath.Join(root, "Instances")
	if err := os.MkdirAll(instancesDir, 0o755); err != nil {
		t.Fatalf("mkdir Instances: %v", err)
	}

	w, err := New(WithDebounceDuration(testDebounce))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { w.Close() })

	if err := w.AddDirectoryUnit(root, nil); err != nil {
		t.Fatalf("AddDirectoryUnit: %v", err)
	}

	w.mu.Lock()
	_, watched := w.dirUnitRootOf[instancesDir]
	w.mu.Unlock()
	if !watched {
		t.Fatal("setup: Instances not registered by AddDirectoryUnit")
	}

	if err := os.RemoveAll(instancesDir); err != nil {
		t.Fatalf("remove Instances: %v", err)
	}
	time.Sleep(testDebounce / 2) // handleRemove runs off the async fsnotify event

	w.mu.Lock()
	_, stillThere := w.dirUnitRootOf[instancesDir]
	dupCount := 0
	for _, d := range w.dirUnits[root] {
		if d == instancesDir {
			dupCount++
		}
	}
	w.mu.Unlock()
	if stillThere {
		t.Error("dirUnitRootOf retains a stale entry for a removed directory-unit subdirectory")
	}
	if dupCount != 0 {
		t.Errorf("dirUnits[root] still contains removed subdirectory %q", instancesDir)
	}

	// Recreate: must be treated as fresh, not silently skipped as
	// "already watched" — the create/delete/recreate cycle this test covers.
	if err := os.MkdirAll(instancesDir, 0o755); err != nil {
		t.Fatalf("recreate Instances: %v", err)
	}
	time.Sleep(testDebounce / 2)

	w.mu.Lock()
	readdedRoot, readded := w.dirUnitRootOf[instancesDir]
	w.mu.Unlock()
	if !readded || readdedRoot != root {
		t.Errorf(
			"dirUnitRootOf[%q] after recreate = (%q, %v), want (%q, true)",
			instancesDir, readdedRoot, readded, root,
		)
	}
}

// --- scheduleDirectoryUnitEvent / fireDirectoryUnitEvent generation guard ---

// TestFireDirectoryUnitEvent_StaleGeneration_DoesNotEmit exercises the
// generation guard directly: an invocation carrying a generation older than
// the currently recorded one (as happens when a timer had already fired and
// started running, blocked on w.mu, while a concurrent
// scheduleDirectoryUnitEvent raced ahead and bumped the generation) must
// return without emitting a second, stale event.
func TestFireDirectoryUnitEvent_StaleGeneration_DoesNotEmit(t *testing.T) {
	w, err := New(WithDebounceDuration(testDebounce))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { w.Close() })

	const root = "/fake/root"
	w.mu.Lock()
	w.dirUnitGenerations[root] = 2
	w.mu.Unlock()

	w.fireDirectoryUnitEvent(root, 1)

	expectNoEvent(t, w.Events())
}

// TestFireDirectoryUnitEvent_CurrentGeneration_Emits confirms the guard
// only suppresses a stale generation — a genuine, current firing still
// emits normally.
func TestFireDirectoryUnitEvent_CurrentGeneration_Emits(t *testing.T) {
	w, err := New(WithDebounceDuration(testDebounce))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { w.Close() })

	const root = "/fake/root"
	w.mu.Lock()
	w.dirUnitGenerations[root] = 1
	w.mu.Unlock()

	w.fireDirectoryUnitEvent(root, 1)

	ev := waitForEvent(t, w.Events())
	if ev.Path != root {
		t.Errorf("event path = %q, want %q", ev.Path, root)
	}
}

// TestScheduleDirectoryUnitEvent_TimerAlreadyFired_BumpsGenerationAndReschedules
// exercises scheduleDirectoryUnitEvent's Stop()-fails branch directly: when
// the previously scheduled timer already fired (Stop returns false — the
// race window scheduleDirectoryUnitEvent cannot close by itself), it must
// bump the generation (so the in-flight, already-fired invocation recognizes
// itself as superseded — see fireDirectoryUnitEvent) and install a fresh
// timer, rather than resetting a timer that can no longer be stopped.
func TestScheduleDirectoryUnitEvent_TimerAlreadyFired_BumpsGenerationAndReschedules(t *testing.T) {
	w, err := New(WithDebounceDuration(testDebounce))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { w.Close() })

	const root = "/fake/root"
	alreadyFired := time.AfterFunc(0, func() {})
	time.Sleep(20 * time.Millisecond) // let it actually fire before scheduling again

	w.mu.Lock()
	w.dirUnitTimers[root] = alreadyFired
	w.dirUnitGenerations[root] = 1
	w.mu.Unlock()

	w.scheduleDirectoryUnitEvent(root)

	w.mu.Lock()
	newGen := w.dirUnitGenerations[root]
	newTimer := w.dirUnitTimers[root]
	w.mu.Unlock()

	if newGen != 2 {
		t.Errorf("generation = %d, want 2 (bumped past the already-fired timer's generation)", newGen)
	}
	if newTimer == alreadyFired {
		t.Error("scheduleDirectoryUnitEvent reused the already-fired timer instead of installing a fresh one")
	}

	ev := waitForEvent(t, w.Events())
	if ev.Path != root {
		t.Errorf("event path = %q, want %q", ev.Path, root)
	}
}
