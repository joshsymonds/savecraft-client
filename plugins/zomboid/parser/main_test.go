package main

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/joshsymonds/savecraft-client/internal/runner"
)

var nativeParser struct {
	sync.Once
	path   string
	err    error
	output []byte
}

func buildNativeParser(t *testing.T) string {
	t.Helper()
	nativeParser.Do(func() {
		directory, err := os.MkdirTemp("", "zomboid-parser-")
		if err != nil {
			nativeParser.err = err
			return
		}
		nativeParser.path = filepath.Join(directory, "parser")
		command := exec.Command("go", "build", "-o", nativeParser.path, ".")
		nativeParser.output, nativeParser.err = command.CombinedOutput()
	})
	if nativeParser.err != nil {
		t.Fatalf("build parser: %v\n%s", nativeParser.err, nativeParser.output)
	}
	return nativeParser.path
}

func readTestdata(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func fixtureMembers(t *testing.T, database string) map[string][]byte {
	t.Helper()
	return map[string][]byte{"players.db": readTestdata(t, database), "WorldDictionaryReadable.lua": readTestdata(t, "tutorial-42.20.2/WorldDictionaryReadable.lua"), "mods.txt": readTestdata(t, "tutorial-42.20.2/mods.txt")}
}

func makeTar(t *testing.T, members map[string][]byte) []byte {
	t.Helper()
	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	for _, name := range []string{"players.db", "players.db-journal", "WorldDictionaryReadable.lua", "mods.txt"} {
		data, ok := members[name]
		if !ok {
			continue
		}
		if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(data))}); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return archive.Bytes()
}

func runParser(t *testing.T, input []byte, arguments ...string) ([]byte, int) {
	t.Helper()
	command := exec.Command(buildNativeParser(t), arguments...)
	command.Stdin = bytes.NewReader(input)
	var output bytes.Buffer
	command.Stdout = &output
	err := command.Run()
	if err == nil {
		return output.Bytes(), 0
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatal(err)
	}
	return output.Bytes(), exitErr.ExitCode()
}

func line(t *testing.T, output []byte, kind string) map[string]any {
	t.Helper()
	for _, raw := range bytes.Split(output, []byte("\n")) {
		var parsed map[string]any
		if json.Unmarshal(raw, &parsed) == nil && parsed["type"] == kind {
			return parsed
		}
	}
	t.Fatalf("missing %s line in %s", kind, output)
	return nil
}

func requireEqual(t *testing.T, got, want any) {
	t.Helper()
	if got != want {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}
func object(t *testing.T, value any) map[string]any {
	t.Helper()
	result, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("got %T, want object", value)
	}
	return result
}
func array(t *testing.T, value any) []any {
	t.Helper()
	result, ok := value.([]any)
	if !ok {
		t.Fatalf("got %T, want array", value)
	}
	return result
}

func TestGoldenCharacterSectionAndDeterminism(t *testing.T) {
	input := makeTar(t, fixtureMembers(t, "tutorial-42.20.2/players.db"))
	first, code := runParser(t, input, "tutorial-save")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, first)
	}
	if len(line(t, first, "result")) == 0 {
		t.Fatal("empty result")
	}
	second, code := runParser(t, input, "tutorial-save")
	if code != 0 || !bytes.Equal(first, second) {
		t.Fatalf("parser output is not deterministic: exit=%d", code)
	}
	result := line(t, first, "result")
	if len(mustJSON(t, result)) >= 2<<20 {
		t.Fatal("result line exceeds 2 MiB")
	}
	identity := object(t, result["identity"])
	requireEqual(t, identity["saveName"], "tutorial-save")
	requireEqual(t, identity["gameId"], "zomboid")
	requireEqual(t, identity["displayName"], "Jane Doe")
	character := object(t, object(t, object(t, result["sections"])["character"])["data"])
	for key, want := range map[string]any{"localPlayerId": float64(1), "forename": "Jane", "surname": "Doe", "profession": "base:unemployed", "female": true, "alive": true, "hoursSurvived": 0.07402128669514241, "zombieKills": float64(2), "survivorKills": float64(0)} {
		requireEqual(t, character[key], want)
	}
	health := object(t, character["health"])
	requireEqual(t, health["minBodyPartHealth"], 99.29702758789062)
	requireEqual(t, health["injuredBodyParts"], float64(1))
	requireEqual(t, health["infected"], false)
	position := object(t, character["position"])
	for key, want := range map[string]any{"x": 178.56251525878906, "y": 147.0225372314453, "z": float64(0), "cellX": float64(22), "cellY": float64(18)} {
		requireEqual(t, position[key], want)
	}
	if len(array(t, character["traits"])) != 0 || len(array(t, character["activeMods"])) != 0 || len(array(t, character["otherCharacters"])) != 0 {
		t.Fatal("expected empty traits, activeMods, and otherCharacters")
	}
	perks := make(map[string]map[string]any)
	for _, value := range array(t, character["perks"]) {
		perk := object(t, value)
		perks[perk["name"].(string)] = perk
	}
	expectedPerks := map[string]struct{ level, xp float64 }{"Strength": {5, 37507}, "Fitness": {5, 37503}, "Aiming": {8, 0}, "SmallBlade": {0, 0.24347273}, "Maintenance": {0, 0.33333334}, "Doctor": {0, 1.25}, "SmallBlunt": {0, 0.795}, "Nimble": {0, 0.25}}
	requireEqual(t, len(perks), len(expectedPerks))
	for name, expected := range expectedPerks {
		requireEqual(t, perks[name]["level"], expected.level)
		requireEqual(t, perks[name]["xp"], expected.xp)
	}
	boosts := array(t, character["xpBoosts"])
	requireEqual(t, len(boosts), 2)
	requireEqual(t, object(t, boosts[0])["perk"], "Fitness")
	requireEqual(t, object(t, boosts[0])["level"], float64(3))
	requireEqual(t, object(t, boosts[1])["perk"], "Strength")
	nutrition := object(t, character["nutrition"])
	for key, want := range map[string]any{"calories": 1116.2328, "proteins": 39.75924, "lipids": 15.695856, "carbohydrates": -0.9328311, "weight": 80.000755} {
		requireEqual(t, nutrition[key], want)
	}
	wornItems := array(t, character["wornItems"])
	requireEqual(t, len(wornItems), 7)
	expectedWorn := map[string]string{"base:back": "Base.Bag_ALICEpack", "base:bandage": "Base.Bandage_LeftHand", "base:shoes": "Base.Shoes_BlueTrainers", "base:shortsshort": "Base.Shorts_ShortDenim", "base:socks": "Base.Socks_Ankle", "base:tshirt": "Base.Tshirt_DefaultTEXTURE_TINT"}
	for _, value := range wornItems {
		item := object(t, value)
		if want, ok := expectedWorn[item["bodyLocation"].(string)]; ok {
			requireEqual(t, item["fulltype"], want)
		}
	}
	carriedItems := array(t, character["carriedItems"])
	requireEqual(t, len(carriedItems), 10)
	counts := make(map[string]float64)
	for _, value := range carriedItems {
		item := object(t, value)
		counts[item["fulltype"].(string)] = item["count"].(float64)
	}
	requireEqual(t, counts["Base.Bandage_LeftHand"], float64(2))
	for _, fulltype := range []string{"Base.Pan", "Base.Tshirt_DefaultTEXTURE_TINT", "Base.EmptyJar", "Base.Shorts_ShortDenim", "Base.Bag_ALICEpack", "Base.Shotgun", "Base.Socks_Ankle", "Base.HuntingKnife", "Base.Shoes_BlueTrainers"} {
		requireEqual(t, counts[fulltype], float64(1))
	}
}

func TestErrorOutputs(t *testing.T) {
	missingPlayers := fixtureMembers(t, "tutorial-42.20.2/players.db")
	delete(missingPlayers, "players.db")
	missingDictionary := fixtureMembers(t, "tutorial-42.20.2/players.db")
	delete(missingDictionary, "WorldDictionaryReadable.lua")
	journal := fixtureMembers(t, "tutorial-42.20.2/players.db")
	journal["players.db-journal"] = []byte("in progress")
	truncated := fixtureMembers(t, "tutorial-42.20.2/players.db")
	truncated["players.db"] = truncated["players.db"][:12000]
	missingShotgun := fixtureMembers(t, "tutorial-42.20.2/players.db")
	missingShotgun["WorldDictionaryReadable.lua"] = removeShotgunRecord(t, missingShotgun["WorldDictionaryReadable.lua"])
	cases := []struct {
		name                string
		input               []byte
		errorType, contains string
	}{{"old version", makeTar(t, fixtureMembers(t, "players-v245.db")), "unsupported_version", "245"}, {"empty", makeTar(t, fixtureMembers(t, "players-empty.db")), "parse_error", "localPlayers has no rows"}, {"missing players", makeTar(t, missingPlayers), "corrupt_file", "missing member players.db"}, {"missing dictionary", makeTar(t, missingDictionary), "corrupt_file", "missing member WorldDictionaryReadable.lua"}, {"journal", makeTar(t, journal), "corrupt_file", "players.db-journal is non-empty"}, {"truncated", makeTar(t, truncated), "corrupt_file", "sqlite corrupt"}, {"missing registry", makeTar(t, missingShotgun), "corrupt_file", "registry id 3651"}}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			output, code := runParser(t, test.input)
			if code == 0 {
				t.Fatal("expected non-zero exit")
			}
			errorLine := line(t, output, "error")
			requireEqual(t, errorLine["errorType"], test.errorType)
			message := errorLine["message"].(string)
			if !strings.Contains(message, test.contains) {
				t.Fatalf("message %q does not contain %q", message, test.contains)
			}
			if test.name == "old version" && !strings.Contains(message, "249") {
				t.Fatalf("message %q does not contain 249", message)
			}
		})
	}
}

func TestOtherCharacters(t *testing.T) {
	output, code := runParser(t, makeTar(t, fixtureMembers(t, "players-two-rows.db")))
	if code != 0 {
		t.Fatal(string(output))
	}
	character := object(t, object(t, object(t, line(t, output, "result")["sections"])["character"])["data"])
	others := array(t, character["otherCharacters"])
	requireEqual(t, len(others), 1)
	other := object(t, others[0])
	for key, want := range map[string]any{"localPlayerId": float64(2), "name": "Old Jane", "alive": false, "hoursSurvived": 0.07402128669514241} {
		requireEqual(t, other[key], want)
	}
}

func TestWazeroEndToEnd(t *testing.T) {
	context := context.Background()
	wazero, err := runner.NewWazeroRunner(context)
	if err != nil {
		t.Fatal(err)
	}
	defer wazero.Close(context)
	wasmPath := filepath.Join(t.TempDir(), "parser.wasm")
	command := exec.Command("go", "build", "-o", wasmPath, ".")
	command.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build wasm: %v\n%s", err, output)
	}
	wasm, err := os.ReadFile(wasmPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := wazero.LoadPlugin(context, "zomboid", wasm, nil); err != nil {
		t.Fatal(err)
	}
	state, err := wazero.Run(context, "zomboid", "wazero-save", makeTar(t, fixtureMembers(t, "tutorial-42.20.2/players.db")), nil)
	if err != nil {
		t.Fatal(err)
	}
	requireEqual(t, state.Identity.SaveName, "wazero-save")
	requireEqual(t, state.Identity.DisplayName, "Jane Doe")
	if _, ok := state.Sections["character"]; !ok {
		t.Fatal("missing character section")
	}
}

func removeShotgunRecord(t *testing.T, source []byte) []byte {
	t.Helper()
	text := string(source)
	registry := strings.Index(text, "registryID = 3651")
	if registry < 0 {
		t.Fatal("Shotgun record missing")
	}
	start := strings.LastIndex(text[:registry], "{")
	end := strings.Index(text[registry:], "}")
	if start < 0 || end < 0 {
		t.Fatal("malformed Shotgun record")
	}
	return []byte(text[:start] + text[registry+end+1:])
}
func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
