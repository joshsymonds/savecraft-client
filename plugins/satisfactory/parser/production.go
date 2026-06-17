package main

// itemAmount is one item class and a per-craft quantity. Fluid amounts are
// in m³-scaled units (Docs.json reports fluids ×1000 vs solids); the idle
// classifier treats fluid buffers specially rather than comparing raw counts.
type itemAmount struct {
	Class  string
	Amount int
}

// recipeSpec is a recipe's inputs and outputs, used by the idle classifier to
// decide whether a stalled machine is starved (a required ingredient is
// absent from its input inventory) or blocked (a product fills its output).
type recipeSpec struct {
	Ingredients []itemAmount
	Products    []itemAmount
}

// itemStackSize (item class → max stack) and recipeIO (recipe class →
// recipeSpec) are generated into production_gen.go by tools/datagen.
