package main

import (
	"strings"
	"testing"
)

func TestDisplayName(t *testing.T) {
	cases := map[string]string{
		"/Game/FactoryGame/Resource/Parts/IronPlate/Desc_IronPlate.Desc_IronPlate_C": "Iron Plate",
		"BP_EquipmentDescriptorHazmatSuit_C":                                         "Hazmat Suit",
		"BP_ItemDescriptorPortableMiner_C":                                           "Portable Miner",
		// Authoritative names override the class path: Coffee Stain's classes
		// disagree with the in-game names, and the canonical table wins.
		"/Game/.../Desc_SteelPlate.Desc_SteelPlate_C":                               "Steel Beam",
		"Desc_SteelPlateReinforced_C":                                               "Encased Industrial Beam",
		"/Game/X/Schematic_Alternate_WetConcrete.Schematic_Alternate_WetConcrete_C": "Alternate: Wet Concrete",
		"Desc_Chainsaw_C": "Chainsaw",
	}
	for in, want := range cases {
		if got := displayName(in); got != want {
			t.Errorf("displayName(%q) = %q, want %q", in, got, want)
		}
	}
}

// Classes absent from the canonical table (mods, future content) fall back to
// the class-path heuristic.
func TestDisplayNameHeuristicFallback(t *testing.T) {
	cases := map[string]string{
		"/Game/Mods/Acme/Desc_AcmeWidget.Desc_AcmeWidget_C": "Acme Widget",
		"Build_FutureMachineMk1_C":                          "Future Machine Mk1",
	}
	for in, want := range cases {
		if _, canonical := canonicalNames[in[strings.LastIndex(in, ".")+1:]]; canonical {
			t.Fatalf("test class %q is unexpectedly in canonicalNames", in)
		}
		if got := displayName(in); got != want {
			t.Errorf("displayName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMilestoneTier(t *testing.T) {
	cases := map[string]int{
		"/Game/FactoryGame/Schematics/Progression/Schematic_5-1.Schematic_5-1_C": 5,
		"Schematic_9-5_C":             9,
		"CustomizerUnlock_Swatches_C": 0,
		"Schematic_Tutorial3_C":       0,
		"Schematic_Alternate_Thing_C": 0,
	}
	for in, want := range cases {
		if got := milestoneTier(in); got != want {
			t.Errorf("milestoneTier(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestElevatorPhase(t *testing.T) {
	phasePath := "/Game/FactoryGame/GamePhases/GP_Project_Assembly_Phase_3.GP_Project_Assembly_Phase_3"
	if got := elevatorPhase(phasePath); got != 3 {
		t.Errorf("elevatorPhase = %d, want 3", got)
	}
	if got := elevatorPhase(""); got != 0 {
		t.Errorf("elevatorPhase(empty) = %d, want 0", got)
	}
}
