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

// twoCableLineFedBy builds one line of two cable constructors (consuming
// 120 wire/min total, none produced in-line) fed by a belt of the given
// instance/throughput, and returns the line's emitted map.
func twoCableLineFedBy(t *testing.T, beltInst string, throughput float64) map[string]any {
	t.Helper()
	s := newSaveState(testHeader())
	c1 := inst("Build_ConstructorMk1_C", 1)
	c2 := inst("Build_ConstructorMk1_C", 2)
	collectMachineAt(s, consClass, c1, [3]float32{0, 0, 0}, recipeProp(cableRecipe))
	collectMachineAt(s, consClass, c2, [3]float32{100, 0, 0}, recipeProp(cableRecipe))
	s.belts = []beltRecord{
		{instance: beltInst, class: "Build_ConveyorBeltMk1_C", throughput: throughput},
	}
	// The belt feeds both machines' inputs (directed belt→machine), which also
	// unions them into one line.
	s.connEdges = []connEdge{
		{from: beltInst, to: c1, transport: "belt", directed: true},
		{from: beltInst, to: c2, transport: "belt", directed: true},
	}
	s.resolve()
	lines, _ := s.buildProductionLinesSection()["lines"].([]map[string]any)
	if len(lines) != 1 {
		t.Fatalf("lines = %d, want 1", len(lines))
	}
	return lines[0]
}

func TestProductionLineDeliveryLimited(t *testing.T) {
	beltInst := inst("Build_ConveyorBeltMk1_C", 99)
	line := twoCableLineFedBy(t, beltInst, 60) // Mk1: 60/min, demand 120/min wire

	if line["inboundBeltCeiling"] != 60.0 {
		t.Errorf("inboundBeltCeiling = %v, want 60", line["inboundBeltCeiling"])
	}
	dl, ok := line["deliveryLimited"].(map[string]any)
	if !ok {
		t.Fatalf("deliveryLimited missing: %+v", line)
	}
	if dl["item"] != "Wire" || dl["requiredPerMin"] != 120.0 || dl["beltCeiling"] != 60.0 {
		t.Errorf("deliveryLimited = %v, want Wire 120 over ceiling 60", dl)
	}
}

func TestProductionLineNotDeliveryLimitedFastBelt(t *testing.T) {
	beltInst := inst("Build_ConveyorBeltMk5_C", 99)
	line := twoCableLineFedBy(t, beltInst, 780) // Mk5: 780/min >> 120/min demand

	if line["inboundBeltCeiling"] != 780.0 {
		t.Errorf("inboundBeltCeiling = %v, want 780", line["inboundBeltCeiling"])
	}
	if _, has := line["deliveryLimited"]; has {
		t.Errorf("should not be delivery-limited on a Mk5 belt: %v", line["deliveryLimited"])
	}
}

func TestProductionLineInternallySupplied(t *testing.T) {
	// A wire constructor (Recipe_Wire: 1 copper → 2 wire, 4s = 30 wire/min) and a
	// cable constructor (60 wire/min) on one Mk1-fed line. Wire is partly produced
	// in-line, so the external wire demand (60-30=30) is below the 60 ceiling, and
	// copper demand (15/min) is too → not delivery-limited.
	s := newSaveState(testHeader())
	wireM := inst("Build_ConstructorMk1_C", 1)
	cableM := inst("Build_ConstructorMk1_C", 2)
	collectMachineAt(s, consClass, wireM, [3]float32{0, 0, 0}, recipeProp(wireRecipe))
	collectMachineAt(s, consClass, cableM, [3]float32{100, 0, 0}, recipeProp(cableRecipe))
	beltInst := inst("Build_ConveyorBeltMk1_C", 99)
	s.belts = []beltRecord{{instance: beltInst, class: "Build_ConveyorBeltMk1_C", throughput: 60}}
	s.connEdges = []connEdge{
		{from: beltInst, to: wireM, transport: "belt", directed: true},
		{from: beltInst, to: cableM, transport: "belt", directed: true},
	}
	s.resolve()
	lines, _ := s.buildProductionLinesSection()["lines"].([]map[string]any)
	if _, has := lines[0]["deliveryLimited"]; has {
		t.Errorf(
			"in-line wire production should keep external demand under ceiling: %v",
			lines[0]["deliveryLimited"],
		)
	}
}

func TestProductionLineNoInboundBelt(t *testing.T) {
	// A line whose machines have no directed belt feeder → no ceiling, no flag.
	s := newSaveState(testHeader())
	c1 := inst("Build_ConstructorMk1_C", 1)
	belt0 := inst("Build_ConveyorBeltMk1_C", 2)
	c2 := inst("Build_ConstructorMk1_C", 3)
	collectMachineAt(s, consClass, c1, [3]float32{0, 0, 0}, recipeProp(cableRecipe))
	collectMachineAt(s, consClass, c2, [3]float32{100, 0, 0}, recipeProp(cableRecipe))
	// Undirected belt links only (no directed feeder edge).
	s.connEdges = []connEdge{belt(c1, belt0), belt(belt0, c2)}
	s.resolve()
	lines, _ := s.buildProductionLinesSection()["lines"].([]map[string]any)
	if _, has := lines[0]["inboundBeltCeiling"]; has {
		t.Errorf("no directed feeder → no inboundBeltCeiling: %v", lines[0]["inboundBeltCeiling"])
	}
	if _, has := lines[0]["deliveryLimited"]; has {
		t.Errorf("no feeder → no deliveryLimited")
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
