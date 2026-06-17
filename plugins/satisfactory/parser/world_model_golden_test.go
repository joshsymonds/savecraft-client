package main

import (
	"strings"
	"testing"
)

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
