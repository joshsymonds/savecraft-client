package main

import (
	"testing"

	"github.com/joshsymonds/savecraft-client/plugins/satisfactory/sav"
)

const (
	wireRecipe   = "/Game/FactoryGame/Recipes/Constructor/Recipe_Wire.Recipe_Wire_C"
	cableRecipe  = "/Game/FactoryGame/Recipes/Constructor/Recipe_Cable.Recipe_Cable_C"
	statorRecipe = "/Game/FactoryGame/Recipes/Assembler/Recipe_Stator.Recipe_Stator_C"
)

// prodProps returns machine props with a recipe and a measured productivity.
func prodProps(recipe string, productivity float64) map[string]any {
	return map[string]any{
		"mCurrentRecipe":                              sav.ObjectRef{Path: recipe},
		"mLastProductivityMeasurementDuration":        100.0,
		"mLastProductivityMeasurementProduceDuration": 100.0 * productivity,
	}
}

// findItem returns the flow_balance item entry for classPath in a base bucket.
func findItem(items []map[string]any, classPath string) map[string]any {
	for _, it := range items {
		if it["classPath"] == classPath {
			return it
		}
	}
	return nil
}

// flowItems returns the first base's flow item entries (the tests build a
// single base).
func flowItems(t *testing.T, s *saveState) []map[string]any {
	t.Helper()
	bases, _ := s.buildFlowBalanceSection()["bases"].([]map[string]any)
	if len(bases) == 0 {
		t.Fatalf("flow_balance has no bases")
	}
	items, _ := bases[0]["items"].([]map[string]any)
	return items
}

// One base: a Wire constructor (30 wire/min) feeding a Cable constructor
// (60 wire/min) AND a Stator assembler (40 wire/min) — net -70, two distinct
// consumers, one of them "hidden" behind a different recipe.
func TestFlowBalanceHiddenConsumer(t *testing.T) {
	s := newSaveState(testHeader())
	collectMachineAt(
		s,
		consClass,
		inst("Build_ConstructorMk1_C", 1),
		[3]float32{0, 0, 0},
		prodProps(wireRecipe, 1.0),
	)
	collectMachineAt(
		s,
		consClass,
		inst("Build_ConstructorMk1_C", 2),
		[3]float32{1000, 0, 0},
		prodProps(cableRecipe, 1.0),
	)
	collectMachineAt(
		s,
		asmClass,
		inst("Build_AssemblerMk1_C", 3),
		[3]float32{2000, 0, 0},
		prodProps(statorRecipe, 1.0),
	)
	s.resolve()

	wire := findItem(flowItems(t, s), "Desc_Wire_C")
	if wire == nil {
		t.Fatalf("no Wire entry in flow_balance: %+v", flowItems(t, s))
	}
	if wire["producedPerMin"] != 30.0 {
		t.Errorf("wire producedPerMin = %v, want 30", wire["producedPerMin"])
	}
	if wire["consumedPerMin"] != 100.0 {
		t.Errorf(
			"wire consumedPerMin = %v, want 100 (cable 60 + stator 40)",
			wire["consumedPerMin"],
		)
	}
	if wire["net"] != -70.0 {
		t.Errorf("wire net = %v, want -70", wire["net"])
	}
	consumers, _ := wire["consumers"].([]map[string]any)
	if len(consumers) != 2 {
		t.Fatalf("wire consumers = %d, want 2 (cable + stator): %+v", len(consumers), consumers)
	}
	// Both consumer recipes present with their own rates.
	rates := map[string]float64{}
	for _, c := range consumers {
		name, _ := c["recipe"].(string)
		rate, _ := c["ratePerMin"].(float64)
		rates[name] = rate
	}
	if rates[displayName(cableRecipe)] != 60.0 {
		t.Errorf("cable consumer rate = %v, want 60", rates[displayName(cableRecipe)])
	}
	if rates[displayName(statorRecipe)] != 40.0 {
		t.Errorf("stator consumer rate = %v, want 40", rates[displayName(statorRecipe)])
	}
}

// measuredPerMin reflects measured productivity; rated producedPerMin does not.
func TestFlowBalanceMeasuredVsRated(t *testing.T) {
	s := newSaveState(testHeader())
	// Wire machine throttled to 0.5 productivity.
	collectMachineAt(
		s,
		consClass,
		inst("Build_ConstructorMk1_C", 1),
		[3]float32{0, 0, 0},
		prodProps(wireRecipe, 0.5),
	)
	s.resolve()

	wire := findItem(flowItems(t, s), "Desc_Wire_C")
	if wire["producedPerMin"] != 30.0 {
		t.Errorf(
			"rated producedPerMin = %v, want 30 (unaffected by throttle)",
			wire["producedPerMin"],
		)
	}
	if wire["measuredPerMin"] != 15.0 {
		t.Errorf("measuredPerMin = %v, want 15 (30 x 0.5)", wire["measuredPerMin"])
	}
}

// Somersloop boost doubles output rate but not consumption.
func TestFlowBalanceBoostOutputOnly(t *testing.T) {
	s := newSaveState(testHeader())
	props := prodProps(cableRecipe, 1.0)
	props["mCurrentProductionBoost"] = 2.0
	collectMachineAt(s, consClass, inst("Build_ConstructorMk1_C", 1), [3]float32{0, 0, 0}, props)
	s.resolve()

	items := flowItems(t, s)
	cable := findItem(items, "Desc_Cable_C")
	wire := findItem(items, "Desc_Wire_C")
	// Cable recipe: 2 wire -> 1 cable, 2s. Base output 30/min; boost 2 -> 60.
	if cable["producedPerMin"] != 60.0 {
		t.Errorf("boosted cable producedPerMin = %v, want 60", cable["producedPerMin"])
	}
	// Wire consumption is clock-only (60/min), NOT doubled by boost.
	if wire["consumedPerMin"] != 60.0 {
		t.Errorf(
			"wire consumedPerMin = %v, want 60 (boost must not amplify input)",
			wire["consumedPerMin"],
		)
	}
}

// The buffer column equals the base's storage bucket for that item.
func TestFlowBalanceBuffer(t *testing.T) {
	s := newSaveState(testHeader())
	collectMachineAt(
		s,
		consClass,
		inst("Build_ConstructorMk1_C", 1),
		[3]float32{0, 0, 0},
		prodProps(cableRecipe, 1.0),
	)
	collectPositionedContainer(s, "L:P.Build_StorageContainerMk1_C_5", [3]float32{500, 0, 0},
		"/Game/X/Desc_Wire.Desc_Wire_C", 1234)
	s.resolve()

	wire := findItem(flowItems(t, s), "Desc_Wire_C")
	if wire["buffer"] != int64(1234) {
		t.Errorf("wire buffer = %v, want 1234 (from in-base storage)", wire["buffer"])
	}
}

// An item also extracted in the base is flagged rawSupplied (deficit is fed by
// mining, not a real shortfall).
func TestFlowBalanceRawSupplied(t *testing.T) {
	s := newSaveState(testHeader())
	// A smelter consuming iron ore, and a miner producing it in the same base.
	ironIngot := "/Game/FactoryGame/Recipes/Smelter/Recipe_IngotIron.Recipe_IngotIron_C"
	collectMachineAt(
		s,
		consClass,
		inst("Build_SmelterMk1_C", 1),
		[3]float32{0, 0, 0},
		prodProps(ironIngot, 1.0),
	)
	miner := "/Game/FactoryGame/Buildable/Factory/MinerMk1/Build_MinerMk1.Build_MinerMk1_C"
	ext := inst("Build_MinerMk1_C", 2)
	collectMachineAt(s, miner, ext, [3]float32{1000, 0, 0}, nil)
	s.machineInventories[ext+".OutputInventory"] = &sav.ObjectData{Properties: map[string]any{
		"mInventoryStacks": []any{stackOf("/Game/X/Desc_OreIron.Desc_OreIron_C", 50)}}}
	s.resolve()

	ore := findItem(flowItems(t, s), "Desc_OreIron_C")
	if ore == nil {
		t.Fatalf("no Iron Ore entry: %+v", flowItems(t, s))
	}
	if ore["rawSupplied"] != true {
		t.Errorf("iron ore rawSupplied = %v, want true (mined in base)", ore["rawSupplied"])
	}
}
