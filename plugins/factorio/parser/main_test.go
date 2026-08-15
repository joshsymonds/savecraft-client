package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
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
	input := syntheticFullExport(t)

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
	if len(sections) != 12 {
		t.Fatalf("expected all 12 normal export sections, got %d", len(sections))
	}
	overview := sections["game_overview"].(map[string]any)
	if overview["description"] != "Map identity and high-level game state" {
		t.Errorf("unexpected description: %v", overview["description"])
	}
	data := overview["data"].(map[string]any)
	if data["hours_played"] != 16.7 {
		t.Errorf("hours_played = %v, want 16.7", data["hours_played"])
	}
	assertSectionSizes(t, "factorio", sections)
}

func syntheticFullExport(t *testing.T) string {
	t.Helper()
	section := func(description string, data map[string]any) map[string]any {
		return map[string]any{"description": description, "data": data}
	}
	export := map[string]any{
		"identity": map[string]any{"save_name": "abc-123", "game_id": "factorio"},
		"summary":  "Factorio — 16.7 hours, 3 rockets launched",
		"sections": map[string]any{
			"game_overview":   section("Map identity and high-level game state", map[string]any{"hours_played": 16.7, "rocket_launches": 3, "save_id": "abc-123"}),
			"production_flow": section("Per-item and per-fluid production and consumption rates", map[string]any{"items": map[string]any{"iron-plate": map[string]any{"produced": 1200, "consumed": 900}}, "fluids": map[string]any{"water": map[string]any{"produced": 5000}}}),
			"research":        section("Current research, queue, completed technologies, and infinite research levels", map[string]any{"current": "automation-3", "queue": []string{"logistics-3"}, "completed": []string{"automation", "logistics"}}),
			"power":           section("Per-surface power generation, consumption, and satisfaction", map[string]any{"surfaces": []any{map[string]any{"name": "nauvis", "generation": 45000000, "consumption": 37000000}}}),
			"machines":        section("Active machines grouped by recipe with module tallies", map[string]any{"recipes": map[string]any{"electronic-circuit": map[string]any{"count": 48, "modules": map[string]any{"speed-module-3": 96}}}}),
			"resources":       section("Resource patches with mining drills and remaining amounts", map[string]any{"patches": []any{map[string]any{"resource": "iron-ore", "amount": 2500000, "drills": 32}}}),
			"fluids":          section("Oil processing setup, fluid tank levels, and fluid-specific production data", map[string]any{"tanks": map[string]any{"petroleum-gas": 200000}, "refineries": 12}),
			"logistics":       section("Per-surface roboport coverage, bot counts, and logistics network state", map[string]any{"networks": []any{map[string]any{"surface": "nauvis", "logistic_bots": 500, "construction_bots": 200}}}),
			"trains":          section("Train list with composition, schedule, cargo, and fuel; station list", map[string]any{"trains": []any{map[string]any{"id": 17, "schedule": []string{"Iron Mine", "Smelter"}}}, "stations": []string{"Iron Mine", "Smelter"}}),
			"defenses":        section("Evolution factor, turret counts, wall count, and nearby enemy bases", map[string]any{"evolution": 0.72, "turrets": map[string]any{"laser-turret": 180}, "walls": 2400}),
			"inventory":       section("Player inventory, equipment, crafting queue, and position", map[string]any{"items": map[string]any{"construction-robot": 50}, "position": map[string]any{"x": 12.5, "y": -8.0}}),
			"alerts":          section("Active game alerts — no fuel, no power, no storage, under attack", map[string]any{"no_power": 2, "under_attack": 1}),
		},
	}
	raw, err := json.Marshal(export)
	if err != nil {
		t.Fatalf("marshal synthetic full export: %v", err)
	}
	return string(raw)
}

const RESULT_UNIT_BYTES = 10_240

func assertSectionSizes(t *testing.T, game string, sections map[string]any) {
	t.Helper()
	exceptions := loadSectionSizeExceptions(t)
	for name, raw := range sections {
		section := raw.(map[string]any)
		encoded, err := json.Marshal(section["data"])
		if err != nil {
			t.Fatalf("marshal %s/%s data: %v", game, name, err)
		}
		if exceptions[game+"/"+name] {
			t.Logf("section-size exception %s/%s: %d bytes", game, name, len(encoded))
		} else if len(encoded) > RESULT_UNIT_BYTES {
			t.Errorf("%s/%s section data = %d bytes, exceeds RESULT_UNIT_BYTES = %d", game, name, len(encoded), RESULT_UNIT_BYTES)
		}
	}
}

func loadSectionSizeExceptions(t *testing.T) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile("../../../scripts/section-size-exceptions.json")
	if err != nil {
		t.Fatalf("read section-size exceptions: %v", err)
	}
	got, err := decodeSectionSizeExceptions(raw)
	if err != nil {
		t.Fatalf("parse section-size exceptions: %v", err)
	}
	return got
}

func decodeSectionSizeExceptions(raw []byte) (map[string]bool, error) {
	type exception struct {
		Game    string `json:"game"`
		Section string `json:"section"`
	}
	var entries []exception
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&entries); err != nil {
		return nil, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("unexpected data after exception list")
	}

	want := map[string]bool{
		"stellaris/species":             true,
		"satisfactory/production_lines": true,
	}
	got := make(map[string]bool, len(entries))
	for _, entry := range entries {
		got[entry.Game+"/"+entry.Section] = true
	}
	if len(entries) != len(want) || len(got) != len(want) {
		return nil, fmt.Errorf("exception list must contain exactly %v", want)
	}
	for key := range want {
		if !got[key] {
			return nil, fmt.Errorf("exception list must contain exactly %v", want)
		}
	}
	return got, nil
}

func TestDecodeSectionSizeExceptionsRejectsNonClosedSets(t *testing.T) {
	for name, input := range map[string]string{
		"missing entry": `[{"game":"stellaris","section":"species"}]`,
		"extra entry":   `[{"game":"stellaris","section":"species"},{"game":"satisfactory","section":"production_lines"},{"game":"factorio","section":"power"}]`,
		"unknown field": `[{"game":"stellaris","section":"species","reason":"large"},{"game":"satisfactory","section":"production_lines"}]`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeSectionSizeExceptions([]byte(input)); err == nil {
				t.Fatal("expected exception list rejection")
			}
		})
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
