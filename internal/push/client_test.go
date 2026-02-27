package push

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/joshsymonds/savecraft.gg/internal/daemon"
)

func testParsedAt() time.Time {
	return time.Date(2026, 2, 25, 21, 30, 0, 0, time.UTC)
}

func testState() *daemon.GameState {
	return &daemon.GameState{
		Identity: daemon.Identity{
			CharacterName: "Hammerdin",
			GameID:        "d2r",
		},
		Summary: "Hammerdin, Level 89 Paladin",
		Sections: map[string]daemon.Section{
			"overview": {Description: "Character overview", Data: map[string]any{"level": float64(89)}},
		},
	}
}

type capturedRequest struct {
	method  string
	path    string
	headers http.Header
	body    []byte
}

func newTestServer(t *testing.T, status int, response any) (*httptest.Server, *capturedRequest) {
	t.Helper()
	captured := &capturedRequest{}
	var mu sync.Mutex

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		captured.method = r.Method
		captured.path = r.URL.Path
		captured.headers = r.Header.Clone()
		captured.body, _ = io.ReadAll(r.Body)

		w.WriteHeader(status)
		if response != nil {
			json.NewEncoder(w).Encode(response)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, captured
}

func newTestClient(t *testing.T, serverURL, token string) *Client {
	t.Helper()
	client, err := New(serverURL, token)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return client
}

func TestPush_Success(t *testing.T) {
	srv, captured := newTestServer(t, http.StatusCreated, daemon.PushResult{
		SaveUUID:          "abc-123",
		SnapshotTimestamp: "2026-02-25T21:30:00Z",
	})

	client := newTestClient(t, srv.URL, "test-token")
	result, err := client.Push(context.Background(), "d2r", testState(), testParsedAt())
	if err != nil {
		t.Fatalf("Push: %v", err)
	}

	if result.SaveUUID != "abc-123" {
		t.Errorf("SaveUUID = %q, want abc-123", result.SaveUUID)
	}
	if result.SnapshotTimestamp != "2026-02-25T21:30:00Z" {
		t.Errorf("SnapshotTimestamp = %q", result.SnapshotTimestamp)
	}

	if captured.method != http.MethodPost {
		t.Errorf("method = %s, want POST", captured.method)
	}
	if captured.path != "/api/v1/push" {
		t.Errorf("path = %s, want /api/v1/push", captured.path)
	}
}

func TestPush_RequestHeaders(t *testing.T) {
	srv, captured := newTestServer(t, http.StatusCreated, daemon.PushResult{})

	client := newTestClient(t, srv.URL, "my-secret-token")
	_, err := client.Push(context.Background(), "d2r", testState(), testParsedAt())
	if err != nil {
		t.Fatalf("Push: %v", err)
	}

	checks := map[string]string{
		"Authorization": "Bearer my-secret-token",
		"Content-Type":  "application/json",
		"X-Game":        "d2r",
		"X-Parsed-At":   "2026-02-25T21:30:00Z",
	}
	for header, want := range checks {
		if got := captured.headers.Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
}

func TestPush_RequestBody(t *testing.T) {
	srv, captured := newTestServer(t, http.StatusCreated, daemon.PushResult{})

	client := newTestClient(t, srv.URL, "token")
	_, err := client.Push(context.Background(), "d2r", testState(), testParsedAt())
	if err != nil {
		t.Fatalf("Push: %v", err)
	}

	var state daemon.GameState
	if unmarshalErr := json.Unmarshal(captured.body, &state); unmarshalErr != nil {
		t.Fatalf("unmarshal body: %v", unmarshalErr)
	}
	if state.Identity.CharacterName != "Hammerdin" {
		t.Errorf("body character = %q, want Hammerdin", state.Identity.CharacterName)
	}
	if state.Summary != "Hammerdin, Level 89 Paladin" {
		t.Errorf("body summary = %q", state.Summary)
	}
}

func TestPush_GameScopedBody_OmitsCharacterName(t *testing.T) {
	srv, captured := newTestServer(t, http.StatusCreated, daemon.PushResult{})

	state := &daemon.GameState{
		Identity: daemon.Identity{
			GameID: "d2r",
			// CharacterName intentionally empty — game-scoped save.
		},
		Summary: "Shared Stash (Softcore), 60 items",
		Sections: map[string]daemon.Section{
			"overview": {Description: "Shared stash overview", Data: map[string]any{"gold": float64(0)}},
		},
	}

	client := newTestClient(t, srv.URL, "token")
	_, err := client.Push(context.Background(), "d2r", state, testParsedAt())
	if err != nil {
		t.Fatalf("Push: %v", err)
	}

	// The JSON body should not include characterName at all.
	var raw map[string]json.RawMessage
	if unmarshalErr := json.Unmarshal(captured.body, &raw); unmarshalErr != nil {
		t.Fatalf("unmarshal body: %v", unmarshalErr)
	}
	var identity map[string]json.RawMessage
	if unmarshalErr := json.Unmarshal(raw["identity"], &identity); unmarshalErr != nil {
		t.Fatalf("unmarshal identity: %v", unmarshalErr)
	}
	if _, hasCharName := identity["characterName"]; hasCharName {
		t.Error("game-scoped push body should not have characterName key")
	}
	if string(identity["gameId"]) != `"d2r"` {
		t.Errorf("gameId = %s, want \"d2r\"", identity["gameId"])
	}
}

func TestPush_ServerError(t *testing.T) {
	srv, _ := newTestServer(t, http.StatusInternalServerError, nil)

	client := newTestClient(t, srv.URL, "token")
	_, err := client.Push(context.Background(), "d2r", testState(), testParsedAt())
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestPush_Unauthorized(t *testing.T) {
	srv, _ := newTestServer(t, http.StatusUnauthorized, nil)

	client := newTestClient(t, srv.URL, "bad-token")
	_, err := client.Push(context.Background(), "d2r", testState(), testParsedAt())
	if err == nil {
		t.Fatal("expected error for 401 response")
	}
}

func TestPush_ContextCanceled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	client := newTestClient(t, srv.URL, "token")
	_, err := client.Push(ctx, "d2r", testState(), testParsedAt())
	if err == nil {
		t.Fatal("expected error for canceled context")
	}
}

func TestPush_BadURL(t *testing.T) {
	client := newTestClient(t, "http://localhost:0", "token")
	_, err := client.Push(context.Background(), "d2r", testState(), testParsedAt())
	if err == nil {
		t.Fatal("expected error for unreachable server")
	}
}

func TestNew_InvalidURL(t *testing.T) {
	_, err := New("://invalid", "token")
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
}
