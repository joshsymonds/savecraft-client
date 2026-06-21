package main

import (
	"testing"

	"github.com/joshsymonds/savecraft.gg/plugins/satisfactory/sav"
)

const (
	consClass = "/Game/FactoryGame/Buildable/Factory/ConstructorMk1/Build_ConstructorMk1.Build_ConstructorMk1_C"
	asmClass  = "/Game/FactoryGame/Buildable/Factory/AssemblerMk1/Build_AssemblerMk1.Build_AssemblerMk1_C"
)

func recipeProp(path string) map[string]any {
	return map[string]any{"mCurrentRecipe": sav.ObjectRef{Path: path}}
}

func TestProductionLinesSectionTwoDisjoint(t *testing.T) {
	s := newSaveState(testHeader())
	c1, b1, a1 := inst(
		"Build_ConstructorMk1_C",
		1,
	), inst(
		"Build_ConveyorBeltMk1_C",
		2,
	), inst(
		"Build_AssemblerMk1_C",
		3,
	)
	c2, b2, a2 := inst(
		"Build_ConstructorMk1_C",
		4,
	), inst(
		"Build_ConveyorBeltMk1_C",
		5,
	), inst(
		"Build_AssemblerMk1_C",
		6,
	)
	collectMachineAt(s, consClass, c1, [3]float32{0, 0, 0}, recipeProp(screwRecipe))
	collectMachineAt(s, asmClass, a1, [3]float32{100, 0, 0}, recipeProp(leachedRecipe))
	collectMachineAt(s, consClass, c2, [3]float32{500000, 0, 0}, recipeProp(screwRecipe))
	collectMachineAt(s, asmClass, a2, [3]float32{500100, 0, 0}, recipeProp(leachedRecipe))
	s.connEdges = []connEdge{belt(c1, b1), belt(b1, a1), belt(c2, b2), belt(b2, a2)}
	s.resolve()

	data := s.buildProductionLinesSection()
	if data["lineCount"] != 2 {
		t.Fatalf("lineCount = %v, want 2", data["lineCount"])
	}
	lines, _ := data["lines"].([]map[string]any)
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(lines))
	}
	for _, l := range lines {
		if l["machineCount"] != 2 {
			t.Errorf("machineCount = %v, want 2", l["machineCount"])
		}
		if recipes, _ := l["recipes"].([]map[string]any); len(recipes) == 0 {
			t.Errorf("line has no recipe groups")
		}
	}
	if data["unconnectedMachines"] != 0 {
		t.Errorf("unconnectedMachines = %v, want 0", data["unconnectedMachines"])
	}
}

func TestProductionLinesSectionProblemCallout(t *testing.T) {
	s := newSaveState(testHeader())
	blocked := inst("Build_ConstructorMk1_C", 1)
	belt0 := inst("Build_ConveyorBeltMk1_C", 2)
	ok := inst("Build_ConstructorMk1_C", 3)
	collectMachineAt(
		s,
		consClass,
		blocked,
		[3]float32{10, 20, 30},
		recipeProp(screwRecipe),
	) // producing=false, prod 0
	collectMachineAt(
		s,
		consClass,
		ok,
		[3]float32{40, 0, 0},
		mergeProps(recipeProp(screwRecipe), map[string]any{
			"mIsProducing":                                true,
			"mLastProductivityMeasurementDuration":        300.0,
			"mLastProductivityMeasurementProduceDuration": 300.0,
		}),
	)
	// blocked machine: output at stack max.
	s.machineInventories[blocked+".OutputInventory"] = &sav.ObjectData{Properties: map[string]any{
		"mInventoryStacks": []any{stackOf("/Game/X/Desc_IronScrew.Desc_IronScrew_C", 500)}}}
	s.connEdges = []connEdge{belt(blocked, belt0), belt(belt0, ok)}
	s.resolve()

	lines, _ := s.buildProductionLinesSection()["lines"].([]map[string]any)
	if len(lines) != 1 {
		t.Fatalf("lines = %d, want 1", len(lines))
	}
	problems, _ := lines[0]["problems"].([]map[string]any)
	if len(problems) != 1 {
		t.Fatalf("problems = %d, want 1: %+v", len(problems), lines[0])
	}
	p := problems[0]
	if p["status"] != string(statusBlocked) {
		t.Errorf("problem status = %v, want %s", p["status"], statusBlocked)
	}
	pos, _ := p["position"].(map[string]any)
	if pos["x"] != float32(10) {
		t.Errorf("problem position = %v, want x=10", pos)
	}
}

func TestProductionLinesSectionLimitingInput(t *testing.T) {
	s := newSaveState(testHeader())
	// A throttled assembler running the leached-iron recipe with a thin iron-ore
	// buffer and plentiful acid: input_limited, limitingInput names Iron Ore.
	limited := inst("Build_AssemblerMk1_C", 1)
	belt0 := inst("Build_ConveyorBeltMk1_C", 2)
	sink := inst("Build_ConstructorMk1_C", 3)
	collectMachineAt(
		s,
		asmClass,
		limited,
		[3]float32{10, 20, 30},
		mergeProps(recipeProp(leachedRecipe), map[string]any{
			"mLastProductivityMeasurementDuration":        300.0,
			"mLastProductivityMeasurementProduceDuration": 90.0,
		}),
	) // prod 0.3
	collectMachineAt(s, consClass, sink, [3]float32{40, 0, 0}, recipeProp(screwRecipe))
	s.machineInventories[limited+".InputInventory"] = &sav.ObjectData{Properties: map[string]any{
		"mInventoryStacks": []any{
			stackOf(
				"/Game/X/Desc_OreIron.Desc_OreIron_C",
				2,
			), // need 5 → ratio 0.4 (thin)
			stackOf("/Game/X/Desc_SulfuricAcid.Desc_SulfuricAcid_C", 5000), // need 1000 → plentiful
		}}}
	s.connEdges = []connEdge{belt(limited, belt0), belt(belt0, sink)}
	s.resolve()

	lines, _ := s.buildProductionLinesSection()["lines"].([]map[string]any)
	if len(lines) != 1 {
		t.Fatalf("lines = %d, want 1", len(lines))
	}
	problems, _ := lines[0]["problems"].([]map[string]any)
	var p map[string]any
	for _, pr := range problems {
		if pr["status"] == string(statusInputLimited) {
			p = pr
		}
	}
	if p == nil {
		t.Fatalf("no input_limited problem found: %+v", problems)
	}
	if p["limitingInput"] != displayName("Desc_OreIron_C") {
		t.Errorf("limitingInput = %v, want %q", p["limitingInput"], displayName("Desc_OreIron_C"))
	}
}

func TestProductionLinesSectionUnconnected(t *testing.T) {
	s := newSaveState(testHeader())
	c1, belt0, c2 := inst(
		"Build_ConstructorMk1_C",
		1,
	), inst(
		"Build_ConveyorBeltMk1_C",
		2,
	), inst(
		"Build_ConstructorMk1_C",
		3,
	)
	c3 := inst("Build_ConstructorMk1_C", 9) // no connections
	for _, c := range []string{c1, c2, c3} {
		collectMachineAt(s, consClass, c, [3]float32{0, 0, 0}, recipeProp(screwRecipe))
	}
	s.connEdges = []connEdge{belt(c1, belt0), belt(belt0, c2)}
	s.resolve()

	data := s.buildProductionLinesSection()
	if data["lineCount"] != 1 {
		t.Errorf("lineCount = %v, want 1", data["lineCount"])
	}
	if data["unconnectedMachines"] != 1 {
		t.Errorf("unconnectedMachines = %v, want 1", data["unconnectedMachines"])
	}
}
