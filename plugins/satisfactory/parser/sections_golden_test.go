package main

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"testing"

	"github.com/joshsymonds/savecraft.gg/plugins/satisfactory/sav"
)

// parseFixtureSections runs the full pipeline (Open -> Extract -> sections)
// exactly as main() does.
func parseFixtureSections(t *testing.T, name string) *saveState {
	t.Helper()
	f, err := os.Open("testdata/" + name)
	if errors.Is(err, fs.ErrNotExist) {
		t.Skipf("%s not present (gitignored fixture)", name)
	}
	if err != nil {
		t.Fatalf("open %s: %v", name, err)
	}
	defer f.Close()

	header, body, err := sav.Open(f)
	if err != nil {
		t.Fatalf("sav.Open(%s): %v", name, err)
	}
	state := newSaveState(header)
	if err := sav.Extract(header, body, state.want, state.collect); err != nil {
		t.Fatalf("Extract(%s): %v", name, err)
	}
	return state
}

// Ground truth probed from the fixtures: despite the name, early_game.sav is
// a creative-mode tier-9 save (space elevator built, active milestone 8-2).
func TestGoldenSectionsEarlyGame(t *testing.T) {
	state := parseFixtureSections(t, "early_game.sav")

	prog := state.buildProgressionSection()
	if prog["currentTier"] != 9 {
		t.Errorf("currentTier = %v, want 9", prog["currentTier"])
	}
	if prog["spaceElevatorBuilt"] != true {
		t.Errorf("spaceElevatorBuilt = %v, want true", prog["spaceElevatorBuilt"])
	}
	if prog["spaceElevatorPhase"] != 4 {
		t.Errorf("spaceElevatorPhase = %v, want 4", prog["spaceElevatorPhase"])
	}
	if prog["activeMilestone"] != "8-2" {
		t.Errorf("activeMilestone = %v, want 8-2", prog["activeMilestone"])
	}
	if prog["mamResearchCompleted"] != 37 {
		t.Errorf("mamResearchCompleted = %v, want 37", prog["mamResearchCompleted"])
	}
	if prog["shopPurchases"] != 47 {
		t.Errorf("shopPurchases = %v, want 47", prog["shopPurchases"])
	}

	player := state.buildPlayerSection()
	inv, _ := player["inventory"].(map[string]any)
	items, _ := inv["items"].([]map[string]any)
	if len(items) != 37 {
		t.Errorf("inventory items = %d, want 37", len(items))
	}
	equipment, _ := player["equipment"].(map[string]any)
	if equipment["BodySlot"] != "Hazmat Suit" {
		t.Errorf("BodySlot = %v, want Hazmat Suit", equipment["BodySlot"])
	}

	summary := state.buildSummary()
	if summary != "Release, Tier 9, 6.0 hours played (creative)" {
		t.Errorf("summary = %q", summary)
	}
}

func TestGoldenSectionsCurrent12(t *testing.T) {
	state := parseFixtureSections(t, "current_1_2.sav")

	prog := state.buildProgressionSection()
	if prog["currentTier"] != 6 {
		t.Errorf("currentTier = %v, want 6", prog["currentTier"])
	}
	if prog["spaceElevatorPhase"] != 3 {
		t.Errorf("spaceElevatorPhase = %v, want 3", prog["spaceElevatorPhase"])
	}
	alts, _ := prog["alternateRecipes"].(map[string]any)
	if alts["count"] != 68 {
		t.Errorf("alternate recipes = %v, want 68", alts["count"])
	}
	if prog["mamResearchCompleted"] != 108 {
		t.Errorf("mamResearchCompleted = %v, want 108", prog["mamResearchCompleted"])
	}

	player := state.buildPlayerSection()
	inv, _ := player["inventory"].(map[string]any)
	items, _ := inv["items"].([]map[string]any)
	if len(items) != 30 {
		t.Errorf("inventory items = %d, want 30", len(items))
	}
}

// The whole result line must stay under the daemon's 2MB cap even for a
// megafactory.
func TestGoldenResultSizeMegafactory(t *testing.T) {
	state := parseFixtureSections(t, "megafactory.sav")

	encoded, err := json.Marshal(state.buildResult())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	const sizeCap = 2 << 20
	if len(encoded) >= sizeCap {
		t.Errorf("result line = %d bytes, must stay under %d", len(encoded), sizeCap)
	}
	t.Logf("megafactory result line: %dKB", len(encoded)>>10)
}
