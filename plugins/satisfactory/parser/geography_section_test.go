package main

import (
	"testing"

	"github.com/joshsymonds/savecraft.gg/plugins/satisfactory/sav"
)

const minerClass = "/Game/FactoryGame/Buildable/Factory/MinerMk1/Build_MinerMk1.Build_MinerMk1_C"

func TestClusterBases(t *testing.T) {
	near1 := machineRecord{instance: "a", position: [3]float32{0, 0, 0}}
	near2 := machineRecord{instance: "b", position: [3]float32{25000, 0, 0}} // adjacent cell
	far := machineRecord{instance: "c", position: [3]float32{5000000, 0, 0}}

	bases := clusterBases([]machineRecord{near1, near2, far}, baseCellSize)
	if len(bases) != 2 {
		t.Fatalf("bases = %d, want 2", len(bases))
	}
	// Largest base first (the two adjacent machines).
	if len(bases[0]) != 2 || len(bases[1]) != 1 {
		t.Errorf("base sizes = %d, %d; want 2, 1", len(bases[0]), len(bases[1]))
	}

	if got := clusterBases(nil, baseCellSize); len(got) != 0 {
		t.Errorf("empty input → %d bases, want 0", len(got))
	}
}

func TestClusterBasesDeterministic(t *testing.T) {
	m := []machineRecord{
		{instance: "a", position: [3]float32{0, 0, 0}},
		{instance: "b", position: [3]float32{9000000, 0, 0}},
	}
	first := clusterBases(m, baseCellSize)
	second := clusterBases(m, baseCellSize)
	if len(first) != 2 {
		t.Fatalf("bases = %d, want 2", len(first))
	}
	if first[0][0].instance != second[0][0].instance {
		t.Error("clusterBases not deterministic")
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
