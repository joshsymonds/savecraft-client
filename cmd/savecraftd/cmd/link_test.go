package cmd

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/joshsymonds/savecraft.gg/internal/localapi"
)

// bootState reads the current state from a localapi.Server via its HTTP handler.
func bootState(t *testing.T, api *localapi.Server) localapi.State {
	t.Helper()

	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/boot", nil))

	var resp localapi.BootResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode boot response: %v", err)
	}

	return resp.State
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestWaitForLink_TransitionsWhenLinked(t *testing.T) {
	t.Parallel()

	var pollCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.Header().Set("Content-Type", "application/json")

		if req.URL.Path == "/api/v1/source/status" {
			n := pollCount.Add(1)

			// Linked on the 3rd poll.
			linked := n >= 3
			json.NewEncoder(rw).Encode(map[string]any{"linked": linked})

			return
		}

		rw.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	api := localapi.NewServer("localhost:0", nil)
	expiresAt := time.Now().Add(20 * time.Minute).UTC().Format(time.RFC3339)

	err := waitForLink(
		context.Background(),
		srv.URL, "sct_test", "https://savecraft.gg",
		api, "123456", expiresAt,
		10*time.Millisecond,
		discardLogger(),
	)
	if err != nil {
		t.Fatalf("waitForLink: %v", err)
	}

	if n := pollCount.Load(); n < 3 {
		t.Errorf("poll count = %d, expected >= 3", n)
	}
}

func TestWaitForLink_RespectsContextCancellation(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		rw.Header().Set("Content-Type", "application/json")
		json.NewEncoder(rw).Encode(map[string]any{"linked": false})
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	api := localapi.NewServer("localhost:0", nil)
	expiresAt := time.Now().Add(20 * time.Minute).UTC().Format(time.RFC3339)

	done := make(chan error, 1)
	go func() {
		done <- waitForLink(
			ctx,
			srv.URL, "sct_test", "https://savecraft.gg",
			api, "123456", expiresAt,
			10*time.Millisecond,
			discardLogger(),
		)
	}()

	// Let it poll a few times, then cancel.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != context.Canceled {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("waitForLink did not return after context cancellation")
	}
}

func TestWaitForLink_RefreshesExpiredCode(t *testing.T) {
	t.Parallel()

	var refreshCalled atomic.Bool
	var statusCalls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.Header().Set("Content-Type", "application/json")

		switch req.URL.Path {
		case "/api/v1/source/link-code":
			refreshCalled.Store(true)
			json.NewEncoder(rw).Encode(map[string]string{
				"link_code":  "654321",
				"expires_at": time.Now().Add(20 * time.Minute).UTC().Format(time.RFC3339),
			})
		case "/api/v1/source/status":
			n := statusCalls.Add(1)
			// Link after refresh has been called.
			linked := n >= 2 && refreshCalled.Load()
			json.NewEncoder(rw).Encode(map[string]any{"linked": linked})
		default:
			rw.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	api := localapi.NewServer("localhost:0", nil)

	// Set expiry in the past so it triggers refresh immediately.
	expiresAt := time.Now().Add(-1 * time.Minute).UTC().Format(time.RFC3339)

	err := waitForLink(
		context.Background(),
		srv.URL, "sct_test", "https://savecraft.gg",
		api, "123456", expiresAt,
		10*time.Millisecond,
		discardLogger(),
	)
	if err != nil {
		t.Fatalf("waitForLink: %v", err)
	}

	if !refreshCalled.Load() {
		t.Error("expected RefreshLinkCode to be called for expired code")
	}
}

func TestWaitForLink_SetsRegisteredState(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		rw.Header().Set("Content-Type", "application/json")
		// Link immediately.
		json.NewEncoder(rw).Encode(map[string]any{"linked": true})
	}))
	defer srv.Close()

	api := localapi.NewServer("localhost:0", nil)
	expiresAt := time.Now().Add(20 * time.Minute).UTC().Format(time.RFC3339)

	err := waitForLink(
		context.Background(),
		srv.URL, "sct_test", "https://savecraft.gg",
		api, "999888", expiresAt,
		10*time.Millisecond,
		discardLogger(),
	)
	if err != nil {
		t.Fatalf("waitForLink: %v", err)
	}

	// After linking, state should be running.
	if state := bootState(t, api); state != localapi.StateRunning {
		t.Errorf("state = %q, want running", state)
	}
}
