package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	pb "github.com/joshsymonds/savecraft.gg/internal/proto/savecraft/v1"
)

func TestStatus_ReturnsSnapshot(t *testing.T) {
	ws := newFakeWSClient()
	cfg := Config{
		SourceID: "steam-deck",
		Version:  "1.2.3",
		Games: map[string]GameConfig{
			"d2r": {
				SavePath:       "/saves/d2r",
				Enabled:        true,
				FileExtensions: []string{".d2s"},
			},
			"bg3": {
				SavePath:       "/saves/bg3",
				Enabled:        false,
				FileExtensions: []string{".lsv"},
			},
		},
	}

	d := New(
		cfg, &fakeFS{}, newFakeWatcher(), &fakeRunner{}, &fakePushClient{},
		ws, &fakePluginManager{}, nil, testLogger(),
	)
	d.watchedDirs["/saves/d2r"] = "d2r"

	status := d.Status()

	if status.Version != "1.2.3" {
		t.Errorf("version = %q, want %q", status.Version, "1.2.3")
	}
	if status.SourceID != "steam-deck" {
		t.Errorf("sourceId = %q, want %q", status.SourceID, "steam-deck")
	}
	if len(status.Games) != 2 {
		t.Fatalf("games count = %d, want 2", len(status.Games))
	}

	d2r := status.Games["d2r"]
	if !d2r.Watching {
		t.Error("d2r should be watching")
	}
	if !d2r.Enabled {
		t.Error("d2r should be enabled")
	}
	if d2r.SavePath != "/saves/d2r" {
		t.Errorf("d2r savePath = %q, want %q", d2r.SavePath, "/saves/d2r")
	}

	bg3 := status.Games["bg3"]
	if bg3.Watching {
		t.Error("bg3 should not be watching")
	}
	if bg3.Enabled {
		t.Error("bg3 should not be enabled")
	}
}

func TestStatus_WSConnected(t *testing.T) {
	ws := newFakeWSClient()
	cfg := Config{
		SourceID: "deck",
		Version:  "0.1.0",
		Games:    map[string]GameConfig{},
	}
	d := New(
		cfg, &fakeFS{}, newFakeWatcher(), &fakeRunner{}, &fakePushClient{},
		ws, &fakePluginManager{}, nil, testLogger(),
	)

	// Before Connect, ws.connected is false.
	status := d.Status()
	if status.WSConnected {
		t.Error("should not be connected before Connect()")
	}

	// After Connect, ws.connected is true.
	ws.Connect(context.Background())

	status = d.Status()
	if !status.WSConnected {
		t.Error("should be connected after Connect()")
	}
}

func TestStatusHandler_ReturnsJSON(t *testing.T) {
	ws := newFakeWSClient()
	cfg := Config{
		SourceID: "deck",
		Version:  "0.1.0",
		Games: map[string]GameConfig{
			"d2r": {SavePath: "/saves/d2r", Enabled: true, FileExtensions: []string{".d2s"}},
		},
	}
	d := New(
		cfg, &fakeFS{}, newFakeWatcher(), &fakeRunner{}, &fakePushClient{},
		ws, &fakePluginManager{}, nil, testLogger(),
	)

	handler := StatusHandler(d)
	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status code = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("content-type = %q, want application/json", ct)
	}

	var status DaemonStatus
	if err := json.NewDecoder(rec.Body).Decode(&status); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if status.Version != "0.1.0" {
		t.Errorf("version = %q, want %q", status.Version, "0.1.0")
	}
	if len(status.Games) != 1 {
		t.Errorf("games count = %d, want 1", len(status.Games))
	}
}

func TestNew_NilLogger(t *testing.T) {
	ws := newFakeWSClient()
	cfg := Config{
		SourceID: "deck",
		Version:  "0.1.0",
		Games:    map[string]GameConfig{},
	}

	// Passing nil logger should not panic.
	d := New(
		cfg, &fakeFS{}, newFakeWatcher(), &fakeRunner{}, &fakePushClient{},
		ws, &fakePluginManager{}, nil, nil,
	)

	// Using the daemon should not panic — nil logger replaced with no-op.
	d.sendMessage(
		context.Background(),
		&pb.Message{Payload: &pb.Message_SourceHeartbeat{SourceHeartbeat: &pb.SourceHeartbeat{}}},
	)
	status := d.Status()
	if status.Version != "0.1.0" {
		t.Errorf("version = %q, want %q", status.Version, "0.1.0")
	}
}
