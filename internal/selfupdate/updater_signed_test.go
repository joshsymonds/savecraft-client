package selfupdate

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/joshsymonds/savecraft.gg/internal/daemon"
	"github.com/joshsymonds/savecraft.gg/internal/signing"
)

// signedManifestServer serves /daemon/manifest.json and a detached
// /daemon/manifest.json.sig over TLS. The signature is over the exact bytes
// served. hitCount records download attempts to other paths so tests can prove
// rejection happens before any download.
func signedManifestServer(
	t *testing.T, priv ed25519.PrivateKey, m manifestResponse,
) (*httptest.Server, *int) {
	t.Helper()
	body, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	sig := signing.Sign(priv, body)
	binHits := 0
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/daemon/manifest.json":
			_, _ = w.Write(body)
		case "/daemon/manifest.json.sig":
			_, _ = w.Write(sig)
		default:
			binHits++
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &binHits
}

func newTestUpdater(t *testing.T, srv *httptest.Server, pub ed25519.PublicKey) *HTTPUpdater {
	t.Helper()
	u := New(srv.URL, pub, t.TempDir(), WithHTTPClient(srv.Client()))
	u.manifestPubKey = pub
	return u
}

func TestCheck_ValidSignedManifest(t *testing.T) {
	pub, priv, _ := signing.GenerateKeypair()
	m := manifestResponse{
		Version: "0.2.0",
		Platforms: map[string]daemon.UpdateInfo{
			"linux-amd64": {URL: "u", SignatureURL: "s", SHA256: "abc"},
		},
	}
	srv, _ := signedManifestServer(t, priv, m)
	u := newTestUpdater(t, srv, pub)

	result, err := u.Check(context.Background(), "0.1.0", "linux-amd64")
	if err != nil {
		t.Fatalf("Check with valid signed manifest: %v", err)
	}
	if result == nil || result.Daemon == nil || result.Daemon.Version != "0.2.0" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestCheck_TamperedManifestRejected(t *testing.T) {
	pub, priv, _ := signing.GenerateKeypair()
	m := manifestResponse{
		Version:   "0.2.0",
		Platforms: map[string]daemon.UpdateInfo{"linux-amd64": {SHA256: "abc"}},
	}
	body, _ := json.Marshal(m)
	sig := signing.Sign(priv, body)

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/daemon/manifest.json":
			// Serve a body that does NOT match the signature.
			tampered := append([]byte(nil), body...)
			tampered[2] ^= 0xFF
			_, _ = w.Write(tampered)
		case "/daemon/manifest.json.sig":
			_, _ = w.Write(sig)
		}
	}))
	t.Cleanup(srv.Close)
	u := newTestUpdater(t, srv, pub)

	result, err := u.Check(context.Background(), "0.1.0", "linux-amd64")
	if err == nil {
		t.Fatal("expected error for tampered manifest body")
	}
	if result != nil {
		t.Errorf("expected nil result on verification failure, got %+v", result)
	}
}

func TestCheck_MissingSignatureRejected(t *testing.T) {
	pub, priv, _ := signing.GenerateKeypair()
	m := manifestResponse{Version: "0.2.0"}
	body, _ := json.Marshal(m)
	_ = priv

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/daemon/manifest.json":
			_, _ = w.Write(body)
		case "/daemon/manifest.json.sig":
			http.NotFound(w, r) // signature unavailable
		}
	}))
	t.Cleanup(srv.Close)
	u := newTestUpdater(t, srv, pub)

	_, err := u.Check(context.Background(), "0.1.0", "linux-amd64")
	if err == nil {
		t.Fatal("expected error when manifest signature is missing (must not skip verification)")
	}
}

func TestApply_RejectsNonPinnedHostBeforeDownload(t *testing.T) {
	pub, priv, _ := signing.GenerateKeypair()
	srv, binHits := signedManifestServer(t, priv, manifestResponse{})
	u := newTestUpdater(t, srv, pub)

	binaryPath := filepath.Join(t.TempDir(), "savecraft-daemon")
	info := &daemon.UpdateInfo{
		Version:      "0.2.0",
		URL:          "https://evil.example.com/binary",
		SignatureURL: "https://evil.example.com/binary.sig",
		SHA256:       "00",
	}

	err := u.Apply(context.Background(), info, binaryPath)
	if err == nil {
		t.Fatal("expected rejection for non-pinned update host")
	}
	if *binHits != 0 {
		t.Errorf("download attempted before origin check (%d hits)", *binHits)
	}
	if _, statErr := os.Stat(binaryPath); statErr == nil {
		t.Error("binary must not be written when origin is rejected")
	}
}

func TestApply_RejectsNonHTTPSURL(t *testing.T) {
	pub, priv, _ := signing.GenerateKeypair()
	srv, _ := signedManifestServer(t, priv, manifestResponse{})
	u := newTestUpdater(t, srv, pub)

	// Same host as the pinned origin but plaintext http:// → must be rejected.
	httpURL := "http://" + srv.Listener.Addr().String() + "/binary"
	info := &daemon.UpdateInfo{
		Version:      "0.2.0",
		URL:          httpURL,
		SignatureURL: httpURL + ".sig",
		SHA256:       "00",
	}

	err := u.Apply(context.Background(), info, filepath.Join(t.TempDir(), "d"))
	if err == nil {
		t.Fatal("expected rejection for non-https update URL")
	}
}

func TestApply_RejectsSubdomainPrefixBypass(t *testing.T) {
	pub, priv, _ := signing.GenerateKeypair()
	srv, binHits := signedManifestServer(t, priv, manifestResponse{})
	u := newTestUpdater(t, srv, pub)

	// Host that merely has the pinned host as a prefix must NOT pass.
	info := &daemon.UpdateInfo{
		Version:      "0.2.0",
		URL:          srv.URL + ".evil.com/binary",
		SignatureURL: srv.URL + ".evil.com/binary.sig",
		SHA256:       "00",
	}
	if err := u.Apply(context.Background(), info, filepath.Join(t.TempDir(), "d")); err == nil {
		t.Fatal("expected rejection for prefix/look-alike host")
	}
	if *binHits != 0 {
		t.Errorf("download attempted for look-alike host (%d hits)", *binHits)
	}
}

func TestApply_AcceptsPinnedOriginAndReplaces(t *testing.T) {
	pub, priv, _ := signing.GenerateKeypair()

	binaryData := []byte("new-daemon-bytes")
	binSig := signing.Sign(priv, binaryData)
	hash := sha256.Sum256(binaryData)
	hashHex := hex.EncodeToString(hash[:])

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/binary":
			_, _ = w.Write(binaryData)
		case "/binary.sig":
			_, _ = w.Write(binSig)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	u := New(srv.URL, pub, t.TempDir(), WithHTTPClient(srv.Client()))
	u.manifestPubKey = pub

	binaryPath := filepath.Join(t.TempDir(), "savecraft-daemon")
	info := &daemon.UpdateInfo{
		Version:      "0.2.0",
		URL:          srv.URL + "/binary",
		SignatureURL: srv.URL + "/binary.sig",
		SHA256:       hashHex,
	}
	if err := u.Apply(context.Background(), info, binaryPath); err != nil {
		t.Fatalf("Apply with pinned-origin https URL: %v", err)
	}
	got, _ := os.ReadFile(binaryPath)
	if string(got) != string(binaryData) {
		t.Errorf("binary = %q, want %q", got, binaryData)
	}
}

// TestApply_NilBinaryKeyFailsClosed proves a nil verify key is NOT a skip:
// even a correctly-served binary is rejected because signing.Verify fails
// closed on an absent key (epic R3 — no skip path anywhere).
func TestApply_NilBinaryKeyFailsClosed(t *testing.T) {
	_, priv, _ := signing.GenerateKeypair()
	binaryData := []byte("daemon-bytes")
	binSig := signing.Sign(priv, binaryData)
	hash := sha256.Sum256(binaryData)

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/binary":
			_, _ = w.Write(binaryData)
		case "/binary.sig":
			_, _ = w.Write(binSig)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	// nil binary pubKey — must fail closed, never skip verification.
	u := New(srv.URL, nil, t.TempDir(), WithHTTPClient(srv.Client()))

	binaryPath := filepath.Join(t.TempDir(), "savecraft-daemon")
	info := &daemon.UpdateInfo{
		Version:      "0.2.0",
		URL:          srv.URL + "/binary",
		SignatureURL: srv.URL + "/binary.sig",
		SHA256:       hex.EncodeToString(hash[:]),
	}
	if err := u.Apply(context.Background(), info, binaryPath); err == nil {
		t.Fatal("expected fail-closed with a nil binary verify key")
	}
	if _, statErr := os.Stat(binaryPath); statErr == nil {
		t.Error("binary must not be installed when verify key is nil")
	}
}

func TestApply_EmptyPinnedOriginFailsClosed(t *testing.T) {
	pub, _, _ := signing.GenerateKeypair()
	// installURL empty → no trustworthy pin → must refuse all updates.
	u := New("", pub, t.TempDir())
	u.manifestPubKey = pub

	info := &daemon.UpdateInfo{
		URL:          "https://install.savecraft.gg/binary",
		SignatureURL: "https://install.savecraft.gg/binary.sig",
		SHA256:       "00",
	}
	if err := u.Apply(context.Background(), info, filepath.Join(t.TempDir(), "d")); err == nil {
		t.Fatal("expected fail-closed when pinned origin is empty")
	}
}
