package main

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/joshsymonds/savecraft-client/internal/runner"
	"github.com/joshsymonds/savecraft-client/plugins/zomboid/parser/blob"
	"github.com/joshsymonds/savecraft-client/plugins/zomboid/parser/dict"
)

// buildParser compiles the parser for the host so a test can drive it the way
// the daemon does, over stdin and stdout.
func buildParser(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "parser")
	output, err := exec.Command("go", "build", "-o", path, ".").CombinedOutput()
	if err != nil {
		t.Fatalf("build parser: %v\n%s", err, output)
	}
	return path
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
	return map[string][]byte{
		"players.db":                  readTestdata(t, database),
		"WorldDictionaryReadable.lua": readTestdata(t, "tutorial-42.20.2/WorldDictionaryReadable.lua"),
		"mods.txt":                    readTestdata(t, "tutorial-42.20.2/mods.txt"),
	}
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

func runParser(t *testing.T, parser string, input []byte, arguments ...string) ([]byte, int) {
	t.Helper()
	command := exec.Command(parser, arguments...)
	command.Stdin = bytes.NewReader(input)
	var output bytes.Buffer
	command.Stdout = &output
	err := command.Run()
	if err == nil {
		return output.Bytes(), 0
	}
	return output.Bytes(), exitCode(t, err)
}

// exitCode reports the status a finished parser run exited with.
func exitCode(t *testing.T, err error) int {
	t.Helper()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatal(err)
	}
	return exitErr.ExitCode()
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
func text(t *testing.T, value any) string {
	t.Helper()
	result, ok := value.(string)
	if !ok {
		t.Fatalf("got %T, want string", value)
	}
	return result
}
func number(t *testing.T, value any) float64 {
	t.Helper()
	result, ok := value.(float64)
	if !ok {
		t.Fatalf("got %T, want number", value)
	}
	return result
}

func TestGoldenCharacterSectionAndDeterminism(t *testing.T) {
	parser := buildParser(t)
	input := makeTar(t, fixtureMembers(t, "tutorial-42.20.2/players.db"))
	first, code := runParser(t, parser, input, "tutorial-save")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, first)
	}
	if len(line(t, first, "result")) == 0 {
		t.Fatal("empty result")
	}
	second, code := runParser(t, parser, input, "tutorial-save")
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
	if len(array(t, character["traits"])) != 0 || len(array(t, character["activeMods"])) != 0 ||
		len(array(t, character["otherCharacters"])) != 0 {
		t.Fatal("expected empty traits, activeMods, and otherCharacters")
	}
	perks := make(map[string]map[string]any)
	for _, value := range array(t, character["perks"]) {
		perk := object(t, value)
		perks[text(t, perk["name"])] = perk
	}
	expectedPerks := map[string]struct{ level, xp float64 }{
		"Strength":    {5, 37507},
		"Fitness":     {5, 37503},
		"Aiming":      {8, 0},
		"SmallBlade":  {0, 0.24347273},
		"Maintenance": {0, 0.33333334},
		"Doctor":      {0, 1.25},
		"SmallBlunt":  {0, 0.795},
		"Nimble":      {0, 0.25},
	}
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
	// The whole worn list, in emitted order. The fixture wears the bandage
	// twice, so a lenient per-location check would not notice either copy going
	// missing.
	wantWorn := [][2]string{
		{"base:back", "Base.Bag_ALICEpack"},
		{"base:bandage", "Base.Bandage_LeftHand"},
		{"base:bandage", "Base.Bandage_LeftHand"},
		{"base:shoes", "Base.Shoes_BlueTrainers"},
		{"base:shortsshort", "Base.Shorts_ShortDenim"},
		{"base:socks", "Base.Socks_Ankle"},
		{"base:tshirt", "Base.Tshirt_DefaultTEXTURE_TINT"},
	}
	gotWorn := make([][2]string, 0, len(wantWorn))
	for _, value := range array(t, character["wornItems"]) {
		item := object(t, value)
		gotWorn = append(gotWorn, [2]string{text(t, item["bodyLocation"]), text(t, item["fulltype"])})
	}
	if !reflect.DeepEqual(gotWorn, wantWorn) {
		t.Fatalf("worn items %v, want %v", gotWorn, wantWorn)
	}
	carriedItems := array(t, character["carriedItems"])
	requireEqual(t, len(carriedItems), 10)
	counts := make(map[string]float64)
	for _, value := range carriedItems {
		item := object(t, value)
		counts[text(t, item["fulltype"])] = number(t, item["count"])
	}
	requireEqual(t, counts["Base.Bandage_LeftHand"], float64(2))
	for _, fulltype := range []string{"Base.Pan", "Base.Tshirt_DefaultTEXTURE_TINT", "Base.EmptyJar", "Base.Shorts_ShortDenim", "Base.Bag_ALICEpack", "Base.Shotgun", "Base.Socks_Ankle", "Base.HuntingKnife", "Base.Shoes_BlueTrainers"} {
		requireEqual(t, counts[fulltype], float64(1))
	}
}

func TestErrorOutputs(t *testing.T) {
	parser := buildParser(t)
	missingPlayers := fixtureMembers(t, "tutorial-42.20.2/players.db")
	delete(missingPlayers, "players.db")
	missingDictionary := fixtureMembers(t, "tutorial-42.20.2/players.db")
	delete(missingDictionary, "WorldDictionaryReadable.lua")
	journal := fixtureMembers(t, "tutorial-42.20.2/players.db")
	journal["players.db-journal"] = []byte("in progress")
	truncated := fixtureMembers(t, "tutorial-42.20.2/players.db")
	truncated["players.db"] = truncated["players.db"][:12000]
	missingShotgun := fixtureMembers(t, "tutorial-42.20.2/players.db")
	missingShotgun["WorldDictionaryReadable.lua"] = removeShotgunRecord(
		t,
		missingShotgun["WorldDictionaryReadable.lua"],
	)
	truncatedDictionary := fixtureMembers(t, "tutorial-42.20.2/players.db")
	truncatedDictionary["WorldDictionaryReadable.lua"] = truncateLastRecord(
		t,
		truncatedDictionary["WorldDictionaryReadable.lua"],
	)
	nonFinitePosition := fixtureMembers(t, "tutorial-42.20.2/players.db")
	nonFinitePosition["players.db"] = nonFiniteX(t, nonFinitePosition["players.db"])
	cases := []struct {
		name                string
		input               []byte
		errorType, contains string
	}{{"old version", makeTar(t, fixtureMembers(t, "players-v245.db")), "unsupported_version", "245"}, {"mixed versions", makeTar(t, fixtureMembers(t, "players-mixed.db")), "unsupported_version", "row 2 has world version 245"}, {"no descriptor", makeTar(t, fixtureMembers(t, "players-no-descriptor.db")), "corrupt_file", "row 1: player has no descriptor"}, {"non-finite position", makeTar(t, nonFinitePosition), "corrupt_file", "non-finite column x"}, {"empty", makeTar(t, fixtureMembers(t, "players-empty.db")), "parse_error", "localPlayers has no rows"}, {"missing players", makeTar(t, missingPlayers), "corrupt_file", "missing member players.db"}, {"missing dictionary", makeTar(t, missingDictionary), "corrupt_file", "missing member WorldDictionaryReadable.lua"}, {"journal", makeTar(t, journal), "corrupt_file", "players.db-journal is non-empty"}, {"truncated", makeTar(t, truncated), "corrupt_file", "sqlite corrupt"}, {"missing registry", makeTar(t, missingShotgun), "corrupt_file", "registry id 3651"}, {"truncated dictionary", makeTar(t, truncatedDictionary), "corrupt_file", "malformed WorldDictionaryReadable.lua"}}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			output, code := runParser(t, parser, test.input)
			if code == 0 {
				t.Fatal("expected non-zero exit")
			}
			errorLine := line(t, output, "error")
			requireEqual(t, errorLine["errorType"], test.errorType)
			message := text(t, errorLine["message"])
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
	output, code := runParser(t, buildParser(t), makeTar(t, fixtureMembers(t, "players-two-rows.db")))
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
	state, err := wazero.Run(
		context,
		"zomboid",
		"wazero-save",
		makeTar(t, fixtureMembers(t, "tutorial-42.20.2/players.db")),
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	requireEqual(t, state.Identity.SaveName, "wazero-save")
	requireEqual(t, state.Identity.DisplayName, "Jane Doe")
	if _, ok := state.Sections["character"]; !ok {
		t.Fatal("missing character section")
	}
}

// TestActiveMods pins the two mods.txt shapes a save can have: a list, which is
// emitted sorted so the result does not inherit the game's write order, and no
// mods.txt at all, which is an empty JSON array rather than null.
func TestActiveMods(t *testing.T) {
	parser := buildParser(t)
	t.Run("sorted", func(t *testing.T) {
		members := fixtureMembers(t, "tutorial-42.20.2/players.db")
		members["mods.txt"] = []byte(
			"VERSION = 1,\n\nmods\n{\n  \"zebra\",\n  \"alpha\",\n  \"Mango\",\n}\n\nmaps\n{\n}\n",
		)
		output, code := runParser(t, parser, makeTar(t, members))
		if code != 0 {
			t.Fatal(string(output))
		}
		character := object(t, object(t, object(t, line(t, output, "result")["sections"])["character"])["data"])
		mods := array(t, character["activeMods"])
		got := make([]string, 0, len(mods))
		for _, value := range mods {
			got = append(got, text(t, value))
		}
		if want := []string{"Mango", "alpha", "zebra"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("active mods %v, want %v", got, want)
		}
	})
	t.Run("absent", func(t *testing.T) {
		members := fixtureMembers(t, "tutorial-42.20.2/players.db")
		delete(members, "mods.txt")
		output, code := runParser(t, parser, makeTar(t, members))
		if code != 0 {
			t.Fatal(string(output))
		}
		if !bytes.Contains(output, []byte(`"activeMods":[]`)) {
			t.Fatal("result does not carry an empty activeMods array")
		}
	})
}

// TestWriteToReportsFailures covers the two ways an ndjson line can be lost:
// the encoder refusing a value, and stdout refusing the bytes. Both used to be
// discarded, which turned a failed run into a silent success.
func TestWriteToReportsFailures(t *testing.T) {
	notFinite := output{
		Type:     "result",
		Sections: &outputSections{Character: section{Data: character{HoursSurvived: math.NaN()}}},
	}
	if err := writeTo(io.Discard, notFinite); err == nil {
		t.Error("a NaN encoded without error")
	}
	if err := writeTo(errorWriter{}, output{Type: "status", Message: "Reading players.db…"}); err == nil {
		t.Error("a failing writer reported success")
	}
}

// TestUnwritableStdoutFailsTheRun is the end-to-end half of the same contract:
// with nowhere to put its lines the parser exits non-zero and says why on
// stderr instead of finishing as though it had reported a result.
func TestUnwritableStdoutFailsTheRun(t *testing.T) {
	readOnly, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer readOnly.Close()
	command := exec.Command(buildParser(t), "tutorial-save")
	command.Stdin = bytes.NewReader(makeTar(t, fixtureMembers(t, "tutorial-42.20.2/players.db")))
	command.Stdout = readOnly
	var stderr bytes.Buffer
	command.Stderr = &stderr
	err = command.Run()
	if err == nil {
		t.Fatalf("parser exited 0 with an unwritable stdout; stderr %q", stderr.String())
	}
	requireEqual(t, exitCode(t, err), 1)
	if !strings.Contains(stderr.String(), "parse_error") {
		t.Fatalf("stderr %q does not name the failure", stderr.String())
	}
}

// TestMakeCharacterRejectsNonFiniteFloats walks every float the character sheet
// carries out of the blob. JSON cannot represent NaN or an infinity, so one
// reaching the encoder would fail the run only after the status lines had
// promised a result.
func TestMakeCharacterRejectsNonFiniteFloats(t *testing.T) {
	items, err := dict.ParseItems(readTestdata(t, "tutorial-42.20.2/WorldDictionaryReadable.lua"))
	if err != nil {
		t.Fatal(err)
	}
	fixturePlayer := func() *blob.Player {
		player, decodeErr := blob.Decode(readTestdata(t, "jane-doe-249.blob"), 249)
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		return player
	}
	rows := func() (rawCharacter, []rawCharacter) {
		return rawCharacter{id: 1, name: "Jane Doe", player: fixturePlayer()},
			[]rawCharacter{{id: 2, name: "Old Jane", player: fixturePlayer()}}
	}
	primary, remaining := rows()
	if _, err = makeCharacter(primary, remaining, items, []string{}); err != nil {
		t.Fatalf("unmutated fixture: %v", err)
	}
	for name, mutate := range map[string]struct {
		contains string
		apply    func(primary rawCharacter, remaining []rawCharacter)
	}{
		"hours survived": {"row 1: non-finite hoursSurvived", func(p rawCharacter, _ []rawCharacter) { p.player.HoursSurvived = math.NaN() }},
		"calories":       {"row 1: non-finite nutrition.calories", func(p rawCharacter, _ []rawCharacter) { p.player.Nutrition.Calories = nan32() }},
		"proteins":       {"row 1: non-finite nutrition.proteins", func(p rawCharacter, _ []rawCharacter) { p.player.Nutrition.Proteins = nan32() }},
		"lipids":         {"row 1: non-finite nutrition.lipids", func(p rawCharacter, _ []rawCharacter) { p.player.Nutrition.Lipids = nan32() }},
		"carbohydrates":  {"row 1: non-finite nutrition.carbohydrates", func(p rawCharacter, _ []rawCharacter) { p.player.Nutrition.Carbohydrates = nan32() }},
		"weight":         {"row 1: non-finite nutrition.weight", func(p rawCharacter, _ []rawCharacter) { p.player.Nutrition.Weight = nan32() }},
		"body part health": {"row 1: non-finite health of body part 3", func(p rawCharacter, _ []rawCharacter) {
			p.player.BodyDamage.Parts[3].Health = float32(math.Inf(1))
		}},
		"perk xp": {"row 1: non-finite xp for perk", func(p rawCharacter, _ []rawCharacter) {
			p.player.XP.Entries[0].XP = float32(math.Inf(-1))
		}},
		"other character": {"row 2: non-finite hoursSurvived", func(_ rawCharacter, r []rawCharacter) {
			r[0].player.HoursSurvived = math.NaN()
		}},
	} {
		t.Run(name, func(t *testing.T) {
			primary, remaining := rows()
			mutate.apply(primary, remaining)
			_, err := makeCharacter(primary, remaining, items, []string{})
			if err == nil {
				t.Fatal("non-finite float accepted")
			}
			if !strings.Contains(err.Error(), mutate.contains) {
				t.Fatalf("error %q does not contain %q", err, mutate.contains)
			}
		})
	}
}

func nan32() float32 { return float32(math.NaN()) }

// errorWriter stands in for a stdout that will not take the parser's bytes.
type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) { return 0, os.ErrClosed }

// nonFiniteX rewrites the fixture's x column to a NaN, which the SQLite record
// format can store and JSON cannot represent.
func nonFiniteX(t *testing.T, database []byte) []byte {
	t.Helper()
	stored := binary.BigEndian.AppendUint64(nil, math.Float64bits(178.56251525878906))
	if count := bytes.Count(database, stored); count != 1 {
		t.Fatalf("the x column's bytes appear %d times, want 1", count)
	}
	return bytes.Replace(database, stored, binary.BigEndian.AppendUint64(nil, math.Float64bits(math.NaN())), 1)
}

// truncateLastRecord cuts the dictionary inside its final ITEMS record, the
// shape a half-written or clipped WorldDictionaryReadable.lua has.
func truncateLastRecord(t *testing.T, source []byte) []byte {
	t.Helper()
	const last = `fulltype = "Base.FishingHook"`
	cut := strings.Index(string(source), last)
	if cut < 0 {
		t.Fatal("FishingHook record missing")
	}
	return source[:cut+len(last)]
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
