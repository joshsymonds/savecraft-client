package main

import "testing"

// cableSpec mirrors the generated recipeIO entry: 2 Wire → 1 Cable in 2s.
var cableSpec = recipeSpec{
	Ingredients: []itemAmount{{Class: "Desc_Wire_C", Amount: 2}},
	Products:    []itemAmount{{Class: "Desc_Cable_C", Amount: 1}},
	DurationSec: 2,
}

func TestGeneratedRecipeHasDuration(t *testing.T) {
	// The regenerated table must carry DurationSec, not just IO.
	if got := recipeIO["Recipe_Cable_C"].DurationSec; got != 2 {
		t.Errorf("Recipe_Cable_C DurationSec = %v, want 2", got)
	}
	if got := recipeIO["Recipe_Screw_C"].DurationSec; got <= 0 {
		t.Errorf("Recipe_Screw_C DurationSec = %v, want > 0", got)
	}
}

func TestOutputPerMin(t *testing.T) {
	cases := []struct {
		name               string
		item               string
		clock, boost, want float64
	}{
		// 1 cable × 60/2 × clock × boost.
		{"100% clock, no boost", "Desc_Cable_C", 1.0, 1.0, 30},
		{"50% clock", "Desc_Cable_C", 0.5, 1.0, 15},
		{"slooped: boost doubles output", "Desc_Cable_C", 1.0, 2.0, 60},
		{"full class path matches", "/Game/X/Desc_Cable.Desc_Cable_C", 1.0, 1.0, 30},
		{"item not produced", "Desc_Wire_C", 1.0, 1.0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := outputPerMin(cableSpec, tc.item, tc.clock, tc.boost); got != tc.want {
				t.Errorf("outputPerMin = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestInputPerMin(t *testing.T) {
	cases := []struct {
		name        string
		item        string
		clock, want float64
	}{
		// 2 wire × 60/2 × clock.
		{"100% clock", "Desc_Wire_C", 1.0, 60},
		{"50% clock", "Desc_Wire_C", 0.5, 30},
		{"item not consumed", "Desc_Cable_C", 1.0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := inputPerMin(cableSpec, tc.item, tc.clock); got != tc.want {
				t.Errorf("inputPerMin = %v, want %v", got, tc.want)
			}
		})
	}
}

// Somersloop amplifies output only — input draw is unchanged by boost.
func TestBoostDoesNotAffectInput(t *testing.T) {
	plain := inputPerMin(cableSpec, "Desc_Wire_C", 1.0)
	// inputPerMin has no boost parameter; a slooped machine draws the same input.
	if plain != 60 {
		t.Fatalf("input draw = %v, want 60 regardless of boost", plain)
	}
	if out := outputPerMin(
		cableSpec,
		"Desc_Cable_C",
		1.0,
		2.0,
	); out != 2*outputPerMin(
		cableSpec,
		"Desc_Cable_C",
		1.0,
		1.0,
	) {
		t.Errorf("boost should double output: got %v", out)
	}
}

// DurationSec == 0 must yield 0, never divide-by-zero.
func TestZeroDurationGuard(t *testing.T) {
	bad := recipeSpec{
		Ingredients: []itemAmount{{Class: "Desc_Wire_C", Amount: 2}},
		Products:    []itemAmount{{Class: "Desc_Cable_C", Amount: 1}},
		DurationSec: 0,
	}
	if got := outputPerMin(bad, "Desc_Cable_C", 1.0, 1.0); got != 0 {
		t.Errorf("outputPerMin with 0 duration = %v, want 0", got)
	}
	if got := inputPerMin(bad, "Desc_Wire_C", 1.0); got != 0 {
		t.Errorf("inputPerMin with 0 duration = %v, want 0", got)
	}
}
