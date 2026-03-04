package localapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleBoot_InitialState(t *testing.T) {
	srv := NewServer("localhost:0", nil)

	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/boot", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp BootResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.State != StateStarting {
		t.Errorf("state = %q, want %q", resp.State, StateStarting)
	}
}

func TestHandleBoot_Registering(t *testing.T) {
	srv := NewServer("localhost:0", nil)
	srv.SetState(StateRegistering)

	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/boot", nil))

	var resp BootResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.State != StateRegistering {
		t.Errorf("state = %q, want %q", resp.State, StateRegistering)
	}
}

func TestHandleBoot_Error(t *testing.T) {
	srv := NewServer("localhost:0", nil)
	srv.SetError("connection refused")

	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/boot", nil))

	var resp BootResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.State != StateError {
		t.Errorf("state = %q, want %q", resp.State, StateError)
	}
	if resp.Error != "connection refused" {
		t.Errorf("error = %q, want %q", resp.Error, "connection refused")
	}
}

func TestHandleLink_BeforeRegistration(t *testing.T) {
	srv := NewServer("localhost:0", nil)

	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/link", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}

	var resp LinkResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.State != StateStarting {
		t.Errorf("state = %q, want %q", resp.State, StateStarting)
	}
}

func TestHandleLink_AfterRegistration(t *testing.T) {
	srv := NewServer("localhost:0", nil)
	srv.SetRegistered("482913", "https://savecraft.gg/link/482913", "2026-03-03T12:20:00Z")

	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/link", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp LinkResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.LinkCode != "482913" {
		t.Errorf("linkCode = %q, want %q", resp.LinkCode, "482913")
	}
	if resp.LinkURL != "https://savecraft.gg/link/482913" {
		t.Errorf("linkUrl = %q, want %q", resp.LinkURL, "https://savecraft.gg/link/482913")
	}
	if resp.ExpiresAt != "2026-03-03T12:20:00Z" {
		t.Errorf("expiresAt = %q, want %q", resp.ExpiresAt, "2026-03-03T12:20:00Z")
	}
}

func TestHandleLink_AlreadyRegistered(t *testing.T) {
	srv := NewServer("localhost:0", nil)
	srv.SetState(StateRunning)

	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/link", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestBuildLinkURL(t *testing.T) {
	got := BuildLinkURL("https://savecraft.gg", "482913")
	want := "https://savecraft.gg/link/482913"
	if got != want {
		t.Errorf("BuildLinkURL = %q, want %q", got, want)
	}
}

func TestBuildLinkURL_TrailingSlash(t *testing.T) {
	got := BuildLinkURL("https://savecraft.gg/", "482913")
	want := "https://savecraft.gg/link/482913"
	if got != want {
		t.Errorf("BuildLinkURL = %q, want %q", got, want)
	}
}

func TestServer_Handle_ExtendsRoutes(t *testing.T) {
	srv := NewServer("localhost:0", nil)
	srv.Handle("/custom", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))

	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/custom", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "ok" {
		t.Errorf("body = %q, want %q", rec.Body.String(), "ok")
	}
}

func TestServer_ConcurrentStateAccess(t *testing.T) {
	srv := NewServer("localhost:0", nil)

	done := make(chan struct{})
	go func() {
		for range 100 {
			srv.SetState(StateRunning)
			srv.SetState(StateRegistering)
			srv.SetRegistered("code", "url", "exp")
			srv.SetError("err")
		}
		close(done)
	}()

	for range 100 {
		rec := httptest.NewRecorder()
		srv.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/boot", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("concurrent /boot returned %d", rec.Code)
		}
	}

	<-done
}
