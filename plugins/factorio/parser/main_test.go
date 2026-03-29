package main

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

// runParser compiles and runs the parser with the given input, returning stdout and exit code.
func runParser(t *testing.T, input string) (string, int) {
	t.Helper()

	// Build the parser as a regular binary for testing (not WASM).
	tmpBin := t.TempDir() + "/parser"
	build := exec.Command("go", "build", "-o", tmpBin, ".")
	build.Dir = "."
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %v\n%s", err, out)
	}

	cmd := exec.Command(tmpBin)
	cmd.Stdin = strings.NewReader(input)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &bytes.Buffer{}

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("run failed: %v", err)
		}
	}
	return stdout.String(), exitCode
}

func TestValidExport(t *testing.T) {
	input := `{
		"identity": {"save_name": "abc-123", "game_id": "factorio"},
		"summary": "Factorio — 16.7 hours, 3 rockets launched",
		"sections": {
			"game_overview": {
				"description": "Map identity and high-level game state",
				"data": {"hours_played": 16.7, "rocket_launches": 3}
			}
		}
	}`

	out, code := runParser(t, input)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d. output: %s", code, out)
	}

	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 ndjson lines (2 status + 1 result), got %d: %s", len(lines), out)
	}

	// Check status lines.
	var status map[string]string
	if err := json.Unmarshal([]byte(lines[0]), &status); err != nil {
		t.Fatalf("parse status line 0: %v", err)
	}
	if status["type"] != "status" {
		t.Errorf("line 0 type = %q, want status", status["type"])
	}

	// Check result line.
	var result map[string]any
	if err := json.Unmarshal([]byte(lines[2]), &result); err != nil {
		t.Fatalf("parse result line: %v", err)
	}
	if result["type"] != "result" {
		t.Errorf("result type = %q, want result", result["type"])
	}

	identity := result["identity"].(map[string]any)
	if identity["saveName"] != "abc-123" {
		t.Errorf("saveName = %q, want abc-123", identity["saveName"])
	}
	if identity["gameId"] != "factorio" {
		t.Errorf("gameId = %q, want factorio", identity["gameId"])
	}
	if result["summary"] != "Factorio — 16.7 hours, 3 rockets launched" {
		t.Errorf("unexpected summary: %v", result["summary"])
	}

	sections := result["sections"].(map[string]any)
	overview := sections["game_overview"].(map[string]any)
	if overview["description"] != "Map identity and high-level game state" {
		t.Errorf("unexpected description: %v", overview["description"])
	}
	data := overview["data"].(map[string]any)
	if data["hours_played"] != 16.7 {
		t.Errorf("hours_played = %v, want 16.7", data["hours_played"])
	}
}

func TestInvalidJSON(t *testing.T) {
	out, code := runParser(t, "not json at all")
	if code != 1 {
		t.Fatalf("expected exit 1, got %d", code)
	}

	lines := strings.Split(strings.TrimSpace(out), "\n")
	// Should have status + error lines.
	lastLine := lines[len(lines)-1]
	var errMsg map[string]string
	if err := json.Unmarshal([]byte(lastLine), &errMsg); err != nil {
		t.Fatalf("parse error line: %v", err)
	}
	if errMsg["type"] != "error" {
		t.Errorf("type = %q, want error", errMsg["type"])
	}
	if errMsg["errorType"] != "corrupt_file" {
		t.Errorf("errorType = %q, want corrupt_file", errMsg["errorType"])
	}
}

func TestMissingSaveName(t *testing.T) {
	input := `{
		"identity": {"save_name": "", "game_id": "factorio"},
		"summary": "test",
		"sections": {"s": {"description": "d", "data": {"a": 1}}}
	}`

	_, code := runParser(t, input)
	if code != 1 {
		t.Fatalf("expected exit 1 for missing save_name, got %d", code)
	}
}

func TestArrayDataRejected(t *testing.T) {
	input := `{
		"identity": {"save_name": "test", "game_id": "factorio"},
		"summary": "test",
		"sections": {"bad": {"description": "d", "data": [1,2,3]}}
	}`

	_, code := runParser(t, input)
	if code != 1 {
		t.Fatalf("expected exit 1 for array section data, got %d", code)
	}
}

func TestMultipleSections(t *testing.T) {
	input := `{
		"identity": {"save_name": "factory-1", "game_id": "factorio"},
		"summary": "Factorio — 5.0 hours",
		"sections": {
			"game_overview": {"description": "Overview", "data": {"hours": 5}},
			"production_flow": {"description": "Production", "data": {"items": {}}}
		}
	}`

	out, code := runParser(t, input)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d. output: %s", code, out)
	}

	lines := strings.Split(strings.TrimSpace(out), "\n")
	lastLine := lines[len(lines)-1]
	var result map[string]any
	if err := json.Unmarshal([]byte(lastLine), &result); err != nil {
		t.Fatalf("parse result: %v", err)
	}

	sections := result["sections"].(map[string]any)
	if len(sections) != 2 {
		t.Errorf("expected 2 sections, got %d", len(sections))
	}
}
