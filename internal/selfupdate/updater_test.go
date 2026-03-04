package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/joshsymonds/savecraft.gg/internal/daemon"
	"github.com/joshsymonds/savecraft.gg/internal/signing"
)

func TestNew_ClientHasTimeout(t *testing.T) {
	u := New("http://example.com", nil, t.TempDir())
	if u.client.Timeout == 0 {
		t.Error("expected non-zero timeout on default client")
	}
	if u.client.Timeout != 120*time.Second {
		t.Errorf("timeout = %v, want 120s", u.client.Timeout)
	}
}

func TestNew_WithHTTPClient(t *testing.T) {
	custom := &http.Client{Timeout: 5 * time.Second}
	u := New("http://example.com", nil, t.TempDir(), WithHTTPClient(custom))
	if u.client != custom {
		t.Error("WithHTTPClient option not applied")
	}
}

func TestCheck_NewerVersionAvailable(t *testing.T) {
	manifest := manifestResponse{
		Version: "0.2.0",
		Platforms: map[string]daemon.UpdateInfo{
			"linux-amd64": {
				URL:          "https://example.com/daemon-linux-amd64",
				SignatureURL: "https://example.com/daemon-linux-amd64.sig",
				SHA256:       "abc123",
			},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/daemon/manifest.json" {
			http.NotFound(w, r)
			return
		}
		// Verify no Authorization header is sent (install is unauthenticated)
		if r.Header.Get("Authorization") != "" {
			t.Errorf("unexpected Authorization header: %s", r.Header.Get("Authorization"))
		}
		json.NewEncoder(w).Encode(manifest)
	}))
	defer srv.Close()

	u := New(srv.URL, nil, t.TempDir())

	info, err := u.Check(context.Background(), "0.1.0", "linux-amd64")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if info == nil {
		t.Fatal("expected non-nil UpdateInfo")
	}
	if info.Version != "0.2.0" {
		t.Errorf("version = %s, want 0.2.0", info.Version)
	}
	if info.URL != "https://example.com/daemon-linux-amd64" {
		t.Errorf("url = %s", info.URL)
	}
	if info.SHA256 != "abc123" {
		t.Errorf("sha256 = %s", info.SHA256)
	}
}

func TestCheck_AlreadyCurrent(t *testing.T) {
	manifest := manifestResponse{
		Version: "0.1.0",
		Platforms: map[string]daemon.UpdateInfo{
			"linux-amd64": {
				URL:    "https://example.com/daemon",
				SHA256: "abc",
			},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(manifest)
	}))
	defer srv.Close()

	u := New(srv.URL, nil, t.TempDir())

	info, err := u.Check(context.Background(), "0.1.0", "linux-amd64")
	if !errors.Is(err, ErrUpToDate) {
		t.Fatalf("Check: got err=%v, want ErrUpToDate", err)
	}
	if info != nil {
		t.Errorf("expected nil for current version, got %+v", info)
	}
}

func TestCheck_PlatformNotFound(t *testing.T) {
	manifest := manifestResponse{
		Version: "0.2.0",
		Platforms: map[string]daemon.UpdateInfo{
			"darwin-arm64": {
				URL:    "https://example.com/daemon",
				SHA256: "abc",
			},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(manifest)
	}))
	defer srv.Close()

	u := New(srv.URL, nil, t.TempDir())

	info, err := u.Check(context.Background(), "0.1.0", "linux-amd64")
	if !errors.Is(err, ErrNoPlatform) {
		t.Fatalf("Check: got err=%v, want ErrNoPlatform", err)
	}
	if info != nil {
		t.Errorf("expected nil for missing platform, got %+v", info)
	}
}

func TestApply_DownloadsAndReplaces(t *testing.T) {
	pubKey, privKey, err := signing.GenerateKeypair()
	if err != nil {
		t.Fatalf("generate keypair: %v", err)
	}

	binaryData := []byte("#!/bin/sh\necho new-daemon-v0.2.0")
	sig := signing.Sign(privKey, binaryData)
	hash := sha256.Sum256(binaryData)
	hashHex := hex.EncodeToString(hash[:])

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify no Authorization header on downloads
		if r.Header.Get("Authorization") != "" {
			t.Errorf("unexpected Authorization header on download: %s", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/binary":
			w.Write(binaryData)
		case "/binary.sig":
			w.Write(sig)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cacheDir := t.TempDir()
	targetDir := t.TempDir()
	binaryPath := filepath.Join(targetDir, "savecraft-daemon")

	// Write an old binary to verify replacement.
	if err := os.WriteFile(binaryPath, []byte("old-binary"), 0o755); err != nil {
		t.Fatalf("write old binary: %v", err)
	}

	u := New(srv.URL, pubKey, cacheDir)

	info := &daemon.UpdateInfo{
		Version:      "0.2.0",
		URL:          srv.URL + "/binary",
		SignatureURL: srv.URL + "/binary.sig",
		SHA256:       hashHex,
	}

	if err := u.Apply(context.Background(), info, binaryPath); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	got, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatalf("read replaced binary: %v", err)
	}
	if string(got) != string(binaryData) {
		t.Errorf("binary contents = %q, want %q", got, binaryData)
	}

	if runtime.GOOS != "windows" {
		stat, err := os.Stat(binaryPath)
		if err != nil {
			t.Fatalf("stat binary: %v", err)
		}
		if stat.Mode().Perm() != 0o700 {
			t.Errorf("binary mode = %v, want 0700", stat.Mode().Perm())
		}
	}
}

func TestApply_SHA256Mismatch(t *testing.T) {
	binaryData := []byte("daemon-binary")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/binary":
			w.Write(binaryData)
		case "/binary.sig":
			w.Write([]byte("not-used"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cacheDir := t.TempDir()
	targetDir := t.TempDir()
	binaryPath := filepath.Join(targetDir, "savecraft-daemon")

	// nil pubKey skips signature verification, so SHA256 check runs.
	u := New(srv.URL, nil, cacheDir)

	info := &daemon.UpdateInfo{
		Version:      "0.2.0",
		URL:          srv.URL + "/binary",
		SignatureURL: srv.URL + "/binary.sig",
		SHA256:       "0000000000000000000000000000000000000000000000000000000000000000",
	}

	err := u.Apply(context.Background(), info, binaryPath)
	if err == nil {
		t.Fatal("expected error for SHA256 mismatch")
	}

	expectedPrefix := "sha256 mismatch"
	if len(err.Error()) < len(expectedPrefix) || err.Error()[:len(expectedPrefix)] != expectedPrefix {
		t.Errorf("error = %q, want prefix %q", err.Error(), expectedPrefix)
	}

	// Binary should NOT exist at target (it should not have been renamed).
	if _, statErr := os.Stat(binaryPath); statErr == nil {
		t.Error("binary should not exist after SHA256 mismatch")
	}
}

func TestApply_BadSignature(t *testing.T) {
	pubKey, _, err := signing.GenerateKeypair()
	if err != nil {
		t.Fatalf("generate keypair: %v", err)
	}

	binaryData := []byte("daemon-binary")
	hash := sha256.Sum256(binaryData)
	hashHex := hex.EncodeToString(hash[:])

	// Generate a different keypair to produce wrong signature.
	_, wrongPrivKey, err := signing.GenerateKeypair()
	if err != nil {
		t.Fatalf("generate wrong keypair: %v", err)
	}
	wrongSig := signing.Sign(wrongPrivKey, binaryData)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/binary":
			w.Write(binaryData)
		case "/binary.sig":
			w.Write(wrongSig)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cacheDir := t.TempDir()
	targetDir := t.TempDir()
	binaryPath := filepath.Join(targetDir, "savecraft-daemon")

	u := New(srv.URL, pubKey, cacheDir)

	info := &daemon.UpdateInfo{
		Version:      "0.2.0",
		URL:          srv.URL + "/binary",
		SignatureURL: srv.URL + "/binary.sig",
		SHA256:       hashHex,
	}

	applyErr := u.Apply(context.Background(), info, binaryPath)
	if applyErr == nil {
		t.Fatal("expected error for bad signature")
	}

	expected := "signature verification: signature verification failed"
	if applyErr.Error() != expected {
		t.Errorf("error = %q, want %q", applyErr.Error(), expected)
	}
}

func TestIsNewer(t *testing.T) {
	tests := []struct {
		latest, current string
		want            bool
	}{
		{"0.2.0", "0.1.0", true},
		{"0.10.0", "0.9.0", true},
		{"1.0.0", "0.99.99", true},
		{"0.1.0", "0.1.0", false},
		{"0.1.0", "0.2.0", false},
		{"0.9.0", "0.10.0", false},
		{"1.0", "1.0.0", false},
		{"1.0.1", "1.0", true},
		// Dev versions use 0.0.0-dev.N.SHA format; numeric comparison
		// treats the "-dev" segment as 0, so any release > 0.0.0 wins.
		{"0.1.0", "0.0.0", true},
		{"0.0.1", "0.0.0", true},
	}
	for _, tt := range tests {
		got := isNewer(tt.latest, tt.current)
		if got != tt.want {
			t.Errorf("isNewer(%q, %q) = %v, want %v", tt.latest, tt.current, got, tt.want)
		}
	}
}

func TestReplaceBinary_ReplacesContent(t *testing.T) {
	targetDir := t.TempDir()
	binaryPath := filepath.Join(targetDir, "savecraftd.exe")

	// Write "old" binary.
	if err := os.WriteFile(binaryPath, []byte("old-daemon"), 0o755); err != nil {
		t.Fatalf("write old: %v", err)
	}

	// Write "new" binary to a temp location.
	newBinary := filepath.Join(t.TempDir(), "new-daemon.tmp")
	if err := os.WriteFile(newBinary, []byte("new-daemon-v2"), 0o644); err != nil {
		t.Fatalf("write new: %v", err)
	}

	if err := replaceBinary(newBinary, binaryPath); err != nil {
		t.Fatalf("replaceBinary: %v", err)
	}

	got, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatalf("read replaced: %v", err)
	}
	if string(got) != "new-daemon-v2" {
		t.Errorf("content = %q, want %q", got, "new-daemon-v2")
	}
}

func TestReplaceBinary_WorksWhenTargetDoesNotExist(t *testing.T) {
	targetDir := t.TempDir()
	binaryPath := filepath.Join(targetDir, "savecraftd.exe")

	newBinary := filepath.Join(t.TempDir(), "new-daemon.tmp")
	if err := os.WriteFile(newBinary, []byte("fresh-install"), 0o644); err != nil {
		t.Fatalf("write new: %v", err)
	}

	if err := replaceBinary(newBinary, binaryPath); err != nil {
		t.Fatalf("replaceBinary: %v", err)
	}

	got, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "fresh-install" {
		t.Errorf("content = %q, want %q", got, "fresh-install")
	}
}

func TestDownloadToFile_WritesContent(t *testing.T) {
	data := []byte("hello-daemon-binary")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(data)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "download.tmp")
	if err := downloadToFile(context.Background(), srv.URL, dest, srv.Client()); err != nil {
		t.Fatalf("downloadToFile: %v", err)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("content = %q, want %q", got, data)
	}
}

func TestDownloadToFile_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "download.tmp")
	err := downloadToFile(context.Background(), srv.URL, dest, srv.Client())
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	if !os.IsNotExist(func() error { _, e := os.Stat(dest); return e }()) {
		t.Error("file should not exist after failed download")
	}
}

func TestDownloadToFile_BadDestPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("data"))
	}))
	defer srv.Close()

	err := downloadToFile(context.Background(), srv.URL, "/nonexistent-dir/file.tmp", srv.Client())
	if err == nil {
		t.Fatal("expected error for bad dest path")
	}
}

func TestDownloadToFile_CancelledContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("data"))
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	dest := filepath.Join(t.TempDir(), "download.tmp")
	err := downloadToFile(ctx, srv.URL, dest, srv.Client())
	if err == nil {
		t.Fatal("expected error for canceled context")
	}
}

func TestCleanupTempFiles_NoErrorOnMissing(t *testing.T) {
	// Should not panic or error on non-existent files.
	cleanupTempFiles(
		filepath.Join(t.TempDir(), "nonexistent-1"),
		filepath.Join(t.TempDir(), "nonexistent-2"),
	)
}

func TestCleanupTempFiles_RemovesExisting(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "temp-file")
	if err := os.WriteFile(tmp, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	cleanupTempFiles(tmp)
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Error("file should have been removed")
	}
}

func TestCheck_DowngradeNotReturned(t *testing.T) {
	manifest := manifestResponse{
		Version: "0.1.0",
		Platforms: map[string]daemon.UpdateInfo{
			"linux-amd64": {
				URL:    "https://example.com/daemon",
				SHA256: "abc",
			},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(manifest)
	}))
	defer srv.Close()

	u := New(srv.URL, nil, t.TempDir())

	// Current version is NEWER than manifest — should not return an update.
	info, err := u.Check(context.Background(), "0.2.0", "linux-amd64")
	if !errors.Is(err, ErrUpToDate) {
		t.Fatalf("Check: got err=%v, want ErrUpToDate", err)
	}
	if info != nil {
		t.Errorf("expected nil for downgrade, got %+v", info)
	}
}
