package pluginmgr

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestCache_WriteAndRead(t *testing.T) {
	dir := t.TempDir()
	cache := NewCache(dir)

	wasm := []byte("wasm data")
	sig := []byte("sig data")

	if err := cache.Write("d2r", "1.0.0", wasm, sig); err != nil {
		t.Fatalf("Write: %v", err)
	}

	gotWasm, gotSig, gotVer, err := cache.Read("d2r")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(gotWasm) != "wasm data" {
		t.Errorf("wasm = %q, want %q", gotWasm, "wasm data")
	}
	if string(gotSig) != "sig data" {
		t.Errorf("sig = %q, want %q", gotSig, "sig data")
	}
	if gotVer != "1.0.0" {
		t.Errorf("version = %q, want 1.0.0", gotVer)
	}
}

func TestCache_HasVersion(t *testing.T) {
	dir := t.TempDir()
	cache := NewCache(dir)

	// Not cached yet.
	if cache.HasVersion("d2r", "1.0.0") {
		t.Error("HasVersion should return false for missing game")
	}

	if err := cache.Write("d2r", "1.0.0", []byte("w"), []byte("s")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if !cache.HasVersion("d2r", "1.0.0") {
		t.Error("HasVersion should return true for matching version")
	}
	if cache.HasVersion("d2r", "2.0.0") {
		t.Error("HasVersion should return false for different version")
	}
}

func TestCache_Read_MissingWasm(t *testing.T) {
	dir := t.TempDir()
	cache := NewCache(dir)

	_, _, _, err := cache.Read("d2r")
	if err == nil {
		t.Fatal("expected error reading missing cache")
	}
}

func TestCache_Read_MissingSig(t *testing.T) {
	dir := t.TempDir()
	cache := NewCache(dir)

	gameDir := filepath.Join(dir, "d2r")
	if err := os.MkdirAll(gameDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(gameDir, "parser.wasm"), []byte("w"), 0o600,
	); err != nil {
		t.Fatal(err)
	}

	_, _, _, err := cache.Read("d2r")
	if err == nil {
		t.Fatal("expected error when sig file missing")
	}
}

func TestCache_Read_MissingVersion(t *testing.T) {
	dir := t.TempDir()
	cache := NewCache(dir)

	gameDir := filepath.Join(dir, "d2r")
	if err := os.MkdirAll(gameDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(gameDir, "parser.wasm"), []byte("w"), 0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(gameDir, "parser.wasm.sig"), []byte("s"), 0o600,
	); err != nil {
		t.Fatal(err)
	}

	_, _, _, err := cache.Read("d2r")
	if err == nil {
		t.Fatal("expected error when version.txt missing")
	}
}

func TestCache_SHA256_AfterWrite(t *testing.T) {
	dir := t.TempDir()
	cache := NewCache(dir)

	wasm := []byte("wasm data")
	if err := cache.Write("d2r", "1.0.0", wasm, []byte("sig")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	h := sha256.Sum256(wasm)
	want := fmt.Sprintf("%x", h)
	got := cache.SHA256("d2r")
	if got != want {
		t.Errorf("SHA256 = %q, want %q", got, want)
	}
}

func TestCache_SHA256_Missing(t *testing.T) {
	dir := t.TempDir()
	cache := NewCache(dir)

	got := cache.SHA256("nonexistent")
	if got != "" {
		t.Errorf("SHA256 = %q, want empty string for missing plugin", got)
	}
}

func TestCache_UpdateVersion(t *testing.T) {
	dir := t.TempDir()
	cache := NewCache(dir)

	wasm := []byte("wasm data")
	sig := []byte("sig data")
	if err := cache.Write("d2r", "1.0.0", wasm, sig); err != nil {
		t.Fatalf("Write: %v", err)
	}

	newHash := "deadbeef"
	if err := cache.UpdateVersion("d2r", "2.0.0", newHash); err != nil {
		t.Fatalf("UpdateVersion: %v", err)
	}

	// version.txt updated
	if !cache.HasVersion("d2r", "2.0.0") {
		t.Error("version should be 2.0.0 after UpdateVersion")
	}
	if cache.HasVersion("d2r", "1.0.0") {
		t.Error("version should no longer be 1.0.0")
	}

	// sha256.txt updated
	if got := cache.SHA256("d2r"); got != newHash {
		t.Errorf("SHA256 = %q, want %q", got, newHash)
	}

	// wasm and sig unchanged
	gotWasm, gotSig, _, err := cache.Read("d2r")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(gotWasm) != "wasm data" {
		t.Errorf("wasm = %q, want %q (should be unchanged)", gotWasm, "wasm data")
	}
	if string(gotSig) != "sig data" {
		t.Errorf("sig = %q, want %q (should be unchanged)", gotSig, "sig data")
	}
}

func TestDefaultCacheDir_EnvOverride(t *testing.T) {
	t.Setenv("SAVECRAFT_CACHE_DIR", "/tmp/custom-cache")
	got := DefaultCacheDir("savecraft")
	if got != "/tmp/custom-cache" {
		t.Errorf("DefaultCacheDir = %q, want /tmp/custom-cache", got)
	}
}

func TestDefaultCacheDir_Default(t *testing.T) {
	t.Setenv("SAVECRAFT_CACHE_DIR", "")
	got := DefaultCacheDir("savecraft")
	if got == "" {
		t.Error("DefaultCacheDir returned empty string")
	}
}

func TestDefaultCacheDir_XDGDataHome(t *testing.T) {
	t.Setenv("SAVECRAFT_CACHE_DIR", "")
	t.Setenv("XDG_DATA_HOME", "/tmp/xdg-test")
	got := DefaultCacheDir("savecraft")
	want := "/tmp/xdg-test/savecraft/plugins"
	if got != want {
		t.Errorf("DefaultCacheDir = %q, want %q", got, want)
	}
}

func TestDefaultCacheDir_XDGDataHome_Staging(t *testing.T) {
	t.Setenv("SAVECRAFT_CACHE_DIR", "")
	t.Setenv("XDG_DATA_HOME", "/tmp/xdg-test")
	got := DefaultCacheDir("savecraft-staging")
	want := "/tmp/xdg-test/savecraft-staging/plugins"
	if got != want {
		t.Errorf("DefaultCacheDir = %q, want %q", got, want)
	}
}
