package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectDevice_Valve(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "board_vendor")
	if err := os.WriteFile(path, []byte("Valve\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := detectDeviceFrom(path); got != "steam_deck" {
		t.Errorf("detectDeviceFrom() = %q, want %q", got, "steam_deck")
	}
}

func TestDetectDevice_Other(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "board_vendor")
	if err := os.WriteFile(path, []byte("ASUSTeK\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := detectDeviceFrom(path); got != "" {
		t.Errorf("detectDeviceFrom() = %q, want %q", got, "")
	}
}

func TestDetectDevice_NoFile(t *testing.T) {
	if got := detectDeviceFrom("/nonexistent/path/board_vendor"); got != "" {
		t.Errorf("detectDeviceFrom() = %q, want %q", got, "")
	}
}
