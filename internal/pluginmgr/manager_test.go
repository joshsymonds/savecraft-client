package pluginmgr

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

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

type countingRegistry struct {
	fakeRegistry
	fetchCount    int
	downloadCount int
}

func (r *countingRegistry) FetchManifest(
	ctx context.Context,
) (map[string]PluginInfo, error) {
	r.fetchCount++
	return r.fakeRegistry.FetchManifest(ctx)
}

func (r *countingRegistry) Download(
	ctx context.Context, url string,
) ([]byte, error) {
	r.downloadCount++
	return r.fakeRegistry.Download(ctx, url)
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
	gameDir := filepath.Join(localDir, "d2r")
	if err := os.MkdirAll(gameDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	wasm := []byte("local wasm")
	wasmPath := filepath.Join(gameDir, "parser.wasm")
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

func TestEnsurePlugin_LocalOverrideMultipleGames(t *testing.T) {
	localDir := t.TempDir()
	for _, gameID := range []string{"d2r", "rimworld"} {
		gameDir := filepath.Join(localDir, gameID)
		if err := os.MkdirAll(gameDir, 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", gameID, err)
		}
		wasm := []byte("local wasm " + gameID)
		if err := os.WriteFile(filepath.Join(gameDir, "parser.wasm"), wasm, 0o600); err != nil {
			t.Fatalf("write %s: %v", gameID, err)
		}
	}

	loader := &fakeLoader{}
	mgr := NewManager(nil, NewCache(t.TempDir()), loader, nil, testLogger())
	mgr.SetLocalDir(localDir)

	for _, gameID := range []string{"d2r", "rimworld"} {
		if err := mgr.EnsurePlugin(context.Background(), gameID); err != nil {
			t.Fatalf("EnsurePlugin(%s): %v", gameID, err)
		}
	}

	loader.mu.Lock()
	loaded := append([]string{}, loader.loaded...)
	loader.mu.Unlock()
	if len(loaded) != 2 {
		t.Fatalf("loaded = %v, want 2 plugins", loaded)
	}
	// Both should have been loaded from local dir.
	got := map[string]bool{}
	for _, g := range loaded {
		got[g] = true
	}
	if !got["d2r"] || !got["rimworld"] {
		t.Errorf("loaded = %v, want d2r and rimworld", loaded)
	}
}

func TestEnsurePlugin_LocalOverrideFallthrough(t *testing.T) {
	// localDir has d2r but not rimworld — rimworld should fall through to remote.
	localDir := t.TempDir()
	gameDir := filepath.Join(localDir, "d2r")
	if err := os.MkdirAll(gameDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(gameDir, "parser.wasm"), []byte("local d2r"), 0o600); err != nil {
		t.Fatal(err)
	}

	pub, priv := generateTestKeys(t)
	remoteWasm := []byte("remote rimworld")
	remoteSig := signing.Sign(priv, remoteWasm)
	hash := sha256.Sum256(remoteWasm)

	reg := &fakeRegistry{
		manifest: map[string]PluginInfo{
			"rimworld": {
				GameID:  "rimworld",
				Version: "1.0.0",
				SHA256:  fmt.Sprintf("%x", hash),
				URL:     pluginURL,
			},
		},
		files: map[string][]byte{
			pluginURL:          remoteWasm,
			pluginURL + ".sig": remoteSig,
		},
	}

	loader := &fakeLoader{}
	mgr := NewManager(reg, NewCache(t.TempDir()), loader, pub, testLogger())
	mgr.SetLocalDir(localDir)

	// d2r loads from local (no sig, no public key needed for local without .sig file).
	mgrNoVerify := NewManager(reg, NewCache(t.TempDir()), loader, nil, testLogger())
	mgrNoVerify.SetLocalDir(localDir)
	if err := mgrNoVerify.EnsurePlugin(context.Background(), "d2r"); err != nil {
		t.Fatalf("EnsurePlugin(d2r): %v", err)
	}

	// rimworld loads from remote.
	if err := mgr.EnsurePlugin(context.Background(), "rimworld"); err != nil {
		t.Fatalf("EnsurePlugin(rimworld): %v", err)
	}

	loader.mu.Lock()
	loaded := append([]string{}, loader.loaded...)
	loader.mu.Unlock()
	got := map[string]bool{}
	for _, g := range loaded {
		got[g] = true
	}
	if !got["d2r"] || !got["rimworld"] {
		t.Errorf("loaded = %v, want both d2r and rimworld", loaded)
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

// evolvingRegistry returns an empty manifest on the first call, then includes
// the game on subsequent calls — simulating a plugin deployed after daemon start.
type evolvingRegistry struct {
	calls    int
	manifest map[string]PluginInfo
	files    map[string][]byte
	mu       sync.Mutex
}

func (r *evolvingRegistry) FetchManifest(_ context.Context) (map[string]PluginInfo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	if r.calls == 1 {
		return map[string]PluginInfo{}, nil
	}
	return r.manifest, nil
}

func (r *evolvingRegistry) Download(_ context.Context, url string) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	data, ok := r.files[url]
	if !ok {
		return nil, fmt.Errorf("not found: %s", url)
	}
	return data, nil
}

func TestEnsurePlugin_RefetchesManifestForUnknownGame(t *testing.T) {
	_, priv := generateTestKeys(t)
	wasm := []byte("clair obscur wasm")
	sig, hash := signAndHash(t, priv, wasm)

	reg := &evolvingRegistry{
		manifest: map[string]PluginInfo{
			"clair-obscur": {
				GameID:  "clair-obscur",
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

	loader := &fakeLoader{}
	mgr := NewManager(reg, NewCache(t.TempDir()), loader, nil, testLogger())

	// First call: manifest is empty, but re-fetch finds the game.
	err := mgr.EnsurePlugin(context.Background(), "clair-obscur")
	if err != nil {
		t.Fatalf("expected success after manifest re-fetch, got: %v", err)
	}

	reg.mu.Lock()
	calls := reg.calls
	reg.mu.Unlock()
	if calls != 2 {
		t.Errorf("expected 2 manifest fetches (initial + re-fetch), got %d", calls)
	}

	loader.mu.Lock()
	loaded := append([]string{}, loader.loaded...)
	loader.mu.Unlock()
	if len(loaded) != 1 || loaded[0] != "clair-obscur" {
		t.Errorf("expected [clair-obscur] loaded, got %v", loaded)
	}
}

func TestEnsurePlugin_RefetchStillFailsForUnknownGame(t *testing.T) {
	// evolvingRegistry always returns empty — game is permanently unknown.
	reg := &evolvingRegistry{
		manifest: map[string]PluginInfo{},
		files:    map[string][]byte{},
	}

	loader := &fakeLoader{}
	mgr := NewManager(reg, NewCache(t.TempDir()), loader, nil, testLogger())

	err := mgr.EnsurePlugin(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for permanently unknown game")
	}
	if !strings.Contains(err.Error(), "unknown plugin") {
		t.Errorf("error = %v, want to contain 'unknown plugin'", err)
	}

	reg.mu.Lock()
	calls := reg.calls
	reg.mu.Unlock()
	// Initial fetch (empty) + re-fetch (still empty) = 2 calls.
	if calls != 2 {
		t.Errorf("expected 2 manifest fetches, got %d", calls)
	}
}

func TestEnsurePlugin_LocalOverrideWithSig(t *testing.T) {
	pub, priv := generateTestKeys(t)
	localDir := t.TempDir()
	gameDir := filepath.Join(localDir, "d2r")
	if err := os.MkdirAll(gameDir, 0o700); err != nil {
		t.Fatal(err)
	}
	wasm := []byte("local wasm with sig")
	sig := signing.Sign(priv, wasm)

	wasmPath := filepath.Join(gameDir, "parser.wasm")
	sigPath := filepath.Join(gameDir, "parser.wasm.sig")
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
	gameDir := filepath.Join(localDir, "d2r")
	if err := os.MkdirAll(gameDir, 0o700); err != nil {
		t.Fatal(err)
	}
	wasm := []byte("local wasm bad sig")
	badSig := make([]byte, ed25519.SignatureSize)

	wasmPath := filepath.Join(gameDir, "parser.wasm")
	sigPath := filepath.Join(gameDir, "parser.wasm.sig")
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

func TestManifests_FetchesAndCaches(t *testing.T) {
	manifest := map[string]PluginInfo{
		"d2r": {
			GameID:  "d2r",
			Name:    "Diablo II: Resurrected",
			Version: "1.0.0",
			SHA256:  "abc",
			URL:     pluginURL,
			DefaultPaths: map[string]string{
				"linux": "~/Games/d2r",
			},
			FileExtensions: []string{".d2s"},
		},
	}

	reg := &countingRegistry{
		fakeRegistry: fakeRegistry{manifest: manifest},
	}

	mgr := NewManager(
		reg, NewCache(t.TempDir()), &fakeLoader{}, nil, testLogger(),
	)

	// First call should fetch.
	got, err := mgr.Manifests(context.Background())
	if err != nil {
		t.Fatalf("Manifests: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d plugins, want 1", len(got))
	}
	if got["d2r"].Name != "Diablo II: Resurrected" {
		t.Errorf("name = %q", got["d2r"].Name)
	}
	if reg.fetchCount != 1 {
		t.Errorf("fetchCount = %d after first call, want 1", reg.fetchCount)
	}

	// Second call should return cached (no additional fetch).
	got2, err := mgr.Manifests(context.Background())
	if err != nil {
		t.Fatalf("Manifests (cached): %v", err)
	}
	if len(got2) != 1 {
		t.Fatalf("cached got %d plugins, want 1", len(got2))
	}
	if reg.fetchCount != 1 {
		t.Errorf("fetchCount = %d after second call, want 1 (should be cached)", reg.fetchCount)
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

func TestEnsurePlugin_SHA256MatchSkipsDownload(t *testing.T) {
	pub, priv := generateTestKeys(t)
	wasm := []byte("same wasm binary")
	sig, hash := signAndHash(t, priv, wasm)

	cacheDir := t.TempDir()
	cache := NewCache(cacheDir)
	// Cache with old version but same wasm content.
	if err := cache.Write("d2r", "1.0.0", wasm, sig); err != nil {
		t.Fatalf("cache write: %v", err)
	}

	reg := &countingRegistry{
		fakeRegistry: fakeRegistry{
			manifest: map[string]PluginInfo{
				"d2r": {
					GameID:  "d2r",
					Version: "2.0.0",
					SHA256:  hash, // Same hash as cached wasm.
					URL:     pluginURL,
				},
			},
			// No files — download should not be called.
		},
	}

	loader := &fakeLoader{}
	mgr := NewManager(reg, cache, loader, pub, testLogger())
	if err := mgr.EnsurePlugin(context.Background(), "d2r"); err != nil {
		t.Fatalf("EnsurePlugin: %v", err)
	}

	// Should not have downloaded anything.
	if reg.downloadCount != 0 {
		t.Errorf("downloadCount = %d, want 0 (SHA256 matched)", reg.downloadCount)
	}

	// Plugin should have been loaded.
	loader.mu.Lock()
	loaded := append([]string{}, loader.loaded...)
	loader.mu.Unlock()
	if len(loaded) != 1 || loaded[0] != "d2r" {
		t.Errorf("loaded = %v, want [d2r]", loaded)
	}

	// Cache version should be updated.
	if !cache.HasVersion("d2r", "2.0.0") {
		t.Error("cache should have version 2.0.0 after SHA256 match")
	}
}

func TestEnsurePlugin_SHA256MismatchDownloads(t *testing.T) {
	pub, priv := generateTestKeys(t)
	oldWasm := []byte("old wasm binary")
	oldSig := signing.Sign(priv, oldWasm)

	newWasm := []byte("new wasm binary")
	newSig, newHash := signAndHash(t, priv, newWasm)

	cacheDir := t.TempDir()
	cache := NewCache(cacheDir)
	if err := cache.Write("d2r", "1.0.0", oldWasm, oldSig); err != nil {
		t.Fatalf("cache write: %v", err)
	}

	reg := &countingRegistry{
		fakeRegistry: fakeRegistry{
			manifest: map[string]PluginInfo{
				"d2r": {
					GameID:  "d2r",
					Version: "2.0.0",
					SHA256:  newHash, // Different hash.
					URL:     pluginURL,
				},
			},
			files: map[string][]byte{
				pluginURL:          newWasm,
				pluginURL + ".sig": newSig,
			},
		},
	}

	loader := &fakeLoader{}
	mgr := NewManager(reg, cache, loader, pub, testLogger())
	if err := mgr.EnsurePlugin(context.Background(), "d2r"); err != nil {
		t.Fatalf("EnsurePlugin: %v", err)
	}

	// Should have downloaded (2 calls: wasm + sig).
	if reg.downloadCount != 2 {
		t.Errorf("downloadCount = %d, want 2 (SHA256 mismatch)", reg.downloadCount)
	}

	// Plugin should have been loaded.
	loader.mu.Lock()
	loaded := append([]string{}, loader.loaded...)
	loader.mu.Unlock()
	if len(loaded) != 1 || loaded[0] != "d2r" {
		t.Errorf("loaded = %v, want [d2r]", loaded)
	}

	// Cache should have new version.
	if !cache.HasVersion("d2r", "2.0.0") {
		t.Error("cache should have version 2.0.0 after download")
	}
}

func TestCheckForUpdates_SHA256MatchSkipsDownload(t *testing.T) {
	pub, priv := generateTestKeys(t)
	wasm := []byte("same wasm binary")
	sig := signing.Sign(priv, wasm)
	h := sha256.Sum256(wasm)
	hash := fmt.Sprintf("%x", h)

	cacheDir := t.TempDir()
	cache := NewCache(cacheDir)
	if err := cache.Write("d2r", "1.0.0", wasm, sig); err != nil {
		t.Fatalf("cache write: %v", err)
	}

	reg := &countingRegistry{
		fakeRegistry: fakeRegistry{
			manifest: map[string]PluginInfo{
				"d2r": {
					GameID:  "d2r",
					Version: "2.0.0",
					SHA256:  hash, // Same hash.
					URL:     pluginURL,
				},
			},
			// No files — download should not be called.
		},
	}

	loader := &fakeLoader{}
	mgr := NewManager(reg, cache, loader, pub, testLogger())
	updated, err := mgr.CheckForUpdates(context.Background())
	if err != nil {
		t.Fatalf("CheckForUpdates: %v", err)
	}

	// Should not have downloaded anything.
	if reg.downloadCount != 0 {
		t.Errorf("downloadCount = %d, want 0 (SHA256 matched)", reg.downloadCount)
	}

	// Should not be in updated list (no actual binary change).
	if len(updated) != 0 {
		t.Errorf("updated = %v, want empty (binary unchanged)", updated)
	}

	// Version should be updated in cache.
	if !cache.HasVersion("d2r", "2.0.0") {
		t.Error("cache should have version 2.0.0 after SHA256 match")
	}
}

func TestCheckForUpdates_SHA256MismatchDownloads(t *testing.T) {
	pub, priv := generateTestKeys(t)
	oldWasm := []byte("old wasm binary")
	oldSig := signing.Sign(priv, oldWasm)

	newWasm := []byte("new wasm binary")
	newSig, newHash := signAndHash(t, priv, newWasm)

	cacheDir := t.TempDir()
	cache := NewCache(cacheDir)
	if err := cache.Write("d2r", "1.0.0", oldWasm, oldSig); err != nil {
		t.Fatalf("cache write: %v", err)
	}

	reg := &countingRegistry{
		fakeRegistry: fakeRegistry{
			manifest: map[string]PluginInfo{
				"d2r": {
					GameID:  "d2r",
					Version: "2.0.0",
					SHA256:  newHash, // Different hash.
					URL:     pluginURL,
				},
			},
			files: map[string][]byte{
				pluginURL:          newWasm,
				pluginURL + ".sig": newSig,
			},
		},
	}

	loader := &fakeLoader{}
	mgr := NewManager(reg, cache, loader, pub, testLogger())
	updated, err := mgr.CheckForUpdates(context.Background())
	if err != nil {
		t.Fatalf("CheckForUpdates: %v", err)
	}

	// Should have downloaded.
	if reg.downloadCount != 2 {
		t.Errorf("downloadCount = %d, want 2 (SHA256 mismatch)", reg.downloadCount)
	}

	// Should be in updated list.
	if len(updated) != 1 || updated[0] != "d2r" {
		t.Errorf("updated = %v, want [d2r]", updated)
	}

	// Cache should have new version.
	if !cache.HasVersion("d2r", "2.0.0") {
		t.Error("cache should have version 2.0.0 after download")
	}
}

func TestCheckForUpdates_LocalPluginChanged(t *testing.T) {
	localDir := t.TempDir()
	gameDir := filepath.Join(localDir, "d2r")
	if err := os.MkdirAll(gameDir, 0o700); err != nil {
		t.Fatal(err)
	}
	wasmV1 := []byte("local wasm v1")
	if err := os.WriteFile(filepath.Join(gameDir, "parser.wasm"), wasmV1, 0o600); err != nil {
		t.Fatal(err)
	}

	loader := &fakeLoader{}
	mgr := NewManager(nil, NewCache(t.TempDir()), loader, nil, testLogger())
	mgr.SetLocalDir(localDir)

	// Initial load via EnsurePlugin.
	if err := mgr.EnsurePlugin(context.Background(), "d2r"); err != nil {
		t.Fatalf("EnsurePlugin: %v", err)
	}

	// Change the WASM on disk.
	wasmV2 := []byte("local wasm v2 changed")
	if err := os.WriteFile(filepath.Join(gameDir, "parser.wasm"), wasmV2, 0o600); err != nil {
		t.Fatal(err)
	}

	// CheckForUpdates should detect the change and reload.
	updated, err := mgr.CheckForUpdates(context.Background())
	if err != nil {
		t.Fatalf("CheckForUpdates: %v", err)
	}

	if len(updated) != 1 || updated[0] != "d2r" {
		t.Errorf("updated = %v, want [d2r]", updated)
	}

	// Should have loaded twice total: once from EnsurePlugin, once from CheckForUpdates.
	loader.mu.Lock()
	loaded := append([]string{}, loader.loaded...)
	loader.mu.Unlock()
	d2rLoads := 0
	for _, g := range loaded {
		if g == "d2r" {
			d2rLoads++
		}
	}
	if d2rLoads != 2 {
		t.Errorf("d2r load count = %d, want 2", d2rLoads)
	}
}

func TestCheckForUpdates_LocalPluginUnchanged(t *testing.T) {
	localDir := t.TempDir()
	gameDir := filepath.Join(localDir, "d2r")
	if err := os.MkdirAll(gameDir, 0o700); err != nil {
		t.Fatal(err)
	}
	wasm := []byte("local wasm unchanged")
	if err := os.WriteFile(filepath.Join(gameDir, "parser.wasm"), wasm, 0o600); err != nil {
		t.Fatal(err)
	}

	loader := &fakeLoader{}
	mgr := NewManager(nil, NewCache(t.TempDir()), loader, nil, testLogger())
	mgr.SetLocalDir(localDir)

	// Initial load.
	if err := mgr.EnsurePlugin(context.Background(), "d2r"); err != nil {
		t.Fatalf("EnsurePlugin: %v", err)
	}

	// CheckForUpdates with unchanged file should NOT reload.
	updated, err := mgr.CheckForUpdates(context.Background())
	if err != nil {
		t.Fatalf("CheckForUpdates: %v", err)
	}

	if len(updated) != 0 {
		t.Errorf("updated = %v, want empty (unchanged)", updated)
	}

	// Only one load total (the initial EnsurePlugin).
	loader.mu.Lock()
	loaded := append([]string{}, loader.loaded...)
	loader.mu.Unlock()
	d2rLoads := 0
	for _, g := range loaded {
		if g == "d2r" {
			d2rLoads++
		}
	}
	if d2rLoads != 1 {
		t.Errorf("d2r load count = %d, want 1 (no reload for unchanged)", d2rLoads)
	}
}

func TestCheckForUpdates_LocalAndRemoteMixed(t *testing.T) {
	// d2r is local, rimworld is remote. Both should be checked.
	localDir := t.TempDir()
	gameDir := filepath.Join(localDir, "d2r")
	if err := os.MkdirAll(gameDir, 0o700); err != nil {
		t.Fatal(err)
	}
	localWasmV1 := []byte("local d2r v1")
	if err := os.WriteFile(filepath.Join(gameDir, "parser.wasm"), localWasmV1, 0o600); err != nil {
		t.Fatal(err)
	}

	pub, priv := generateTestKeys(t)
	remoteWasm := []byte("remote rimworld")
	remoteSig := signing.Sign(priv, remoteWasm)
	hash := sha256.Sum256(remoteWasm)
	remoteWasm2 := []byte("remote rimworld v2")
	remoteSig2 := signing.Sign(priv, remoteWasm2)
	hash2 := sha256.Sum256(remoteWasm2)

	reg := &fakeRegistry{
		manifest: map[string]PluginInfo{
			"rimworld": {
				GameID:  "rimworld",
				Version: "1.0.0",
				SHA256:  fmt.Sprintf("%x", hash),
				URL:     pluginURL,
			},
		},
		files: map[string][]byte{
			pluginURL:          remoteWasm,
			pluginURL + ".sig": remoteSig,
		},
	}

	loader := &fakeLoader{}
	cache := NewCache(t.TempDir())

	// Initial load: d2r from local (no pub key needed), rimworld from remote.
	mgr := NewManager(reg, cache, loader, pub, testLogger())
	mgr.SetLocalDir(localDir)

	// EnsurePlugin for d2r — but we need no sig verification for local.
	// Use a separate manager with no public key for the local-only load.
	mgrNoKey := NewManager(reg, cache, loader, nil, testLogger())
	mgrNoKey.SetLocalDir(localDir)
	if err := mgrNoKey.EnsurePlugin(context.Background(), "d2r"); err != nil {
		t.Fatalf("EnsurePlugin(d2r): %v", err)
	}
	if err := mgr.EnsurePlugin(context.Background(), "rimworld"); err != nil {
		t.Fatalf("EnsurePlugin(rimworld): %v", err)
	}

	// Change local d2r, update remote rimworld manifest.
	localWasmV2 := []byte("local d2r v2")
	if err := os.WriteFile(filepath.Join(gameDir, "parser.wasm"), localWasmV2, 0o600); err != nil {
		t.Fatal(err)
	}
	reg.manifest["rimworld"] = PluginInfo{
		GameID:  "rimworld",
		Version: "2.0.0",
		SHA256:  fmt.Sprintf("%x", hash2),
		URL:     pluginURL,
	}
	reg.files[pluginURL] = remoteWasm2
	reg.files[pluginURL+".sig"] = remoteSig2

	// CheckForUpdates should reload d2r (local changed) and rimworld (remote updated).
	// Use mgrNoKey so local d2r doesn't need sig verification.
	updated, err := mgrNoKey.CheckForUpdates(context.Background())
	if err != nil {
		t.Fatalf("CheckForUpdates: %v", err)
	}

	got := map[string]bool{}
	for _, g := range updated {
		got[g] = true
	}
	if !got["d2r"] {
		t.Error("d2r should be in updated list (local changed)")
	}
	if !got["rimworld"] {
		t.Error("rimworld should be in updated list (remote updated)")
	}
}
