package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigCache_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	games := map[string]GameConfig{
		"d2r": {SavePath: "/saves/d2r", FileExtensions: []string{".d2s"}, Enabled: true},
		"sdv": {SavePath: "/saves/sdv", FileExtensions: []string{".xml"}, Enabled: false},
	}

	if err := saveConfigCache(dir, games); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := loadConfigCache(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("loaded %d games, want 2", len(loaded))
	}
	if loaded["d2r"].SavePath != "/saves/d2r" {
		t.Errorf("d2r SavePath = %s", loaded["d2r"].SavePath)
	}
	if !loaded["d2r"].Enabled {
		t.Error("d2r should be enabled")
	}
	if loaded["sdv"].Enabled {
		t.Error("sdv should be disabled")
	}
	if len(loaded["d2r"].FileExtensions) != 1 || loaded["d2r"].FileExtensions[0] != ".d2s" {
		t.Errorf("d2r FileExtensions = %v", loaded["d2r"].FileExtensions)
	}
}

func TestConfigCache_MissingFile(t *testing.T) {
	dir := t.TempDir()
	games, err := loadConfigCache(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if games != nil {
		t.Errorf("expected nil for missing file, got %v", games)
	}
}

func TestConfigCache_CorruptFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, configCacheFile), []byte("{invalid json"), 0o644); err != nil {
		t.Fatal(err)
	}
	games, err := loadConfigCache(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if games != nil {
		t.Errorf("expected nil for corrupt file, got %v", games)
	}
}

func TestConfigCache_EmptyDir(t *testing.T) {
	games, err := loadConfigCache("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if games != nil {
		t.Errorf("expected nil for empty dir, got %v", games)
	}
}

func TestConfigCache_CreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "config")
	games := map[string]GameConfig{
		"d2r": {SavePath: "/saves/d2r", Enabled: true},
	}
	if err := saveConfigCache(dir, games); err != nil {
		t.Fatalf("save: %v", err)
	}
	// Verify the file was created
	if _, err := os.Stat(filepath.Join(dir, configCacheFile)); err != nil {
		t.Errorf("config file not created: %v", err)
	}
}
