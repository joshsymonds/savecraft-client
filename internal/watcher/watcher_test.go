package watcher

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/joshsymonds/savecraft.gg/internal/daemon"
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
