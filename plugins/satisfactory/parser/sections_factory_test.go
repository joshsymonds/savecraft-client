package main

import (
	"testing"

	"github.com/joshsymonds/savecraft-client/plugins/satisfactory/sav"
)

func TestFactoryKind(t *testing.T) {
	cases := map[string]string{
		"/Game/FactoryGame/Buildable/Factory/ConstructorMk1/Build_ConstructorMk1.Build_ConstructorMk1_C":       "manufacturer",
		"/Game/FactoryGame/Buildable/Factory/MinerMk2/Build_MinerMk2.Build_MinerMk2_C":                         "extractor",
		"/Game/FactoryGame/Buildable/Factory/GeneratorNuclear/Build_GeneratorNuclear.Build_GeneratorNuclear_C": "generator",
		"/Game/FactoryGame/Buildable/Factory/PowerStorageMk1/Build_PowerStorageMk1.Build_PowerStorageMk1_C":    "powerStorage",
		"/Game/FactoryGame/Buildable/Factory/ConveyorBeltMk5/Build_ConveyorBeltMk5.Build_ConveyorBeltMk5_C":    "",
		"/Script/FactoryGame.FGFactoryConnectionComponent":                                                     "",
	}
	for in, want := range cases {
		if got := factoryKind(in); got != want {
			t.Errorf("factoryKind(%q) = %q, want %q", in, got, want)
		}
	}
}

func collectMachine(s *saveState, classPath string, props map[string]any) {
	o := sav.Object{ObjectHeader: sav.ObjectHeader{ClassPath: classPath}}
	s.collectFactory(factoryKind(classPath), o, &sav.ObjectData{Properties: props})
}

func factoryState() *saveState {
	s := newSaveState(testHeader())
	constructor := "/Game/FactoryGame/Buildable/Factory/ConstructorMk1/Build_ConstructorMk1.Build_ConstructorMk1_C"
	ironPlate := sav.ObjectRef{
		Path: "/Game/FactoryGame/Recipes/Constructor/Recipe_IronPlate.Recipe_IronPlate_C",
	}
	// Two iron plate constructors at 100% (one idle), one at 150% clock.
	collectMachine(s, constructor, map[string]any{
		"mCurrentRecipe": ironPlate, "mIsProducing": true,
		"mLastProductivityMeasurementDuration":        300.0,
		"mLastProductivityMeasurementProduceDuration": 300.0,
	})
	collectMachine(s, constructor, map[string]any{
		"mCurrentRecipe": ironPlate, "mIsProducing": false,
		"mLastProductivityMeasurementDuration":        300.0,
		"mLastProductivityMeasurementProduceDuration": 150.0,
	})
	// Overclocked to 150% and fully somersloop-amplified (2x output).
	collectMachine(s, constructor, map[string]any{
		"mCurrentRecipe": ironPlate, "mIsProducing": true, "mCurrentPotential": 1.5,
		"mCurrentProductionBoost": 2.0,
	})
	// One recipe-less assembler.
	assembler := "/Game/FactoryGame/Buildable/Factory/AssemblerMk1/Build_AssemblerMk1.Build_AssemblerMk1_C"
	collectMachine(s, assembler, map[string]any{"mIsProducing": false})
	// Extractor at 250%.
	miner := "/Game/FactoryGame/Buildable/Factory/MinerMk3/Build_MinerMk3.Build_MinerMk3_C"
	collectMachine(s, miner, map[string]any{"mIsProducing": true, "mCurrentPotential": 2.5})
	// Two fuel generators on turbofuel, one nuclear. Fuel generators burn the
	// liquid (Desc_LiquidTurboFuel_C, "Turbofuel"); Desc_TurboFuel_C is the
	// packaged item.
	turbo := sav.ObjectRef{Path: "/Game/X/Desc_LiquidTurboFuel.Desc_LiquidTurboFuel_C"}
	fuelGen := "/Game/FactoryGame/Buildable/Factory/GeneratorFuel/Build_GeneratorFuel.Build_GeneratorFuel_C"
	for range 2 {
		collectMachine(s, fuelGen, map[string]any{"mIsProducing": true, "mCurrentFuelClass": turbo})
	}
	nuclear := "/Game/FactoryGame/Buildable/Factory/GeneratorNuclear/Build_GeneratorNuclear.Build_GeneratorNuclear_C"
	collectMachine(s, nuclear, map[string]any{"mIsProducing": true})
	// Power storage at half charge.
	storage := "/Game/FactoryGame/Buildable/Factory/PowerStorageMk1/Build_PowerStorageMk1.Build_PowerStorageMk1_C"
	collectMachine(s, storage, map[string]any{"mPowerStore": 50.0})
	s.powerCircuits = 3
	return s
}

func TestBuildMachinesSection(t *testing.T) {
	data := factoryState().buildMachinesSection()

	if data["totalManufacturers"] != 4 {
		t.Errorf("totalManufacturers = %v, want 4", data["totalManufacturers"])
	}
	if data["totalExtractors"] != 1 {
		t.Errorf("totalExtractors = %v, want 1", data["totalExtractors"])
	}
	groups, _ := data["manufacturers"].([]map[string]any)
	// 3 groups: 2x iron plate @100%, 1x @150%, 1x recipe-less assembler.
	if len(groups) != 3 {
		t.Fatalf("manufacturer groups = %d (%v), want 3", len(groups), groups)
	}
	top := groups[0]
	if top["count"] != 2 || top["recipe"] != "Iron Plate" || top["producing"] != 1 {
		t.Errorf("top group = %v", top)
	}
	if top["measuredProductivityPct"] != 75.0 {
		t.Errorf(
			"measuredProductivityPct = %v, want 75 ((100+50)/2)",
			top["measuredProductivityPct"],
		)
	}
	if _, ok := top["slooped"]; ok {
		t.Errorf("unboosted group has slooped key: %v", top)
	}
	boosted := groups[1]
	if boosted["slooped"] != 1 || boosted["avgClock"] != 1.5 {
		t.Errorf("boosted group = %v, want slooped=1 avgClock=1.5", boosted)
	}
}

func TestBuildProductionSection(t *testing.T) {
	data := factoryState().buildProductionSection()

	recipes, _ := data["byRecipe"].([]map[string]any)
	if len(recipes) != 1 {
		t.Fatalf("byRecipe = %v, want 1 entry", recipes)
	}
	r := recipes[0]
	// Capacity in 100%-clock machine equivalents: 1 + 1 + 1.5*2 (sloop) = 5.
	if r["recipe"] != "Iron Plate" || r["machines"] != 3 || r["effectiveCapacity"] != 5.0 {
		t.Errorf("iron plate entry = %v", r)
	}
	if _, ok := r["totalClock"]; ok {
		t.Errorf("totalClock should be replaced by effectiveCapacity: %v", r)
	}
	if data["machinesWithoutRecipe"] != 1 {
		t.Errorf("machinesWithoutRecipe = %v, want 1", data["machinesWithoutRecipe"])
	}
}

func TestBuildPowerSection(t *testing.T) {
	data := factoryState().buildPowerSection()

	if data["circuits"] != 3 {
		t.Errorf("circuits = %v, want 3", data["circuits"])
	}
	if data["totalGenerators"] != 3 {
		t.Errorf("totalGenerators = %v, want 3", data["totalGenerators"])
	}
	groups, _ := data["generators"].([]map[string]any)
	if len(groups) != 2 {
		t.Fatalf("generator groups = %v, want 2", groups)
	}
	if groups[0]["count"] != 2 || groups[0]["fuel"] != "Turbofuel" {
		t.Errorf("fuel generators = %v", groups[0])
	}
	storage, _ := data["powerStorage"].(map[string]any)
	if storage["count"] != 1 || storage["totalStoredMWh"] != 50.0 {
		t.Errorf("powerStorage = %v", storage)
	}
}
