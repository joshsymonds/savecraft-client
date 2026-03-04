package localapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClient_Boot(t *testing.T) {
	srv := NewServer("localhost:0", nil)
	srv.SetState(StateRunning)

	ts := httptest.NewServer(srv.mux)
	defer ts.Close()

	client := NewClient(ts.URL)
	resp, err := client.Boot(context.Background())
	if err != nil {
		t.Fatalf("Boot: %v", err)
	}
	if resp.State != StateRunning {
		t.Errorf("state = %q, want %q", resp.State, StateRunning)
	}
}

func TestClient_Boot_Error(t *testing.T) {
	srv := NewServer("localhost:0", nil)
	srv.SetError("registration failed")

	ts := httptest.NewServer(srv.mux)
	defer ts.Close()

	client := NewClient(ts.URL)
	resp, err := client.Boot(context.Background())
	if err != nil {
		t.Fatalf("Boot: %v", err)
	}
	if resp.State != StateError {
		t.Errorf("state = %q, want %q", resp.State, StateError)
	}
	if resp.Error != "registration failed" {
		t.Errorf("error = %q, want %q", resp.Error, "registration failed")
	}
}

func TestClient_Link_Registered(t *testing.T) {
	srv := NewServer("localhost:0", nil)
	srv.SetRegistered("123456", "https://savecraft.gg/link/123456", "2026-03-03T12:20:00Z")

	ts := httptest.NewServer(srv.mux)
	defer ts.Close()

	client := NewClient(ts.URL)
	resp, status, err := client.Link(context.Background())
	if err != nil {
		t.Fatalf("Link: %v", err)
	}
	if status != http.StatusOK {
		t.Errorf("status = %d, want 200", status)
	}
	if resp.LinkCode != "123456" {
		t.Errorf("linkCode = %q, want %q", resp.LinkCode, "123456")
	}
}

func TestClient_Link_NotReady(t *testing.T) {
	srv := NewServer("localhost:0", nil)

	ts := httptest.NewServer(srv.mux)
	defer ts.Close()

	client := NewClient(ts.URL)
	resp, status, err := client.Link(context.Background())
	if err != nil {
		t.Fatalf("Link: %v", err)
	}
	if status != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", status)
	}
	if resp.Error != "device not yet registered" {
		t.Errorf("error = %q, want %q", resp.Error, "device not yet registered")
	}
}

func TestClient_Link_AlreadyRegistered(t *testing.T) {
	srv := NewServer("localhost:0", nil)
	srv.SetState(StateRunning)

	ts := httptest.NewServer(srv.mux)
	defer ts.Close()

	client := NewClient(ts.URL)
	resp, status, err := client.Link(context.Background())
	if err != nil {
		t.Fatalf("Link: %v", err)
	}
	if status != http.StatusNotFound {
		t.Errorf("status = %d, want 404", status)
	}
	if resp.Error != "device was already registered" {
		t.Errorf("error = %q, want %q", resp.Error, "device was already registered")
	}
}

func TestClient_Status(t *testing.T) {
	srv := NewServer("localhost:0", nil)
	srv.Handle("/status", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"version": "1.0.0"})
	}))

	ts := httptest.NewServer(srv.mux)
	defer ts.Close()

	client := NewClient(ts.URL)
	raw, err := client.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}

	var parsed map[string]string
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed["version"] != "1.0.0" {
		t.Errorf("version = %q, want %q", parsed["version"], "1.0.0")
	}
}

func TestClient_Logs(t *testing.T) {
	inner := slog.NewJSONHandler(discardWriter{}, nil)
	rb := NewRingBuffer(10, inner)
	logger := slog.New(rb)

	srv := NewServer("localhost:0", nil)
	srv.SetRingBuffer(rb)

	logger.Info("test log")

	ts := httptest.NewServer(srv.mux)
	defer ts.Close()

	client := NewClient(ts.URL)
	entries, err := client.Logs(context.Background())
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if entries[0].Message != "test log" {
		t.Errorf("msg = %q, want %q", entries[0].Message, "test log")
	}
}

func TestClient_Shutdown(t *testing.T) {
	done := make(chan struct{})
	srv := NewServer("localhost:0", nil)
	srv.SetShutdownFunc(func() { close(done) })

	ts := httptest.NewServer(srv.mux)
	defer ts.Close()

	client := NewClient(ts.URL)
	err := client.Shutdown(context.Background())
	if err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("shutdown callback was not called within 1s")
	}
}

func TestClient_Restart(t *testing.T) {
	var called bool
	srv := NewServer("localhost:0", nil)
	srv.SetRestartFunc(func() error {
		called = true
		return nil
	})

	ts := httptest.NewServer(srv.mux)
	defer ts.Close()

	client := NewClient(ts.URL)
	err := client.Restart(context.Background())
	if err != nil {
		t.Fatalf("Restart: %v", err)
	}
	if !called {
		t.Error("restart callback was not called")
	}
}

func TestClient_ConnectionRefused(t *testing.T) {
	client := NewClient("http://localhost:1") // nothing listening

	_, err := client.Boot(context.Background())
	if err == nil {
		t.Fatal("expected error for connection refused")
	}
}
