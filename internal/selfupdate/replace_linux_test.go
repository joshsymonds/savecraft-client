//go:build linux

package selfupdate

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func deviceOf(t *testing.T, path string) uint64 {
	t.Helper()
	var st syscall.Stat_t
	if err := syscall.Stat(path, &st); err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return uint64(st.Dev) //nolint:unconvert // Dev is int32 on some platforms
}

// The update is downloaded under the data dir (~/.local/share/savecraft/...)
// while the binary lives elsewhere (~/.local/bin, or a Steam Deck's separate
// mount): a bare rename fails with EXDEV. Prod 2026-08-18: three Linux
// daemons failing "replace binary: rename ...: invalid cross-device link" on
// every reconnect. Replacement must survive crossing filesystems.
func TestReplaceBinary_AcrossFilesystems(t *testing.T) {
	shm := "/dev/shm"
	if _, err := os.Stat(shm); err != nil {
		t.Skip("no /dev/shm on this host")
	}
	targetDir := t.TempDir()
	if deviceOf(t, shm) == deviceOf(t, targetDir) {
		t.Skip("/dev/shm and TMPDIR are on the same filesystem; cannot exercise EXDEV")
	}
	// Deliberately not t.TempDir(): it must sit on a different filesystem.
	srcDir, err := os.MkdirTemp(shm, "savecraft-update-") //nolint:usetesting // cross-device fixture
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(srcDir) })

	newBinary := filepath.Join(srcDir, "daemon-update.tmp")
	if err := os.WriteFile(newBinary, []byte("new-daemon-v2"), 0o600); err != nil {
		t.Fatalf("write new: %v", err)
	}
	binaryPath := filepath.Join(targetDir, "savecraft-daemon")
	if err := os.WriteFile(binaryPath, []byte("old-daemon"), 0o700); err != nil {
		t.Fatalf("write old: %v", err)
	}

	if err := replaceBinary(newBinary, binaryPath); err != nil {
		t.Fatalf("replaceBinary across filesystems: %v", err)
	}
	got, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatalf("read replaced: %v", err)
	}
	if string(got) != "new-daemon-v2" {
		t.Fatalf("content = %q, want new-daemon-v2", got)
	}
	info, err := os.Stat(binaryPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o100 == 0 {
		t.Fatalf("replaced binary is not executable: %v", info.Mode())
	}
	if _, err := os.Stat(newBinary); !os.IsNotExist(err) {
		t.Fatalf("downloaded temp file should be consumed, stat err = %v", err)
	}
}
