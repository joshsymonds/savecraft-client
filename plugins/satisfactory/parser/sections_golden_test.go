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
	if prog["activeMilestone"] != "Advanced Aluminum Production" {
		t.Errorf("activeMilestone = %v, want Advanced Aluminum Production", prog["activeMilestone"])
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

	// This session was created with non-default 1.2 Game Modes (probed).
	gm, _ := state.buildOverviewSection()["gameMode"].(map[string]any)
	if gm == nil {
		t.Fatal("gameMode missing from current_1_2 overview")
	}
	if gm["partsCostMultiplier"] != 0.75 || gm["energyCostMultiplier"] != 0.5 ||
		gm["spacePartsCostMultiplier"] != 0.75 {
		t.Errorf("multipliers = %v", gm)
	}
	if gm["nodeRandomization"] != "NRM_Strict" || gm["nodePurity"] != "NPS_Increase" {
		t.Errorf("node settings = %v", gm)
	}
	if gm["nodeRandomizationSeed"] != int64(1231861653) {
		t.Errorf("seed = %v (%T)", gm["nodeRandomizationSeed"], gm["nodeRandomizationSeed"])
	}
	if gm["cheatNoPower"] != true || gm["cheatNoFuel"] != true {
		t.Errorf("cheats = %v", gm)
	}
	if gm["startingTier"] != int64(6) || gm["noUnlockCost"] != true ||
		gm["unlockInstantAltRecipes"] != true {
		t.Errorf("game rules = %v", gm)
	}
}

// Megafactory factory sections, pinned from a probed run: 3,622
// manufacturers across 146 recipe/clock groups, 621 extractors, 81 power
// circuits, 384 coal generators.
func TestGoldenFactorySectionsMegafactory(t *testing.T) {
	state := parseFixtureSections(t, "megafactory.sav")

	// Vanilla session: no Game Mode properties serialized, no gameMode key.
	if _, ok := state.buildOverviewSection()["gameMode"]; ok {
		t.Error("megafactory (vanilla) should have no gameMode key")
	}

	machines := state.buildMachinesSection()
	if machines["totalManufacturers"] != 3622 {
		t.Errorf("totalManufacturers = %v, want 3622", machines["totalManufacturers"])
	}
	if machines["totalExtractors"] != 621 {
		t.Errorf("totalExtractors = %v, want 621", machines["totalExtractors"])
	}

	prod := state.buildProductionSection()
	recipes, _ := prod["byRecipe"].([]map[string]any)
	if len(recipes) != 96 {
		t.Errorf("byRecipe = %d entries, want 96", len(recipes))
	}
	if recipes[0]["recipe"] != "Pure Aluminum Ingot" || recipes[0]["machines"] != 257 {
		t.Errorf("top recipe = %v %v, want Pure Aluminum Ingot x257", recipes[0]["recipe"], recipes[0]["machines"])
	}

	power := state.buildPowerSection()
	if power["circuits"] != 81 {
		t.Errorf("circuits = %v, want 81", power["circuits"])
	}
	generators, _ := power["generators"].([]map[string]any)
	if len(generators) == 0 || generators[0]["building"] != "Generator Coal" || generators[0]["count"] != 384 {
		t.Errorf("top generators = %v, want 384 coal", generators[0])
	}
	// Measured productivity comes from the save's rolling window — sanity
	// band, not exact, in case of float drift across architectures.
	if pct, ok := generators[0]["measuredProductivityPct"].(float64); !ok || pct < 80 || pct > 90 {
		t.Errorf("coal productivity = %v, want ~83", generators[0]["measuredProductivityPct"])
	}
}

// Storage/logistics/resource_nodes pinned from a probed megafactory run:
// 452 containers, 14.6M nitrogen gas in storage, 96 depot items, 64 trains
// with player-named stations decoded from TextProperties.
func TestGoldenLogisticsSectionsMegafactory(t *testing.T) {
	state := parseFixtureSections(t, "megafactory.sav")

	storage := state.buildStorageSection()
	containers, _ := storage["containers"].(map[string]int)
	if containers["Storage Container Mk2"] != 396 || containers["Storage Container Mk1"] != 56 {
		t.Errorf("containers = %v, want 396 Mk2 + 56 Mk1", containers)
	}
	items, _ := storage["itemsInStorage"].([]map[string]any)
	if len(items) != 93 {
		t.Errorf("itemsInStorage = %d entries, want 93", len(items))
	}
	if len(items) > 0 && (items[0]["name"] != "Nitrogen Gas" || items[0]["count"] != int64(14604451)) {
		t.Errorf("top stored item = %v %v, want Nitrogen Gas x14604451", items[0]["name"], items[0]["count"])
	}
	depot, _ := storage["dimensionalDepot"].(map[string]any)
	depotItems, _ := depot["items"].([]map[string]any)
	if len(depotItems) != 96 {
		t.Errorf("depot items = %d, want 96", len(depotItems))
	}

	logistics := state.buildLogisticsSection()
	trains, _ := logistics["trains"].(map[string]any)
	if trains["trains"] != 64 || trains["locomotives"] != 91 || trains["freightWagons"] != 259 {
		t.Errorf("trains = %v, want 64/91/259", trains)
	}
	if trains["stations"] != 82 || trains["timetables"] != 64 {
		t.Errorf("stations = %v timetables = %v, want 82/64", trains["stations"], trains["timetables"])
	}
	stationNames, _ := trains["stationNames"].([]string)
	if len(stationNames) != 82 || stationNames[0] != "Air-Liquide Eneco Exp" {
		t.Errorf("stationNames = %d first %q, want 82 starting with Air-Liquide Eneco Exp",
			len(stationNames), first(stationNames))
	}
	drones, _ := logistics["drones"].(map[string]any)
	droneNames, _ := drones["stationNames"].([]string)
	if drones["drones"] != 12 || len(droneNames) != 13 {
		t.Errorf("drones = %v with %d stations, want 12/13", drones["drones"], len(droneNames))
	}

	// 621 extractors occupy 176 distinct sources: 451 water pumps share
	// water volumes, miners sit one-per-node.
	nodes := state.buildResourceNodesSection()
	if nodes["occupiedNodes"] != 176 {
		t.Errorf("occupiedNodes = %v, want 176", nodes["occupiedNodes"])
	}
}

func first(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[0]
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

// current_sv60.sav is a real save from a later 1.2 patch (SaveVersion 60,
// build 493833) — newer than the version ceiling shipped in v1.0.0, which
// rejected it. It must parse cleanly: all sections populated, header
// reported as sv60. Tier 3 early-game factory (probed from the real save).
func TestGoldenSectionsSv60(t *testing.T) {
	state := parseFixtureSections(t, "current_sv60.sav")

	overview := state.buildOverviewSection()
	if overview["saveVersion"] != int32(60) {
		t.Errorf("saveVersion = %v, want 60", overview["saveVersion"])
	}
	if overview["gameBuild"] != int32(493833) {
		t.Errorf("gameBuild = %v, want 493833", overview["gameBuild"])
	}

	prog := state.buildProgressionSection()
	if prog["currentTier"] != 3 {
		t.Errorf("currentTier = %v, want 3", prog["currentTier"])
	}
	if prog["milestonesPurchased"] != 9 {
		t.Errorf("milestonesPurchased = %v, want 9", prog["milestonesPurchased"])
	}

	// The factory has real machines, power, and stored items — a parse that
	// silently produced empty sections (the sv60-misparse failure mode) would
	// trip these.
	power := state.buildPowerSection()
	if power["totalGenerators"] != 12 {
		t.Errorf("totalGenerators = %v, want 12", power["totalGenerators"])
	}
	storage := state.buildStorageSection()
	items, _ := storage["itemsInStorage"].([]map[string]any)
	var screwCount int64 = -1
	for _, it := range items {
		if it["name"] == "Screws" {
			screwCount, _ = it["count"].(int64)
		}
	}
	if screwCount != 7039 {
		t.Errorf("Screws count in storage = %d, want 7039", screwCount)
	}
}
