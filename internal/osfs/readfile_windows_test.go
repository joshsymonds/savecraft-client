//go:build windows

package osfs

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// These tests pin how a ReadFileShared-style reader interacts with a game
// process committing a save. Windows gives three distinct semantics, all
// exercised below:
//
//   - DELETE of the held file: the RH oplock breaks and the deleting
//     process WAITS for our close (bounded by a read's few milliseconds),
//     after which the delete completes fully — the name is freed, not
//     left in delete-pending limbo.
//   - Legacy rename-REPLACE (MoveFileEx) over the held file: NOT
//     oplock-mediated; it fails on any open target handle no matter the
//     share mode. Games that commit this way cannot be protected by a
//     reader — but UE5 (Palworld) commits as delete-then-rename, which
//     the oplock fully covers.
//   - Plain os.Open (stdlib): no share-delete, no oplock — the game's
//     delete AND replace both fail. This is the regression that made
//     Palworld error its own autosave ("could not save").

func TestReadFileShared_ReadsContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Level.sav")
	want := []byte("live save bytes, windows edition")
	if err := os.WriteFile(path, want, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ReadFileShared(path)
	if err != nil {
		t.Fatalf("ReadFileShared: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("content = %q, want %q", got, want)
	}
}

// The UE5 save commit — delete the destination, then rename the temp file
// into place — must SUCCEED while a shared read handle is held, with the
// delete waiting out the reader instead of failing. This is the exact
// pattern Palworld uses for Level.sav.
func TestSaveCommit_DeleteThenRenameSucceedsDuringHeldRead(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "Level.sav")
	tmp := filepath.Join(dir, "Level.sav.tmp")
	if err := os.WriteFile(target, []byte("old world"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmp, []byte("new world"), 0o644); err != nil {
		t.Fatal(err)
	}

	const hold = 300 * time.Millisecond
	r, err := openShared(target)
	if err != nil {
		t.Fatalf("openShared: %v", err)
	}
	if r.oplockEvent == 0 {
		r.Close()
		t.Skip("RH oplock not granted on this filesystem — yield semantics unavailable")
	}
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		time.Sleep(hold)
		r.Close()
	}()

	start := time.Now()
	if err := os.Remove(target); err != nil {
		t.Fatalf("delete of held file must wait for the reader and succeed, got: %v", err)
	}
	deleteElapsed := time.Since(start)
	if err := os.Rename(tmp, target); err != nil {
		t.Fatalf("rename into the freed name must succeed, got: %v", err)
	}
	<-closed

	if deleteElapsed < hold/2 {
		t.Errorf("delete returned after %v — expected it to wait ~%v for the reader's close (oplock break)", deleteElapsed, hold)
	}

	got, err := ReadFileShared(target)
	if err != nil {
		t.Fatalf("ReadFileShared after commit: %v", err)
	}
	if string(got) != "new world" {
		t.Errorf("post-commit content = %q, want %q", got, "new world")
	}
}

// Legacy rename-replace (MoveFileEx, what os.Rename uses) over a file
// with ANY open handle fails regardless of share mode or oplock — the
// operation is not oplock-mediated. Documented as an inherent limit: a
// game committing this way is only safe because our reads are
// milliseconds long. If this test ever starts passing, Windows/Go
// semantics changed and the delete-then-rename distinction can go.
func TestLegacyRenameReplace_StillFailsOverHeldHandle(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "Level.sav")
	tmp := filepath.Join(dir, "Level.sav.tmp")
	if err := os.WriteFile(target, []byte("old world"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmp, []byte("new world"), 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := openShared(target)
	if err != nil {
		t.Fatalf("openShared: %v", err)
	}
	defer r.Close()

	if err := os.Rename(tmp, target); err == nil {
		t.Fatal("rename-replace over a held handle succeeded — Windows/Go semantics " +
			"changed; revisit the delete-then-rename distinction in this file's comments")
	}
}

// Canary for why this file exists at all: a plain os.Open handle (no
// FILE_SHARE_DELETE, no oplock) makes the game's delete fail outright. If
// Go's stdlib ever starts yielding to writers (golang/go#32088), this
// fails — at which point the platform split can be deleted in favor of
// os.ReadFile everywhere.
func TestPlainOsOpen_BlocksDelete(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "Level.sav")
	if err := os.WriteFile(target, []byte("old world"), 0o644); err != nil {
		t.Fatal(err)
	}

	f, err := os.Open(target)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	if err := os.Remove(target); err == nil {
		t.Fatal("delete over a plain os.Open handle succeeded — the stdlib " +
			"now shares delete access, so the osfs platform split is obsolete")
	}
}
