package pluginmgr

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestPluginWatcher_DetectsWASMChange(t *testing.T) {
	localDir := t.TempDir()
	gameDir := filepath.Join(localDir, "d2r")
	if err := os.MkdirAll(gameDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gameDir, "parser.wasm"), []byte("v1"), 0o600); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var reloaded []string
	callback := func(gameID string) {
		mu.Lock()
		reloaded = append(reloaded, gameID)
		mu.Unlock()
	}

	pw, err := NewPluginWatcher(localDir, callback)
	if err != nil {
		t.Fatalf("NewPluginWatcher: %v", err)
	}
	defer pw.Close()

	// Modify the WASM file.
	if err := os.WriteFile(filepath.Join(gameDir, "parser.wasm"), []byte("v2"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Wait for debounce + processing.
	deadline := time.After(3 * time.Second)
	for {
		mu.Lock()
		n := len(reloaded)
		mu.Unlock()
		if n > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for reload callback")
		case <-time.After(50 * time.Millisecond):
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if len(reloaded) != 1 || reloaded[0] != "d2r" {
		t.Errorf("reloaded = %v, want [d2r]", reloaded)
	}
}

func TestPluginWatcher_DebounceRapidWrites(t *testing.T) {
	localDir := t.TempDir()
	gameDir := filepath.Join(localDir, "d2r")
	if err := os.MkdirAll(gameDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gameDir, "parser.wasm"), []byte("v1"), 0o600); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var reloaded []string
	callback := func(gameID string) {
		mu.Lock()
		reloaded = append(reloaded, gameID)
		mu.Unlock()
	}

	pw, err := NewPluginWatcher(localDir, callback, WithWatcherDebounce(200*time.Millisecond))
	if err != nil {
		t.Fatalf("NewPluginWatcher: %v", err)
	}
	defer pw.Close()

	// Rapid-fire writes.
	for i := range 5 {
		data := []byte("rapid write " + string(rune('0'+i)))
		if err := os.WriteFile(filepath.Join(gameDir, "parser.wasm"), data, 0o600); err != nil {
			t.Fatal(err)
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Wait for exactly 1 callback via polling (more robust than fixed sleep).
	deadline := time.After(3 * time.Second)
	for {
		mu.Lock()
		n := len(reloaded)
		mu.Unlock()
		if n > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for debounced callback")
		case <-time.After(50 * time.Millisecond):
		}
	}

	// Give extra time to ensure no additional callbacks fire.
	time.Sleep(300 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	// Should have coalesced into exactly 1 callback.
	if len(reloaded) != 1 {
		t.Errorf("reloaded count = %d, want 1 (debounced)", len(reloaded))
	}
}

func TestPluginWatcher_IgnoresNonWASMFiles(t *testing.T) {
	localDir := t.TempDir()
	gameDir := filepath.Join(localDir, "d2r")
	if err := os.MkdirAll(gameDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gameDir, "parser.wasm"), []byte("v1"), 0o600); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var reloaded []string
	callback := func(gameID string) {
		mu.Lock()
		reloaded = append(reloaded, gameID)
		mu.Unlock()
	}

	pw, err := NewPluginWatcher(localDir, callback)
	if err != nil {
		t.Fatalf("NewPluginWatcher: %v", err)
	}
	defer pw.Close()

	// Write a non-WASM file — should be ignored.
	if err := os.WriteFile(filepath.Join(gameDir, "notes.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Wait a bit to ensure no callback fires.
	time.Sleep(800 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(reloaded) != 0 {
		t.Errorf("reloaded = %v, want empty (non-WASM ignored)", reloaded)
	}
}

func TestPluginWatcher_ExtractsGameID(t *testing.T) {
	localDir := t.TempDir()
	for _, gameID := range []string{"d2r", "rimworld"} {
		gameDir := filepath.Join(localDir, gameID)
		if err := os.MkdirAll(gameDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(gameDir, "parser.wasm"), []byte("v1-"+gameID), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	var mu sync.Mutex
	var reloaded []string
	callback := func(gameID string) {
		mu.Lock()
		reloaded = append(reloaded, gameID)
		mu.Unlock()
	}

	pw, err := NewPluginWatcher(localDir, callback)
	if err != nil {
		t.Fatalf("NewPluginWatcher: %v", err)
	}
	defer pw.Close()

	// Modify both.
	if err := os.WriteFile(filepath.Join(localDir, "d2r", "parser.wasm"), []byte("v2-d2r"), 0o600); err != nil {
		t.Fatal(err)
	}
	rimworldWasm := filepath.Join(localDir, "rimworld", "parser.wasm")
	if err := os.WriteFile(rimworldWasm, []byte("v2-rimworld"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Wait for callbacks.
	deadline := time.After(3 * time.Second)
	for {
		mu.Lock()
		n := len(reloaded)
		mu.Unlock()
		if n >= 2 {
			break
		}
		select {
		case <-deadline:
			mu.Lock()
			t.Fatalf("timed out, reloaded = %v", reloaded)
			mu.Unlock()
		case <-time.After(50 * time.Millisecond):
		}
	}

	mu.Lock()
	defer mu.Unlock()
	got := map[string]bool{}
	for _, g := range reloaded {
		got[g] = true
	}
	if !got["d2r"] || !got["rimworld"] {
		t.Errorf("reloaded = %v, want both d2r and rimworld", reloaded)
	}
}
