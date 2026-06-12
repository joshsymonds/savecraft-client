package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/joshsymonds/savecraft.gg/plugins/satisfactory/sav"
)

// Factory building classes live under /Buildable/Factory/<dir>/Build_*. The
// directory list below names every production/extraction/generation
// building; passive infrastructure (belts, pipes, poles, signs — tens of
// thousands of objects in megafactories) is deliberately not extracted.
// New machine types added by game updates are missed until listed here;
// plugin.toml carries that limitation.
func factoryKind(classPath string) string {
	dir, ok := strings.CutPrefix(classPath, "/Game/FactoryGame/Buildable/Factory/")
	if !ok {
		return ""
	}
	dir, _, ok = strings.Cut(dir, "/")
	if !ok {
		return ""
	}
	switch dir {
	case "ConstructorMk1", "AssemblerMk1", "ManufacturerMk1", "SmelterMk1",
		"FoundryMk1", "OilRefinery", "Packager", "Blender", "HadronCollider",
		"QuantumEncoder", "Converter":
		return "manufacturer"
	case "MinerMk1", "MinerMk2", "MinerMk3", "OilPump", "WaterPump",
		"FrackingExtractor", "FrackingSmasher":
		return "extractor"
	case "GeneratorCoal", "GeneratorFuel", "GeneratorNuclear",
		"GeneratorBiomass", "GeneratorGeoThermal":
		return "generator"
	case "PowerStorageMk1":
		return "powerStorage"
	default:
		return ""
	}
}

// machineRecord is the compact per-machine state kept during the streaming
// pass (full ObjectData is not retained).
type machineRecord struct {
	building     string // class name, e.g. Build_ConstructorMk1_C
	recipe       string // recipe class path ("" = no recipe set / extractor)
	fuel         string // generators: fuel class path
	clock        float64
	producing    bool
	productivity float64 // measured produce/duration ratio, -1 if absent
	node         string  // extractors: occupied resource node instance path
}

func (s *saveState) collectFactory(kind string, o sav.Object, od *sav.ObjectData) {
	rec := machineRecord{
		building:     o.ClassPath[strings.LastIndex(o.ClassPath, ".")+1:],
		clock:        1.0,
		productivity: -1,
	}
	if clock, ok := prop[float64](od, "mCurrentPotential"); ok {
		rec.clock = clock
	}
	if producing, ok := prop[bool](od, "mIsProducing"); ok {
		rec.producing = producing
	}
	if recipe, ok := prop[sav.ObjectRef](od, "mCurrentRecipe"); ok {
		rec.recipe = recipe.Path
	}
	if fuel, ok := prop[sav.ObjectRef](od, "mCurrentFuelClass"); ok {
		rec.fuel = fuel.Path
	}
	if node, ok := prop[sav.ObjectRef](od, "mExtractableResource"); ok {
		rec.node = node.Path
	}
	duration, okDuration := prop[float64](od, "mLastProductivityMeasurementDuration")
	produce, okProduce := prop[float64](od, "mLastProductivityMeasurementProduceDuration")
	if okDuration && okProduce && duration > 0 {
		rec.productivity = produce / duration
	}

	switch kind {
	case "manufacturer":
		s.manufacturers = append(s.manufacturers, rec)
	case "extractor":
		s.extractors = append(s.extractors, rec)
	case "generator":
		s.generators = append(s.generators, rec)
	case "powerStorage":
		charge, _ := prop[float64](od, "mPowerStore")
		s.powerStorageCharges = append(s.powerStorageCharges, charge)
	}
}

// machineGroup aggregates identical machines.
type machineGroup struct {
	building    string
	recipe      string
	fuel        string
	count       int
	producing   int
	sumClock    float64
	sumProd     float64
	prodSamples int
}

func groupMachines(records []machineRecord, key func(machineRecord) string) []*machineGroup {
	byKey := map[string]*machineGroup{}
	order := []string{}
	for _, r := range records {
		k := key(r)
		g, ok := byKey[k]
		if !ok {
			g = &machineGroup{building: r.building, recipe: r.recipe, fuel: r.fuel}
			byKey[k] = g
			order = append(order, k)
		}
		g.count++
		if r.producing {
			g.producing++
		}
		g.sumClock += r.clock
		if r.productivity >= 0 {
			g.sumProd += r.productivity
			g.prodSamples++
		}
	}
	groups := make([]*machineGroup, 0, len(order))
	for _, k := range order {
		groups = append(groups, byKey[k])
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].count > groups[j].count })
	return groups
}

func (g *machineGroup) describe() map[string]any {
	out := map[string]any{
		"building":  displayName(g.building),
		"count":     g.count,
		"producing": g.producing,
		"avgClock":  round2(g.sumClock / float64(g.count)),
	}
	if g.recipe != "" {
		out["recipe"] = displayName(g.recipe)
		out["recipeClassPath"] = g.recipe
	}
	if g.fuel != "" {
		out["fuel"] = displayName(g.fuel)
	}
	if g.prodSamples > 0 {
		out["measuredProductivityPct"] = round2(100 * g.sumProd / float64(g.prodSamples))
	}
	return out
}

func round2(v float64) float64 {
	return float64(int(v*100+0.5)) / 100
}

func describeGroups(groups []*machineGroup) []map[string]any {
	out := make([]map[string]any, 0, len(groups))
	for _, g := range groups {
		out = append(out, g.describe())
	}
	return out
}

func (s *saveState) buildMachinesSection() map[string]any {
	manufacturers := groupMachines(s.manufacturers, func(r machineRecord) string {
		return r.building + "|" + r.recipe + "|" + fmt.Sprintf("%.4f", r.clock)
	})
	extractors := groupMachines(s.extractors, func(r machineRecord) string {
		return r.building + "|" + fmt.Sprintf("%.4f", r.clock)
	})
	return map[string]any{
		"totalManufacturers": len(s.manufacturers),
		"totalExtractors":    len(s.extractors),
		"manufacturers":      describeGroups(manufacturers),
		"extractors":         describeGroups(extractors),
	}
}

func (s *saveState) buildProductionSection() map[string]any {
	byRecipe := groupMachines(s.manufacturers, func(r machineRecord) string { return r.recipe })
	recipes := make([]map[string]any, 0, len(byRecipe))
	for _, g := range byRecipe {
		if g.recipe == "" {
			continue
		}
		entry := map[string]any{
			"recipe":          displayName(g.recipe),
			"recipeClassPath": g.recipe,
			"machines":        g.count,
			"producing":       g.producing,
			"totalClock":      round2(g.sumClock),
		}
		if g.prodSamples > 0 {
			entry["measuredProductivityPct"] = round2(100 * g.sumProd / float64(g.prodSamples))
		}
		recipes = append(recipes, entry)
	}
	idle := 0
	for _, r := range s.manufacturers {
		if r.recipe == "" {
			idle++
		}
	}
	return map[string]any{
		"byRecipe":              recipes,
		"machinesWithoutRecipe": idle,
	}
}

func (s *saveState) buildPowerSection() map[string]any {
	generators := groupMachines(s.generators, func(r machineRecord) string {
		return r.building + "|" + r.fuel
	})

	data := map[string]any{
		"circuits":        s.powerCircuits,
		"totalGenerators": len(s.generators),
		"generators":      describeGroups(generators),
	}
	if n := len(s.powerStorageCharges); n > 0 {
		var total float64
		for _, c := range s.powerStorageCharges {
			total += c
		}
		// mPowerStore is in MWh: a full PowerStorageMk1 (100 MWh capacity)
		// reads 99.99996 in the fixture.
		data["powerStorage"] = map[string]any{
			"count":          n,
			"totalStoredMWh": round2(total),
		}
	}
	return data
}
