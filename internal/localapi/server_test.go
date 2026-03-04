package localapi

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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

func TestHandleLogs_ReturnsEntries(t *testing.T) {
	inner := testHandler{}
	rb := NewRingBuffer(10, inner)
	logger := slog.New(rb)

	srv := NewServer("localhost:0", nil)
	srv.SetRingBuffer(rb)

	logger.Info("hello")
	logger.Warn("world")

	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/logs", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp LogsResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(resp.Entries))
	}
	if resp.Entries[0].Message != "hello" {
		t.Errorf("entries[0].msg = %q, want %q", resp.Entries[0].Message, "hello")
	}
	if resp.Entries[1].Message != "world" {
		t.Errorf("entries[1].msg = %q, want %q", resp.Entries[1].Message, "world")
	}
}

func TestHandleLogs_EmptyBuffer(t *testing.T) {
	rb := NewRingBuffer(10, testHandler{})

	srv := NewServer("localhost:0", nil)
	srv.SetRingBuffer(rb)

	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/logs", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp LogsResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Entries) != 0 {
		t.Errorf("entries = %d, want 0", len(resp.Entries))
	}
}

func TestHandleLogs_NoBuffer(t *testing.T) {
	srv := NewServer("localhost:0", nil)

	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/logs", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestHandleShutdown_CallsCallback(t *testing.T) {
	done := make(chan struct{})
	srv := NewServer("localhost:0", nil)
	srv.SetShutdownFunc(func() { close(done) })

	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/shutdown", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp OKResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.OK {
		t.Error("ok = false, want true")
	}

	// Shutdown runs in a goroutine so the response can be written first.
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("shutdown callback was not called within 1s")
	}
}

func TestHandleShutdown_NoCallback(t *testing.T) {
	srv := NewServer("localhost:0", nil)

	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/shutdown", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestHandleShutdown_WrongMethod(t *testing.T) {
	srv := NewServer("localhost:0", nil)
	srv.SetShutdownFunc(func() {})

	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/shutdown", nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestHandleRestart_CallsCallback(t *testing.T) {
	var called bool
	srv := NewServer("localhost:0", nil)
	srv.SetRestartFunc(func() error {
		called = true
		return nil
	})

	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/restart", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp OKResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.OK {
		t.Error("ok = false, want true")
	}
	if !called {
		t.Error("restart callback was not called")
	}
}

func TestHandleRestart_Error(t *testing.T) {
	srv := NewServer("localhost:0", nil)
	srv.SetRestartFunc(func() error {
		return fmt.Errorf("restart failed")
	})

	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/restart", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}

	var resp OKResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.OK {
		t.Error("ok = true, want false")
	}
	if resp.Error != "restart failed" {
		t.Errorf("error = %q, want %q", resp.Error, "restart failed")
	}
}

func TestHandleRestart_WrongMethod(t *testing.T) {
	srv := NewServer("localhost:0", nil)
	srv.SetRestartFunc(func() error { return nil })

	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/restart", nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestHandleRestart_NoCallback(t *testing.T) {
	srv := NewServer("localhost:0", nil)

	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/restart", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
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
