//go:build windows

package osfs

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The game's save commit is "write Level.sav.tmp, then rename it over
// Level.sav". These tests pin that a ReadFileShared-style reader never
// makes that commit FAIL — the regression that made Palworld error its
// own autosave ("could not save") whenever the daemon was mid-read.
//
// Windows semantics make this two distinct guarantees:
//   - delete of a held file succeeds outright (FILE_SHARE_DELETE);
//   - rename-OVER a held file cannot succeed while any handle is open,
//     so the RH oplock makes the writer WAIT for our close (bounded by a
//     read's few milliseconds) instead of failing with ACCESS_DENIED.

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

func TestOpenShared_RenameReplaceWaitsForReaderInsteadOfFailing(t *testing.T) {
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

	// The game's commit: rename-replace over the file the daemon is
	// reading. With the RH oplock this must BLOCK until the reader
	// closes, then succeed — never fail.
	start := time.Now()
	if err := os.Rename(tmp, target); err != nil {
		t.Fatalf("rename-replace while a shared read handle is open: %v", err)
	}
	elapsed := time.Since(start)
	<-closed

	if elapsed < hold/2 {
		t.Errorf("rename returned after %v — expected it to wait ~%v for the reader's close", elapsed, hold)
	}

	got, err := ReadFileShared(target)
	if err != nil {
		t.Fatalf("ReadFileShared after replace: %v", err)
	}
	if string(got) != "new world" {
		t.Errorf("post-replace content = %q, want %q", got, "new world")
	}
}

func TestOpenShared_AllowsDeleteWhileOpen(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "Level.sav")
	if err := os.WriteFile(target, []byte("doomed"), 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := openShared(target)
	if err != nil {
		t.Fatalf("openShared: %v", err)
	}
	defer r.Close()

	if err := os.Remove(target); err != nil {
		t.Fatalf("delete while a shared read handle is open: %v", err)
	}
}

// Canary for why this file exists at all: a plain os.Open handle (no
// FILE_SHARE_DELETE, no oplock) makes the game's rename-replace fail. If
// Go's stdlib ever starts yielding to writers (golang/go#32088), this
// test fails — at which point the platform split can be deleted in favor
// of os.ReadFile everywhere.
func TestPlainOsOpen_BlocksRenameReplace(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "Level.sav")
	tmp := filepath.Join(dir, "Level.sav.tmp")
	if err := os.WriteFile(target, []byte("old world"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmp, []byte("new world"), 0o644); err != nil {
		t.Fatal(err)
	}

	f, err := os.Open(target)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	if err := os.Rename(tmp, target); err == nil {
		t.Fatal("os.Rename over a plain os.Open handle succeeded — the stdlib " +
			"now yields to writers, so the osfs platform split is obsolete")
	}
}
