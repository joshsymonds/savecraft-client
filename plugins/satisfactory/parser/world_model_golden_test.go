package main

import (
	"strings"
	"testing"
)

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
