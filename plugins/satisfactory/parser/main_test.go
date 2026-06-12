package main

import (
	"strings"
	"testing"
	"time"

	"github.com/joshsymonds/savecraft.gg/plugins/satisfactory/sav"
)

func testHeader() *sav.Header {
	return &sav.Header{
		HeaderVersion: 14,
		SaveVersion:   58,
		BuildVersion:  423794,
		SaveName:      "MyFactory_autosave_0",
		MapName:       "Persistent_Level",
		SessionName:   "MyFactory",
		PlayDuration:  58723 * time.Second,
		SaveTime:      time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	}
}

func TestBuildResultIdentity(t *testing.T) {
	result := buildResult(testHeader())

	identity, ok := result["identity"].(map[string]any)
	if !ok {
		t.Fatalf("identity missing or wrong type: %v", result["identity"])
	}
	// Identity is the session, not the file: autosave rotation must map
	// MyFactory_autosave_0/1/2 onto ONE save, keyed by session name.
	if identity["saveName"] != "MyFactory" {
		t.Errorf("saveName = %v, want MyFactory", identity["saveName"])
	}
	if identity["gameId"] != "satisfactory" {
		t.Errorf("gameId = %v, want satisfactory", identity["gameId"])
	}
}

func TestBuildResultSummary(t *testing.T) {
	result := buildResult(testHeader())
	summary, _ := result["summary"].(string)
	if !strings.Contains(summary, "MyFactory") {
		t.Errorf("summary %q should contain session name", summary)
	}
	if !strings.Contains(summary, "16.3") {
		t.Errorf("summary %q should contain playtime hours (16.3)", summary)
	}
}

func TestBuildResultGameOverview(t *testing.T) {
	h := testHeader()
	h.CreativeMode = true
	h.Modded = true
	result := buildResult(h)

	sections, ok := result["sections"].(map[string]any)
	if !ok {
		t.Fatalf("sections missing: %v", result["sections"])
	}
	overview, ok := sections["game_overview"].(map[string]any)
	if !ok {
		t.Fatalf("game_overview missing: %v", sections)
	}
	if desc, _ := overview["description"].(string); desc == "" {
		t.Error("game_overview description is empty")
	}
	data, ok := overview["data"].(map[string]any)
	if !ok {
		t.Fatalf("game_overview data is not an object: %v", overview["data"])
	}

	if data["sessionName"] != "MyFactory" {
		t.Errorf("sessionName = %v", data["sessionName"])
	}
	if data["playTimeSeconds"] != int32(58723) {
		t.Errorf("playTimeSeconds = %v (%T)", data["playTimeSeconds"], data["playTimeSeconds"])
	}
	if data["savedAt"] != "2026-01-02T03:04:05Z" {
		t.Errorf("savedAt = %v", data["savedAt"])
	}
	if data["creativeMode"] != true {
		t.Errorf("creativeMode = %v", data["creativeMode"])
	}
	if data["modded"] != true {
		t.Errorf("modded = %v", data["modded"])
	}
	if data["gameBuild"] != int32(423794) {
		t.Errorf("gameBuild = %v", data["gameBuild"])
	}
}

func TestErrorTypeMapping(t *testing.T) {
	if got := errorType(&sav.UnsupportedVersionError{HeaderVersion: 12}); got != "unsupported_version" {
		t.Errorf("errorType(UnsupportedVersionError) = %q, want unsupported_version", got)
	}
	if got := errorType(strings.NewReader("").UnreadByte()); got != "corrupt_file" {
		t.Errorf("errorType(generic) = %q, want corrupt_file", got)
	}
}
