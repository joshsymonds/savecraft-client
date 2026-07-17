package pluginmgr

import (
	"context"
	"crypto/ed25519"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/joshsymonds/savecraft-client/internal/signing"
)

const manifestJSON = `{
	"plugins": {
		"d2r": {
			"game_id": "d2r",
			"name": "Diablo II: Resurrected",
			"version": "1.0.0",
			"sha256": "abc123",
			"url": "https://install.example/plugins/d2r/parser.wasm",
			"default_paths": {"windows": "%USERPROFILE%/d2r", "linux": "~/d2r"},
			"file_extensions": [".d2s", ".d2i"]
		}
	}
}`

// signedManifestTLS serves manifestJSON and a detached .sig over the exact
// bytes on a TLS server. When withSig is false the signature 404s (simulating
// a missing signature — verification must still hard-fail).
func signedManifestTLS(t *testing.T, priv ed25519.PrivateKey, withSig bool) *httptest.Server {
	t.Helper()
	sig := signing.Sign(priv, []byte(manifestJSON))
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/plugins/manifest.json":
			_, _ = w.Write([]byte(manifestJSON))
		case "/plugins/manifest.json.sig":
			if !withSig {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write(sig)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestHTTPRegistry_FetchManifest_ValidSigned(t *testing.T) {
	pub, priv, _ := signing.GenerateKeypair()
	srv := signedManifestTLS(t, priv, true)
	reg := NewHTTPRegistry(srv.URL, pub, WithHTTPClient(srv.Client()))

	got, err := reg.FetchManifest(context.Background())
	if err != nil {
		t.Fatalf("FetchManifest: %v", err)
	}
	info, ok := got["d2r"]
	if !ok || info.Version != "1.0.0" || info.Name != "Diablo II: Resurrected" {
		t.Fatalf("unexpected manifest: %+v", got)
	}
	if len(info.FileExtensions) != 2 || info.DefaultPaths["linux"] != "~/d2r" {
		t.Errorf("fields not decoded: %+v", info)
	}
}

func TestHTTPRegistry_FetchManifest_TamperedRejected(t *testing.T) {
	pub, priv, _ := signing.GenerateKeypair()
	// Signature is over manifestJSON, but the server serves a different body.
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/plugins/manifest.json":
			_, _ = w.Write([]byte(`{"plugins":{"evil":{"game_id":"evil"}}}`))
		case "/plugins/manifest.json.sig":
			_, _ = w.Write(signing.Sign(priv, []byte(manifestJSON)))
		}
	}))
	t.Cleanup(srv.Close)

	reg := NewHTTPRegistry(srv.URL, pub, WithHTTPClient(srv.Client()))
	if _, err := reg.FetchManifest(context.Background()); err == nil {
		t.Fatal("expected error for tampered manifest body")
	}
}

func TestHTTPRegistry_FetchManifest_MissingSigRejected(t *testing.T) {
	pub, priv, _ := signing.GenerateKeypair()
	srv := signedManifestTLS(t, priv, false) // 0-len → 404 sig
	reg := NewHTTPRegistry(srv.URL, pub, WithHTTPClient(srv.Client()))
	if _, err := reg.FetchManifest(context.Background()); err == nil {
		t.Fatal("expected error when manifest signature is missing")
	}
}

func TestHTTPRegistry_FetchManifest_WrongKeyRejected(t *testing.T) {
	_, priv, _ := signing.GenerateKeypair()
	wrongPub, _, _ := signing.GenerateKeypair()
	srv := signedManifestTLS(t, priv, true)
	reg := NewHTTPRegistry(srv.URL, wrongPub, WithHTTPClient(srv.Client()))
	if _, err := reg.FetchManifest(context.Background()); err == nil {
		t.Fatal("expected error verifying with the wrong key")
	}
}

func TestHTTPRegistry_FetchManifest_NonHTTPSOriginRejected(t *testing.T) {
	pub, priv, _ := signing.GenerateKeypair()
	srv := signedManifestTLS(t, priv, true)
	// Plain-http install origin → origin pin must reject before any fetch.
	reg := NewHTTPRegistry("http://install.example", pub, WithHTTPClient(srv.Client()))
	if _, err := reg.FetchManifest(context.Background()); err == nil {
		t.Fatal("expected rejection for non-https install origin")
	}
}

func TestHTTPRegistry_Download_PinnedOrigin(t *testing.T) {
	pub, _, _ := signing.GenerateKeypair()
	data := []byte("wasm binary data")
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(data)
	}))
	t.Cleanup(srv.Close)

	reg := NewHTTPRegistry(srv.URL, pub, WithHTTPClient(srv.Client()))
	got, err := reg.Download(context.Background(), srv.URL+"/plugins/d2r/parser.wasm")
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("data = %q, want %q", got, data)
	}
}

func TestHTTPRegistry_Download_OffOriginRejected(t *testing.T) {
	pub, _, _ := signing.GenerateKeypair()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("x"))
	}))
	t.Cleanup(srv.Close)

	reg := NewHTTPRegistry(srv.URL, pub, WithHTTPClient(srv.Client()))
	if _, err := reg.Download(context.Background(), "https://evil.example/plugins/d2r/parser.wasm"); err == nil {
		t.Fatal("expected rejection for off-origin download URL")
	}
	// Same host as the pinned origin but plaintext http:// → rejected.
	httpURL := "http://" + srv.Listener.Addr().String() + "/x"
	if _, err := reg.Download(context.Background(), httpURL); err == nil {
		t.Fatal("expected rejection for non-https download URL")
	}
}

func TestHTTPRegistry_Download_Non200(t *testing.T) {
	pub, _, _ := signing.GenerateKeypair()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	reg := NewHTTPRegistry(srv.URL, pub, WithHTTPClient(srv.Client()))
	if _, err := reg.Download(context.Background(), srv.URL+"/missing.wasm"); err == nil {
		t.Fatal("expected error for 404 response")
	}
}
