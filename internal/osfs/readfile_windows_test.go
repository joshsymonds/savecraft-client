//go:build windows

package osfs

import (
	"os"
	"path/filepath"
	"testing"
)

// The game's save commit is "write Level.sav.tmp, then rename it over
// Level.sav". These tests pin that a ReadFileShared-style handle never
// blocks that commit — the regression that made Palworld fail its own
// autosave ("could not save") whenever the daemon was mid-read.

func TestOpenShared_AllowsRenameReplaceWhileOpen(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "Level.sav")
	tmp := filepath.Join(dir, "Level.sav.tmp")
	if err := os.WriteFile(target, []byte("old world"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmp, []byte("new world"), 0o644); err != nil {
		t.Fatal(err)
	}

	f, err := openShared(target)
	if err != nil {
		t.Fatalf("openShared: %v", err)
	}
	defer f.Close()

	// The game's commit: rename-replace over the file the daemon is
	// reading. With FILE_SHARE_DELETE on the read handle this must
	// succeed; without it, it fails with a sharing violation.
	if err := os.Rename(tmp, target); err != nil {
		t.Fatalf("rename-replace while a shared read handle is open: %v", err)
	}

	f.Close()
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

	f, err := openShared(target)
	if err != nil {
		t.Fatalf("openShared: %v", err)
	}
	defer f.Close()

	if err := os.Remove(target); err != nil {
		t.Fatalf("delete while a shared read handle is open: %v", err)
	}
}

// Canary for why openShared exists at all: a plain os.Open handle (no
// FILE_SHARE_DELETE) blocks the game's rename-replace. If Go's stdlib ever
// starts opening with FILE_SHARE_DELETE (golang/go#32088), this test fails
// — at which point openShared and the platform split can be deleted in
// favor of os.ReadFile everywhere.
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
			"now shares delete access, so the osfs platform split is obsolete")
	}
}
