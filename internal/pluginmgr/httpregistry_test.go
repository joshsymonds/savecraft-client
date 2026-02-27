package pluginmgr

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPRegistry_FetchManifest(t *testing.T) {
	manifest := map[string]PluginInfo{
		"d2r": {
			GameID:  "d2r",
			Version: "1.0.0",
			SHA256:  "abc123",
			URL:     "https://example.com/d2r.wasm",
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/plugins/manifest" {
				t.Errorf("unexpected path: %s", r.URL.Path)
			}
			if got := r.Header.Get("Authorization"); got != "Bearer tok123" {
				t.Errorf("auth header = %q, want Bearer tok123", got)
			}
			w.Header().Set("Content-Type", "application/json")
			body := struct {
				Plugins map[string]PluginInfo `json:"plugins"`
			}{Plugins: manifest}
			_ = json.NewEncoder(w).Encode(body)
		},
	))
	defer srv.Close()

	reg := NewHTTPRegistry(srv.URL, "tok123")
	got, err := reg.FetchManifest(context.Background())
	if err != nil {
		t.Fatalf("FetchManifest: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d plugins, want 1", len(got))
	}
	info := got["d2r"]
	if info.Version != "1.0.0" {
		t.Errorf("version = %q, want 1.0.0", info.Version)
	}
}

func TestHTTPRegistry_FetchManifest_NoAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if got := r.Header.Get("Authorization"); got != "" {
				t.Errorf("auth header = %q, want empty", got)
			}
			body := struct {
				Plugins map[string]PluginInfo `json:"plugins"`
			}{Plugins: map[string]PluginInfo{}}
			_ = json.NewEncoder(w).Encode(body)
		},
	))
	defer srv.Close()

	reg := NewHTTPRegistry(srv.URL, "")
	_, err := reg.FetchManifest(context.Background())
	if err != nil {
		t.Fatalf("FetchManifest: %v", err)
	}
}

func TestHTTPRegistry_FetchManifest_Non200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		},
	))
	defer srv.Close()

	reg := NewHTTPRegistry(srv.URL, "")
	_, err := reg.FetchManifest(context.Background())
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestHTTPRegistry_FetchManifest_BadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("not json"))
		},
	))
	defer srv.Close()

	reg := NewHTTPRegistry(srv.URL, "")
	_, err := reg.FetchManifest(context.Background())
	if err == nil {
		t.Fatal("expected error for bad JSON")
	}
}

func TestHTTPRegistry_Download(t *testing.T) {
	data := []byte("wasm binary data")
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if got := r.Header.Get("Authorization"); got != "Bearer secret" {
				t.Errorf("auth header = %q, want Bearer secret", got)
			}
			_, _ = w.Write(data)
		},
	))
	defer srv.Close()

	reg := NewHTTPRegistry("", "secret")
	got, err := reg.Download(context.Background(), srv.URL+"/plugin.wasm")
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("data = %q, want %q", got, data)
	}
}

func TestHTTPRegistry_Download_Non200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		},
	))
	defer srv.Close()

	reg := NewHTTPRegistry("", "")
	_, err := reg.Download(context.Background(), srv.URL+"/missing.wasm")
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
}
