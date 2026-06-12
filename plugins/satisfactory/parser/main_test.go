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

// testState builds a saveState with synthetic manager data, exercising the
// builders without a real save body.
func testState() *saveState {
	s := newSaveState(testHeader())
	s.playerCount = 1
	s.playerPosition = [3]float32{100, -200, 300}
	s.gameState = &sav.ObjectData{Properties: map[string]any{
		"mIsSpaceElevatorBuilt": true,
	}}
	schematicBase := "/Game/FactoryGame/Schematics"
	s.schematics = &sav.ObjectData{Properties: map[string]any{
		"mPurchasedSchematics": []any{
			sav.ObjectRef{Path: schematicBase + "/Progression/Schematic_1-1.Schematic_1-1_C"},
			sav.ObjectRef{Path: schematicBase + "/Progression/Schematic_5-2.Schematic_5-2_C"},
			sav.ObjectRef{Path: schematicBase + "/Research/Schematic_Caterium1.Schematic_Caterium1_C"},
			sav.ObjectRef{
				Path: schematicBase + "/Alternate/Schematic_Alternate_WetConcrete.Schematic_Alternate_WetConcrete_C",
			},
			sav.ObjectRef{Path: schematicBase + "/ResourceSink/Schematic_Sink_Coupon1.Schematic_Sink_Coupon1_C"},
		},
		"mActiveSchematic": sav.ObjectRef{Path: "/Game/X/Schematic_5-3.Schematic_5-3_C"},
	}}
	phasePath := "/Game/FactoryGame/GamePhases/GP_Project_Assembly_Phase_2.GP_Project_Assembly_Phase_2"
	s.gamePhase = &sav.ObjectData{Properties: map[string]any{
		"mCurrentGamePhase": sav.ObjectRef{Path: phasePath},
	}}
	s.unlocks = &sav.ObjectData{Properties: map[string]any{
		"mNumTotalInventorySlots": int64(48),
	}}
	s.playerInventory["inventory"] = &sav.ObjectData{Properties: map[string]any{
		"mInventoryStacks": []any{
			map[string]any{
				"Item":     sav.InventoryItem{ItemClass: "/Game/X/Desc_IronPlate.Desc_IronPlate_C"},
				"NumItems": int64(200),
			},
			map[string]any{"Item": sav.InventoryItem{}, "NumItems": int64(0)}, // empty slot
		},
	}}
	s.playerInventory["BodySlot"] = &sav.ObjectData{Properties: map[string]any{
		"mInventoryStacks": []any{
			map[string]any{
				"Item": sav.InventoryItem{
					ItemClass: "/Game/X/BP_EquipmentDescriptorHazmatSuit.BP_EquipmentDescriptorHazmatSuit_C",
				},
				"NumItems": int64(1),
			},
		},
	}}
	return s
}

func TestBuildResultIdentity(t *testing.T) {
	result := testState().buildResult()

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

func TestBuildSummaryIncludesTier(t *testing.T) {
	summary, _ := testState().buildResult()["summary"].(string)
	if !strings.Contains(summary, "MyFactory") {
		t.Errorf("summary %q should contain session name", summary)
	}
	if !strings.Contains(summary, "Tier 5") {
		t.Errorf("summary %q should contain Tier 5", summary)
	}
	if !strings.Contains(summary, "16.3") {
		t.Errorf("summary %q should contain playtime hours (16.3)", summary)
	}
}

func TestBuildProgressionSection(t *testing.T) {
	data := testState().buildProgressionSection()

	if data["currentTier"] != 5 {
		t.Errorf("currentTier = %v, want 5", data["currentTier"])
	}
	if data["milestonesPurchased"] != 2 {
		t.Errorf("milestonesPurchased = %v, want 2", data["milestonesPurchased"])
	}
	if data["mamResearchCompleted"] != 1 {
		t.Errorf("mamResearchCompleted = %v, want 1", data["mamResearchCompleted"])
	}
	if data["shopPurchases"] != 1 {
		t.Errorf("shopPurchases = %v, want 1", data["shopPurchases"])
	}
	if data["spaceElevatorPhase"] != 2 {
		t.Errorf("spaceElevatorPhase = %v, want 2", data["spaceElevatorPhase"])
	}
	if data["spaceElevatorBuilt"] != true {
		t.Errorf("spaceElevatorBuilt = %v, want true", data["spaceElevatorBuilt"])
	}
	if data["activeMilestone"] != "5-3" {
		t.Errorf("activeMilestone = %v, want 5-3", data["activeMilestone"])
	}
	alts, _ := data["alternateRecipes"].(map[string]any)
	names, _ := alts["names"].([]string)
	if len(names) != 1 || names[0] != "Alternate Wet Concrete" {
		t.Errorf("alternateRecipes = %v", alts)
	}
}

func TestBuildPlayerSection(t *testing.T) {
	data := testState().buildPlayerSection()

	inv, _ := data["inventory"].(map[string]any)
	items, _ := inv["items"].([]map[string]any)
	if len(items) != 1 {
		t.Fatalf("items = %v, want 1 (empty slots excluded)", items)
	}
	if items[0]["name"] != "Iron Plate" || items[0]["count"] != int64(200) {
		t.Errorf("item = %v", items[0])
	}
	equipment, _ := data["equipment"].(map[string]any)
	if equipment["BodySlot"] != "Hazmat Suit" {
		t.Errorf("equipment = %v", equipment)
	}
	if data["totalInventorySlots"] != int64(48) {
		t.Errorf("totalInventorySlots = %v", data["totalInventorySlots"])
	}
	if data["playerCount"] != 1 {
		t.Errorf("playerCount = %v", data["playerCount"])
	}
}

func TestBuildOverviewSection(t *testing.T) {
	data := testState().buildOverviewSection()

	if data["sessionName"] != "MyFactory" {
		t.Errorf("sessionName = %v", data["sessionName"])
	}
	if data["playTimeSeconds"] != int32(58723) {
		t.Errorf("playTimeSeconds = %v (%T)", data["playTimeSeconds"], data["playTimeSeconds"])
	}
	if data["savedAt"] != "2026-01-02T03:04:05Z" {
		t.Errorf("savedAt = %v", data["savedAt"])
	}
	if data["spaceElevatorBuilt"] != true {
		t.Errorf("spaceElevatorBuilt = %v", data["spaceElevatorBuilt"])
	}
	if data["gameBuild"] != int32(423794) {
		t.Errorf("gameBuild = %v (%T)", data["gameBuild"], data["gameBuild"])
	}
}

// Builders must tolerate a state where extraction found nothing (corrupt
// body recovered, or future format drift) — header-only output.
func TestBuildResultEmptyState(t *testing.T) {
	s := newSaveState(testHeader())
	result := s.buildResult()
	sections, _ := result["sections"].(map[string]any)
	if len(sections) != 3 {
		t.Fatalf("sections = %d, want 3", len(sections))
	}
	summary, _ := result["summary"].(string)
	if strings.Contains(summary, "Tier") {
		t.Errorf("summary %q should not contain a tier with no schematics", summary)
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
