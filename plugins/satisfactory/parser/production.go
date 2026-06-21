package main

// itemAmount is one item class and a per-craft quantity. Fluid amounts are
// in m³-scaled units (Docs.json reports fluids ×1000 vs solids); the idle
// classifier treats fluid buffers specially rather than comparing raw counts.
type itemAmount struct {
	Class  string
	Amount int
}

// recipeSpec is a recipe's inputs, outputs, and craft duration. The classifier
// uses the IO to decide whether a stalled machine is starved (a required
// ingredient is absent) or blocked (a product fills its output); DurationSec
// (seconds per craft at 100% clock) drives the flow-balance per-minute rates.
type recipeSpec struct {
	Ingredients []itemAmount
	Products    []itemAmount
	DurationSec float64
}

// itemStackSize (item class → max stack) and recipeIO (recipe class →
// recipeSpec) are generated into production_gen.go by tools/datagen.
