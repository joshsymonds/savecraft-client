package main

import (
	"strings"
	"testing"
)

// On a real save, the connection adjacency contracts into multiple production
// lines: storage containers act as boundaries (so the factory is NOT one giant
// component), every line has a machine, and no machine is double-counted.
func TestGoldenProductionLines(t *testing.T) {
	state := parseFixtureSections(t, "current_sv60.sav")
	machines := map[string]bool{}
	for _, r := range state.manufacturers {
		machines[r.instance] = true
	}
	for _, r := range state.extractors {
		machines[r.instance] = true
	}
	for _, r := range state.generators {
		machines[r.instance] = true
	}

	lines := buildProductionLines(state.connEdges, machines)
	if len(lines) == 0 {
		t.Fatal("no production lines built")
	}
	t.Logf("current_sv60.sav: %d production lines from %d machines", len(lines), len(machines))

	seen := map[string]bool{}
	for _, l := range lines {
		if len(l.machines) == 0 {
			t.Errorf("line with no machine: %+v", l)
		}
		for _, m := range l.machines {
			if seen[m] {
				t.Errorf("machine %s in more than one line", m)
			}
			seen[m] = true
		}
	}
	if len(seen) > len(machines) {
		t.Errorf("distinct line machines %d > total machines %d", len(seen), len(machines))
	}
	// Storage boundaries must split the factory into more than one line, not
	// merge everything into a single component.
	if len(lines) < 2 {
		t.Errorf("expected >1 line (storage boundaries split the factory), got %d", len(lines))
	}
}

// On a real save, connection edges are captured and at least one edge touches
// a known production machine.
func TestGoldenConnectionAdjacency(t *testing.T) {
	state := parseFixtureSections(t, "current_sv60.sav")
	if len(state.connEdges) == 0 {
		t.Fatal("no connection edges extracted")
	}

	machines := map[string]bool{}
	for _, r := range state.manufacturers {
		machines[r.instance] = true
	}
	for _, r := range state.extractors {
		machines[r.instance] = true
	}

	touchesMachine := false
	for _, e := range state.connEdges {
		if e.from == "" || e.to == "" {
			t.Errorf("edge with empty endpoint: %+v", e)
		}
		if machines[e.from] || machines[e.to] {
			touchesMachine = true
		}
	}
	if !touchesMachine {
		t.Error("no connection edge touches a known manufacturer or extractor")
	}
}

// On a real save the machines section's manufacturer groups carry a status
// breakdown; per group the breakdown sums to the group count, group counts sum
// to totalManufacturers, and at least one non-producing status appears.
func TestGoldenMachinesStatusConsistency(t *testing.T) {
	state := parseFixtureSections(t, "current_sv60.sav")
	data := state.buildMachinesSection()

	total, ok := data["totalManufacturers"].(int)
	if !ok || total == 0 {
		t.Fatalf("totalManufacturers = %v, want > 0", data["totalManufacturers"])
	}
	groups, _ := data["manufacturers"].([]map[string]any)

	sumCounts := 0
	tally := map[machineStatus]int{}
	for _, g := range groups {
		count, _ := g["count"].(int)
		sumCounts += count
		status, has := g["status"].(map[machineStatus]int)
		if !has {
			continue // fully-producing group
		}
		groupSum := 0
		for st, n := range status {
			groupSum += n
			tally[st] += n
		}
		if groupSum != count {
			t.Errorf("group %v: status sum %d != count %d", g["recipe"], groupSum, count)
		}
	}
	if sumCounts != total {
		t.Errorf("Σ group counts %d != totalManufacturers %d", sumCounts, total)
	}
	// sv60 was saved while the factory was stopped (productivity 0 across the
	// board) → machines read as idle, never as a fabricated power state.
	if tally[statusIdle] == 0 {
		t.Errorf("expected idle machines in current_sv60.sav, got tally %v", tally)
	}
	if got := tally[machineStatus("likely_unpowered")]; got != 0 {
		t.Errorf("expected zero likely_unpowered (not power-corroborated), got %d", got)
	}
}

// Ground truth probed from current_sv60.sav: the screw constructor
// Build_ConstructorMk1_C_2147283346 sits at (-249734,-55302,-271), draws
// Iron Rod, and its output inventory holds 144 Iron Screws.
func TestGoldenWorldModelInventoryJoin(t *testing.T) {
	state := parseFixtureSections(t, "current_sv60.sav")

	var rec *machineRecord
	for i := range state.manufacturers {
		if strings.Contains(state.manufacturers[i].instance, "Build_ConstructorMk1_C_2147283346") {
			rec = &state.manufacturers[i]
			break
		}
	}
	if rec == nil {
		t.Fatal("screw constructor Build_ConstructorMk1_C_2147283346 not found among manufacturers")
	}

	if rec.position == [3]float32{0, 0, 0} {
		t.Errorf("position = %v, want non-zero", rec.position)
	}
	if !strings.Contains(rec.recipe, "Recipe_Screw") {
		t.Errorf("recipe = %q, want Recipe_Screw", rec.recipe)
	}

	var screws int64
	for _, st := range rec.outputContents {
		if strings.Contains(st.itemClass, "Desc_IronScrew") {
			screws = st.count
		}
	}
	if screws != 144 {
		t.Errorf("output iron screws = %d, want 144 (outputContents=%+v)", screws, rec.outputContents)
	}
}
