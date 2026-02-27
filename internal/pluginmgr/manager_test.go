package pluginmgr

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"strings"

	"github.com/joshsymonds/savecraft.gg/internal/signing"
)

// --- Fakes ---

type fakeRegistry struct {
	manifest map[string]PluginInfo
	files    map[string][]byte
	fetchErr error
}

func (r *fakeRegistry) FetchManifest(
	_ context.Context,
) (map[string]PluginInfo, error) {
	if r.fetchErr != nil {
		return nil, r.fetchErr
	}
	return r.manifest, nil
}

func (r *fakeRegistry) Download(
	_ context.Context, url string,
) ([]byte, error) {
	data, ok := r.files[url]
	if !ok {
		return nil, fmt.Errorf("not found: %s", url)
	}
	return data, nil
}

type fakeLoader struct {
	loaded  []string
	loadErr map[string]error
	mu      sync.Mutex
}

func (l *fakeLoader) LoadPlugin(
	_ context.Context, gameID string, _, _ []byte,
) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.loaded = append(l.loaded, gameID)
	if l.loadErr != nil {
		if err, ok := l.loadErr[gameID]; ok {
			return err
		}
	}
	return nil
}

// --- Helpers ---

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(
		os.Stderr, &slog.HandlerOptions{Level: slog.LevelError},
	))
}

func generateTestKeys(
	t *testing.T,
) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := signing.GenerateKeypair()
	if err != nil {
		t.Fatalf("generate keypair: %v", err)
	}
	return pub, priv
}

func signAndHash(
	t *testing.T, priv ed25519.PrivateKey, data []byte,
) (sig []byte, hash string) {
	t.Helper()
	sig = signing.Sign(priv, data)
	h := sha256.Sum256(data)
	return sig, fmt.Sprintf("%x", h)
}

const pluginURL = "https://example.com/plugins/d2r/parser.wasm"

// --- Tests ---

func TestEnsurePlugin_DownloadVerifyCache(t *testing.T) {
	pub, priv := generateTestKeys(t)
	wasm := []byte("fake wasm binary")
	sig, hash := signAndHash(t, priv, wasm)

	reg := &fakeRegistry{
		manifest: map[string]PluginInfo{
			"d2r": {
				GameID:  "d2r",
				Version: "1.0.0",
				SHA256:  hash,
				URL:     pluginURL,
			},
		},
		files: map[string][]byte{
			pluginURL:          wasm,
			pluginURL + ".sig": sig,
		},
	}

	cacheDir := t.TempDir()
	cache := NewCache(cacheDir)
	loader := &fakeLoader{}

	mgr := NewManager(reg, cache, loader, pub, testLogger())
	if err := mgr.EnsurePlugin(context.Background(), "d2r"); err != nil {
		t.Fatalf("EnsurePlugin: %v", err)
	}

	// Loader should have loaded the plugin.
	loader.mu.Lock()
	loaded := append([]string{}, loader.loaded...)
	loader.mu.Unlock()
	if len(loaded) != 1 || loaded[0] != "d2r" {
		t.Errorf("loaded = %v, want [d2r]", loaded)
	}

	// Cache should have the plugin.
	if !cache.HasVersion("d2r", "1.0.0") {
		t.Error("cache does not have version 1.0.0")
	}
}

func TestEnsurePlugin_CacheHit(t *testing.T) {
	pub, priv := generateTestKeys(t)
	wasm := []byte("cached wasm")
	sig, _ := signAndHash(t, priv, wasm)

	cacheDir := t.TempDir()
	cache := NewCache(cacheDir)
	if err := cache.Write("d2r", "1.0.0", wasm, sig); err != nil {
		t.Fatalf("cache write: %v", err)
	}

	reg := &fakeRegistry{
		manifest: map[string]PluginInfo{
			"d2r": {
				GameID:  "d2r",
				Version: "1.0.0",
				SHA256:  "unused",
				URL:     pluginURL,
			},
		},
		// No files — download should not be called.
	}

	loader := &fakeLoader{}
	mgr := NewManager(reg, cache, loader, pub, testLogger())
	if err := mgr.EnsurePlugin(context.Background(), "d2r"); err != nil {
		t.Fatalf("EnsurePlugin: %v", err)
	}

	loader.mu.Lock()
	loaded := append([]string{}, loader.loaded...)
	loader.mu.Unlock()
	if len(loaded) != 1 || loaded[0] != "d2r" {
		t.Errorf("loaded = %v, want [d2r]", loaded)
	}
}

func TestEnsurePlugin_LocalOverride(t *testing.T) {
	localDir := t.TempDir()
	wasm := []byte("local wasm")
	wasmPath := filepath.Join(localDir, "d2r.wasm")
	if err := os.WriteFile(wasmPath, wasm, 0o600); err != nil {
		t.Fatalf("write local wasm: %v", err)
	}

	loader := &fakeLoader{}
	// No public key — skip verification.
	mgr := NewManager(
		nil, NewCache(t.TempDir()), loader, nil, testLogger(),
	)
	mgr.SetLocalDir(localDir)

	if err := mgr.EnsurePlugin(context.Background(), "d2r"); err != nil {
		t.Fatalf("EnsurePlugin: %v", err)
	}

	loader.mu.Lock()
	loaded := append([]string{}, loader.loaded...)
	loader.mu.Unlock()
	if len(loaded) != 1 || loaded[0] != "d2r" {
		t.Errorf("loaded = %v, want [d2r]", loaded)
	}
}

func TestEnsurePlugin_BadSignature(t *testing.T) {
	pub, _ := generateTestKeys(t)
	wasm := []byte("some wasm")
	badSig := make([]byte, ed25519.SignatureSize)

	hash := sha256.Sum256(wasm)
	reg := &fakeRegistry{
		manifest: map[string]PluginInfo{
			"d2r": {
				GameID:  "d2r",
				Version: "1.0.0",
				SHA256:  fmt.Sprintf("%x", hash),
				URL:     pluginURL,
			},
		},
		files: map[string][]byte{
			pluginURL:          wasm,
			pluginURL + ".sig": badSig,
		},
	}

	loader := &fakeLoader{}
	mgr := NewManager(
		reg, NewCache(t.TempDir()), loader, pub, testLogger(),
	)
	err := mgr.EnsurePlugin(context.Background(), "d2r")
	if err == nil {
		t.Fatal("expected error for bad signature")
	}

	loader.mu.Lock()
	loaded := append([]string{}, loader.loaded...)
	loader.mu.Unlock()
	if len(loaded) != 0 {
		t.Errorf(
			"should not have loaded plugin with bad signature, loaded = %v",
			loaded,
		)
	}
}

func TestEnsurePlugin_SHA256Mismatch(t *testing.T) {
	pub, priv := generateTestKeys(t)
	wasm := []byte("some wasm")
	sig := signing.Sign(priv, wasm)

	reg := &fakeRegistry{
		manifest: map[string]PluginInfo{
			"d2r": {
				GameID:  "d2r",
				Version: "1.0.0",
				SHA256:  "wrong_hash",
				URL:     pluginURL,
			},
		},
		files: map[string][]byte{
			pluginURL:          wasm,
			pluginURL + ".sig": sig,
		},
	}

	loader := &fakeLoader{}
	mgr := NewManager(
		reg, NewCache(t.TempDir()), loader, pub, testLogger(),
	)
	err := mgr.EnsurePlugin(context.Background(), "d2r")
	if err == nil {
		t.Fatal("expected error for sha256 mismatch")
	}
}

func TestEnsurePlugin_UnknownGame(t *testing.T) {
	reg := &fakeRegistry{
		manifest: map[string]PluginInfo{},
	}

	mgr := NewManager(
		reg, NewCache(t.TempDir()), &fakeLoader{}, nil, testLogger(),
	)
	err := mgr.EnsurePlugin(context.Background(), "unknown")
	if err == nil {
		t.Fatal("expected error for unknown game")
	}
}

func TestEnsurePlugin_ManifestFetchError(t *testing.T) {
	reg := &fakeRegistry{
		fetchErr: fmt.Errorf("connection refused"),
	}

	mgr := NewManager(
		reg, NewCache(t.TempDir()), &fakeLoader{}, nil, testLogger(),
	)
	err := mgr.EnsurePlugin(context.Background(), "d2r")
	if err == nil {
		t.Fatal("expected error when manifest fetch fails")
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("error = %v, want to contain 'connection refused'", err)
	}
}

func TestEnsurePlugin_DownloadError(t *testing.T) {
	reg := &fakeRegistry{
		manifest: map[string]PluginInfo{
			"d2r": {
				GameID:  "d2r",
				Version: "1.0.0",
				SHA256:  "abc",
				URL:     pluginURL,
			},
		},
		// No files — download will fail.
	}

	loader := &fakeLoader{}
	mgr := NewManager(
		reg, NewCache(t.TempDir()), loader, nil, testLogger(),
	)
	err := mgr.EnsurePlugin(context.Background(), "d2r")
	if err == nil {
		t.Fatal("expected error when download fails")
	}

	loader.mu.Lock()
	loaded := append([]string{}, loader.loaded...)
	loader.mu.Unlock()
	if len(loaded) != 0 {
		t.Errorf("should not have loaded plugin on download failure, loaded = %v", loaded)
	}
}

func TestEnsurePlugin_LoaderError(t *testing.T) {
	_, priv := generateTestKeys(t)
	wasm := []byte("loader fail wasm")
	sig, hash := signAndHash(t, priv, wasm)

	reg := &fakeRegistry{
		manifest: map[string]PluginInfo{
			"d2r": {
				GameID:  "d2r",
				Version: "1.0.0",
				SHA256:  hash,
				URL:     pluginURL,
			},
		},
		files: map[string][]byte{
			pluginURL:          wasm,
			pluginURL + ".sig": sig,
		},
	}

	loader := &fakeLoader{
		loadErr: map[string]error{"d2r": fmt.Errorf("compilation failed")},
	}
	// No public key — skip signature verification.
	mgr := NewManager(
		reg, NewCache(t.TempDir()), loader, nil, testLogger(),
	)
	err := mgr.EnsurePlugin(context.Background(), "d2r")
	if err == nil {
		t.Fatal("expected error when loader fails")
	}
	if !strings.Contains(err.Error(), "compilation failed") {
		t.Errorf("error = %v, want to contain 'compilation failed'", err)
	}
}

func TestEnsurePlugin_LocalOverrideWithSig(t *testing.T) {
	pub, priv := generateTestKeys(t)
	localDir := t.TempDir()
	wasm := []byte("local wasm with sig")
	sig := signing.Sign(priv, wasm)

	wasmPath := filepath.Join(localDir, "d2r.wasm")
	sigPath := filepath.Join(localDir, "d2r.wasm.sig")
	if err := os.WriteFile(wasmPath, wasm, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sigPath, sig, 0o600); err != nil {
		t.Fatal(err)
	}

	loader := &fakeLoader{}
	mgr := NewManager(
		nil, NewCache(t.TempDir()), loader, pub, testLogger(),
	)
	mgr.SetLocalDir(localDir)

	if err := mgr.EnsurePlugin(context.Background(), "d2r"); err != nil {
		t.Fatalf("EnsurePlugin: %v", err)
	}

	loader.mu.Lock()
	loaded := append([]string{}, loader.loaded...)
	loader.mu.Unlock()
	if len(loaded) != 1 || loaded[0] != "d2r" {
		t.Errorf("loaded = %v, want [d2r]", loaded)
	}
}

func TestEnsurePlugin_LocalOverrideBadSig(t *testing.T) {
	pub, _ := generateTestKeys(t)
	localDir := t.TempDir()
	wasm := []byte("local wasm bad sig")
	badSig := make([]byte, ed25519.SignatureSize)

	wasmPath := filepath.Join(localDir, "d2r.wasm")
	sigPath := filepath.Join(localDir, "d2r.wasm.sig")
	if err := os.WriteFile(wasmPath, wasm, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sigPath, badSig, 0o600); err != nil {
		t.Fatal(err)
	}

	loader := &fakeLoader{}
	mgr := NewManager(
		nil, NewCache(t.TempDir()), loader, pub, testLogger(),
	)
	mgr.SetLocalDir(localDir)

	err := mgr.EnsurePlugin(context.Background(), "d2r")
	if err == nil {
		t.Fatal("expected error for bad local sig")
	}

	loader.mu.Lock()
	loaded := append([]string{}, loader.loaded...)
	loader.mu.Unlock()
	if len(loaded) != 0 {
		t.Errorf("should not have loaded, loaded = %v", loaded)
	}
}

func TestCheckForUpdates_NoExistingCache(t *testing.T) {
	reg := &fakeRegistry{
		manifest: map[string]PluginInfo{
			"d2r": {
				GameID:  "d2r",
				Version: "1.0.0",
				SHA256:  "abc",
				URL:     pluginURL,
			},
		},
	}

	mgr := NewManager(
		reg, NewCache(t.TempDir()), &fakeLoader{}, nil, testLogger(),
	)
	updated, err := mgr.CheckForUpdates(context.Background())
	if err != nil {
		t.Fatalf("CheckForUpdates: %v", err)
	}
	if len(updated) != 0 {
		t.Errorf("updated = %v, want empty (no cached plugins)", updated)
	}
}

func TestCheckForUpdates_UpToDate(t *testing.T) {
	cacheDir := t.TempDir()
	cache := NewCache(cacheDir)
	if err := cache.Write("d2r", "1.0.0", []byte("w"), []byte("s")); err != nil {
		t.Fatal(err)
	}

	reg := &fakeRegistry{
		manifest: map[string]PluginInfo{
			"d2r": {
				GameID:  "d2r",
				Version: "1.0.0",
				SHA256:  "abc",
				URL:     pluginURL,
			},
		},
	}

	mgr := NewManager(
		reg, cache, &fakeLoader{}, nil, testLogger(),
	)
	updated, err := mgr.CheckForUpdates(context.Background())
	if err != nil {
		t.Fatalf("CheckForUpdates: %v", err)
	}
	if len(updated) != 0 {
		t.Errorf("updated = %v, want empty (already up to date)", updated)
	}
}

func TestCheckForUpdates_DownloadFailure(t *testing.T) {
	cacheDir := t.TempDir()
	cache := NewCache(cacheDir)
	if err := cache.Write("d2r", "1.0.0", []byte("w"), []byte("s")); err != nil {
		t.Fatal(err)
	}

	reg := &fakeRegistry{
		manifest: map[string]PluginInfo{
			"d2r": {
				GameID:  "d2r",
				Version: "2.0.0",
				SHA256:  "abc",
				URL:     pluginURL,
			},
		},
		// No files — download will fail.
	}

	mgr := NewManager(
		reg, cache, &fakeLoader{}, nil, testLogger(),
	)
	updated, err := mgr.CheckForUpdates(context.Background())
	if err != nil {
		t.Fatalf("CheckForUpdates: %v", err)
	}
	// Download fails, so d2r should not be in updated list.
	if len(updated) != 0 {
		t.Errorf("updated = %v, want empty (download failed)", updated)
	}
}

func TestCheckForUpdates_ManifestFetchError(t *testing.T) {
	reg := &fakeRegistry{
		fetchErr: fmt.Errorf("network error"),
	}

	mgr := NewManager(
		reg, NewCache(t.TempDir()), &fakeLoader{}, nil, testLogger(),
	)
	_, err := mgr.CheckForUpdates(context.Background())
	if err == nil {
		t.Fatal("expected error when manifest fetch fails")
	}
}

func TestCheckForUpdates_UpdateSigDownloadFailure(t *testing.T) {
	_, priv := generateTestKeys(t)
	wasm := []byte("old wasm")
	oldSig := signing.Sign(priv, wasm)

	cacheDir := t.TempDir()
	cache := NewCache(cacheDir)
	if err := cache.Write("d2r", "1.0.0", wasm, oldSig); err != nil {
		t.Fatal(err)
	}

	newWasm := []byte("new wasm v2")
	_, newHash := signAndHash(t, priv, newWasm)

	reg := &fakeRegistry{
		manifest: map[string]PluginInfo{
			"d2r": {
				GameID:  "d2r",
				Version: "2.0.0",
				SHA256:  newHash,
				URL:     pluginURL,
			},
		},
		files: map[string][]byte{
			// Wasm present, but sig missing — sig download fails.
			pluginURL: newWasm,
		},
	}

	mgr := NewManager(
		reg, cache, &fakeLoader{}, nil, testLogger(),
	)
	updated, err := mgr.CheckForUpdates(context.Background())
	if err != nil {
		t.Fatalf("CheckForUpdates: %v", err)
	}
	if len(updated) != 0 {
		t.Errorf("updated = %v, want empty (sig download failed)", updated)
	}
}

func TestCheckForUpdates_UpdateVerificationFailure(t *testing.T) {
	pub, priv := generateTestKeys(t)
	wasm := []byte("old wasm")
	oldSig := signing.Sign(priv, wasm)

	cacheDir := t.TempDir()
	cache := NewCache(cacheDir)
	if err := cache.Write("d2r", "1.0.0", wasm, oldSig); err != nil {
		t.Fatal(err)
	}

	newWasm := []byte("new wasm v2")
	badSig := make([]byte, ed25519.SignatureSize)
	_, newHash := signAndHash(t, priv, newWasm)

	reg := &fakeRegistry{
		manifest: map[string]PluginInfo{
			"d2r": {
				GameID:  "d2r",
				Version: "2.0.0",
				SHA256:  newHash,
				URL:     pluginURL,
			},
		},
		files: map[string][]byte{
			pluginURL:          newWasm,
			pluginURL + ".sig": badSig,
		},
	}

	mgr := NewManager(
		reg, cache, &fakeLoader{}, pub, testLogger(),
	)
	updated, err := mgr.CheckForUpdates(context.Background())
	if err != nil {
		t.Fatalf("CheckForUpdates: %v", err)
	}
	if len(updated) != 0 {
		t.Errorf("updated = %v, want empty (verification failed)", updated)
	}
}

func TestCheckForUpdates_LoaderFailure(t *testing.T) {
	pub, priv := generateTestKeys(t)
	wasm := []byte("old wasm")
	oldSig := signing.Sign(priv, wasm)

	cacheDir := t.TempDir()
	cache := NewCache(cacheDir)
	if err := cache.Write("d2r", "1.0.0", wasm, oldSig); err != nil {
		t.Fatal(err)
	}

	newWasm := []byte("new wasm v2")
	newSig, newHash := signAndHash(t, priv, newWasm)

	reg := &fakeRegistry{
		manifest: map[string]PluginInfo{
			"d2r": {
				GameID:  "d2r",
				Version: "2.0.0",
				SHA256:  newHash,
				URL:     pluginURL,
			},
		},
		files: map[string][]byte{
			pluginURL:          newWasm,
			pluginURL + ".sig": newSig,
		},
	}

	loader := &fakeLoader{
		loadErr: map[string]error{"d2r": fmt.Errorf("load failed")},
	}
	mgr := NewManager(reg, cache, loader, pub, testLogger())
	updated, err := mgr.CheckForUpdates(context.Background())
	if err != nil {
		t.Fatalf("CheckForUpdates: %v", err)
	}
	if len(updated) != 0 {
		t.Errorf("updated = %v, want empty (loader failed)", updated)
	}
}

func TestCheckForUpdates_StaleVersion(t *testing.T) {
	pub, priv := generateTestKeys(t)

	oldWasm := []byte("old wasm")
	oldSig := signing.Sign(priv, oldWasm)

	newWasm := []byte("new wasm v2")
	newSig, newHash := signAndHash(t, priv, newWasm)

	cacheDir := t.TempDir()
	cache := NewCache(cacheDir)
	if err := cache.Write("d2r", "1.0.0", oldWasm, oldSig); err != nil {
		t.Fatalf("cache write: %v", err)
	}

	reg := &fakeRegistry{
		manifest: map[string]PluginInfo{
			"d2r": {
				GameID:  "d2r",
				Version: "2.0.0",
				SHA256:  newHash,
				URL:     pluginURL,
			},
		},
		files: map[string][]byte{
			pluginURL:          newWasm,
			pluginURL + ".sig": newSig,
		},
	}

	loader := &fakeLoader{}
	mgr := NewManager(reg, cache, loader, pub, testLogger())
	updated, err := mgr.CheckForUpdates(context.Background())
	if err != nil {
		t.Fatalf("CheckForUpdates: %v", err)
	}
	if len(updated) != 1 || updated[0] != "d2r" {
		t.Errorf("updated = %v, want [d2r]", updated)
	}
	if !cache.HasVersion("d2r", "2.0.0") {
		t.Error("cache should have version 2.0.0 after update")
	}
}
