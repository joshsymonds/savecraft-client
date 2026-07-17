package main

import (
	"testing"

	"github.com/joshsymonds/savecraft-client/plugins/satisfactory/sav"
)

const minerClass = "/Game/FactoryGame/Buildable/Factory/MinerMk1/Build_MinerMk1.Build_MinerMk1_C"

func TestBaseIndexClusters(t *testing.T) {
	near1 := machineRecord{instance: "a", position: [3]float32{0, 0, 0}}
	near2 := machineRecord{instance: "b", position: [3]float32{25000, 0, 0}} // adjacent cell
	far := machineRecord{instance: "c", position: [3]float32{5000000, 0, 0}}

	bases := newBaseIndex([]machineRecord{near1, near2, far}).bases
	if len(bases) != 2 {
		t.Fatalf("bases = %d, want 2", len(bases))
	}
	// Largest base first (the two adjacent machines).
	if len(bases[0]) != 2 || len(bases[1]) != 1 {
		t.Errorf("base sizes = %d, %d; want 2, 1", len(bases[0]), len(bases[1]))
	}

	if got := newBaseIndex(nil).bases; len(got) != 0 {
		t.Errorf("empty input → %d bases, want 0", len(got))
	}
}

func TestBaseIndexDeterministic(t *testing.T) {
	m := []machineRecord{
		{instance: "a", position: [3]float32{0, 0, 0}},
		{instance: "b", position: [3]float32{9000000, 0, 0}},
	}
	first := newBaseIndex(m)
	second := newBaseIndex(m)
	if len(first.bases) != 2 {
		t.Fatalf("bases = %d, want 2", len(first.bases))
	}
	if first.bases[0][0].instance != second.bases[0][0].instance {
		t.Error("newBaseIndex not deterministic")
	}
	// assign must also be stable across rebuilds.
	pos := [3]float32{0, 0, 0}
	if first.assign(pos) != second.assign(pos) {
		t.Error("assign not deterministic across rebuilds")
	}
}

func TestBaseIndexAssign(t *testing.T) {
	// Two bases: A at the origin cell, B far away on the +X axis.
	a := machineRecord{instance: "a", position: [3]float32{0, 0, 0}}
	b := machineRecord{instance: "b", position: [3]float32{5000000, 0, 0}}
	idx := newBaseIndex([]machineRecord{a, b})
	if len(idx.bases) != 2 {
		t.Fatalf("bases = %d, want 2", len(idx.bases))
	}
	// Base 0 is the larger... here both are size 1, so order is by instance:
	// "a" (origin) is base 0, "b" (far) is base 1.
	aBase, bBase := idx.assign(a.position), idx.assign(b.position)

	// A container sitting in base A's own cell → base A.
	if got := idx.assign([3]float32{1000, 1000, 0}); got != aBase {
		t.Errorf("in-cell assign = %d, want %d (base A)", got, aBase)
	}
	// A container in an empty cell, but much closer to B → base B (nearest centroid).
	if got := idx.assign([3]float32{4900000, 0, 0}); got != bBase {
		t.Errorf("between-bases assign = %d, want %d (nearest base B)", got, bBase)
	}
	// A container in an empty cell closer to A → base A.
	if got := idx.assign([3]float32{200000, 0, 0}); got != aBase {
		t.Errorf("between-bases assign = %d, want %d (nearest base A)", got, aBase)
	}
}

func TestBaseIndexAssignEmpty(t *testing.T) {
	if got := newBaseIndex(nil).assign([3]float32{0, 0, 0}); got != -1 {
		t.Errorf("assign on empty index = %d, want -1", got)
	}
}

func TestGeographySection(t *testing.T) {
	s := newSaveState(testHeader())
	mfg := inst("Build_ConstructorMk1_C", 1)
	ext := inst("Build_MinerMk1_C", 2)
	nodePath := "Persistent_Level:PersistentLevel.BP_ResourceNode42"

	collectMachineAt(s, consClass, mfg, [3]float32{0, 0, 0}, recipeProp(screwRecipe))
	collectMachineAt(s, minerClass, ext, [3]float32{5000000, 0, 0}, map[string]any{
		"mExtractableResource": sav.ObjectRef{Path: nodePath},
	})
	s.machineInventories[ext+".OutputInventory"] = &sav.ObjectData{Properties: map[string]any{
		"mInventoryStacks": []any{stackOf("/Game/X/Desc_OreIron.Desc_OreIron_C", 50)}}}
	s.resourceNodePos[nodePath] = [3]float32{5000000, 0, 0}
	s.mapMarkers = []mapMarker{{name: "Home Base", x: 0, y: 0}}
	s.gameState = &sav.ObjectData{Properties: map[string]any{
		"mVisitedMapAreas": []any{sav.ObjectRef{Path: "/Game/X/Area_Foothills.Area_Foothills_C"}}}}
	s.resolve()

	data := s.buildGeographySection()

	bases, _ := data["bases"].([]map[string]any)
	if len(bases) != 2 {
		t.Fatalf("bases = %d, want 2", len(bases))
	}
	if _, ok := bases[0]["name"].(string); !ok {
		t.Errorf("base missing name: %+v", bases[0])
	}

	markers, _ := data["markers"].([]map[string]any)
	if len(markers) != 1 || markers[0]["name"] != "Home Base" {
		t.Errorf("markers = %+v", markers)
	}

	areas, _ := data["visitedAreas"].([]string)
	if len(areas) != 1 || areas[0] != "Area_Foothills" {
		t.Errorf("visitedAreas = %v", areas)
	}

	nodes, _ := data["resourceNodes"].(map[string]any)
	if total, _ := nodes["total"].(int); total != 1 {
		t.Errorf("resourceNodes.total = %v, want 1", nodes["total"])
	}
	occ, _ := nodes["occupied"].([]map[string]any)
	if len(occ) != 1 {
		t.Fatalf("occupied = %d, want 1", len(occ))
	}
	if occ[0]["resource"] == "unknown" || occ[0]["resource"] == nil {
		t.Errorf("occupied resource = %v, want a known resource", occ[0]["resource"])
	}
}
