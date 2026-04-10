package daemon

import (
	"os"
	"runtime"
	"testing"
)

func TestResolveKnownFolder_Documents(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	path, err := resolveKnownFolder("DOCUMENTS")
	if err != nil {
		t.Fatalf("resolveKnownFolder(DOCUMENTS) error: %v", err)
	}
	if path == "" {
		t.Fatal("resolveKnownFolder(DOCUMENTS) returned empty string")
	}
	// On Linux/macOS stub, should be home + /Documents.
	if runtime.GOOS != "windows" {
		want := home + "/Documents"
		if path != want {
			t.Errorf("resolveKnownFolder(DOCUMENTS) = %q, want %q", path, want)
		}
	}
}

func TestResolveKnownFolder_SavedGames(t *testing.T) {
	path, err := resolveKnownFolder("SAVED_GAMES")
	if runtime.GOOS == "windows" {
		// On Windows, Saved Games should resolve.
		if err != nil {
			t.Fatalf("resolveKnownFolder(SAVED_GAMES) error: %v", err)
		}
		if path == "" {
			t.Fatal("resolveKnownFolder(SAVED_GAMES) returned empty string")
		}
	} else if err == nil {
		// On non-Windows, Saved Games has no equivalent — should error.
		t.Fatal("resolveKnownFolder(SAVED_GAMES) should error on non-Windows")
	}
}

func TestResolveKnownFolder_LocalAppData(t *testing.T) {
	path, err := resolveKnownFolder("LOCALAPPDATA")
	if err != nil {
		t.Fatalf("resolveKnownFolder(LOCALAPPDATA) error: %v", err)
	}
	if path == "" {
		t.Fatal("resolveKnownFolder(LOCALAPPDATA) returned empty string")
	}
}

func TestResolveKnownFolder_LocalAppDataLow(t *testing.T) {
	path, err := resolveKnownFolder("LOCALAPPDATA_LOW")
	if err != nil {
		t.Fatalf("resolveKnownFolder(LOCALAPPDATA_LOW) error: %v", err)
	}
	if runtime.GOOS == "windows" && path == "" {
		t.Fatal("resolveKnownFolder(LOCALAPPDATA_LOW) returned empty string on Windows")
	}
}

func TestResolveKnownFolder_Unknown(t *testing.T) {
	_, err := resolveKnownFolder("NONEXISTENT_FOLDER")
	if err == nil {
		t.Fatal("resolveKnownFolder(NONEXISTENT_FOLDER) should return error")
	}
}
