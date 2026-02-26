package osfs

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/joshsymonds/savecraft.gg/internal/daemon"
)

// Verify OSFS implements daemon.FS at compile time.
var _ daemon.FS = (*OSFS)(nil)

func TestStat_ExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("hello"), 0o644)

	osfs := New()
	info, err := osfs.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Name() != "test.txt" {
		t.Errorf("Name = %q, want test.txt", info.Name())
	}
	if info.Size() != 5 {
		t.Errorf("Size = %d, want 5", info.Size())
	}
}

func TestStat_NotFound(t *testing.T) {
	osfs := New()
	_, err := osfs.Stat("/nonexistent/path")
	if err == nil {
		t.Fatal("expected error for nonexistent path")
	}
}

func TestReadFile_Success(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.bin")
	os.WriteFile(path, []byte("content"), 0o644)

	osfs := New()
	data, err := osfs.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "content" {
		t.Errorf("data = %q, want content", string(data))
	}
}

func TestReadFile_NotFound(t *testing.T) {
	osfs := New()
	_, err := osfs.ReadFile("/nonexistent/file")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestReadDir_Success(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0o644)
	os.Mkdir(filepath.Join(dir, "subdir"), 0o755)

	osfs := New()
	entries, err := osfs.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}

	names := make(map[string]bool)
	for _, entry := range entries {
		names[entry.Name()] = true
	}

	for _, want := range []string{"a.txt", "b.txt", "subdir"} {
		if !names[want] {
			t.Errorf("missing entry %q", want)
		}
	}
}

func TestReadDir_IncludesTypes(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "file.txt"), []byte("x"), 0o644)
	os.Mkdir(filepath.Join(dir, "subdir"), 0o755)

	osfs := New()
	entries, err := osfs.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}

	for _, entry := range entries {
		switch entry.Name() {
		case "file.txt":
			if entry.IsDir() {
				t.Error("file.txt reported as directory")
			}
			if entry.Type()&fs.ModeDir != 0 {
				t.Error("file.txt has directory type bit")
			}
		case "subdir":
			if !entry.IsDir() {
				t.Error("subdir not reported as directory")
			}
		}
	}
}

func TestReadDir_NotFound(t *testing.T) {
	osfs := New()
	_, err := osfs.ReadDir("/nonexistent/dir")
	if err == nil {
		t.Fatal("expected error for nonexistent directory")
	}
}
