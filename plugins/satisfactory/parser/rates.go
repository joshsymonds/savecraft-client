package main

// Per-minute flow rates for a machine running a recipe, computed deterministically
// from the recipe's craft duration. The formula mirrors the reference layer's
// perMinute (reference/lookup.go): amount × 60 / DurationSec, then scaled by the
// machine's clock. Rates are in the recipe's own Amount units per minute (fluids
// stay ×1000-scaled; display-side normalization is the caller's concern).

// ratePerMin converts a per-craft amount into a per-minute flow for a machine
// running at the given clock, scaled by mult (somersloop boost for outputs, 1
// for inputs). A recipe with no positive duration yields 0 (no divide-by-zero).
func ratePerMin(amount int, durationSec, clock, mult float64) float64 {
	if durationSec <= 0 {
		return 0
	}
	return float64(amount) * 60 / durationSec * clock * mult
}

// outputPerMin is the per-minute production of itemClass by a machine running
// spec at the given clock and somersloop boost. Boost amplifies output only.
// Returns 0 if the recipe does not produce the item. itemClass may be a short
// class or a full path (both reduce to the short class for matching).
func outputPerMin(spec recipeSpec, itemClass string, clock, boost float64) float64 {
	want := classTail(itemClass)
	for _, p := range spec.Products {
		if p.Class == want {
			return ratePerMin(p.Amount, spec.DurationSec, clock, boost)
		}
	}
	return 0
}

// inputPerMin is the per-minute consumption of itemClass by a machine running
// spec at the given clock. Somersloop boost does NOT increase input draw, so it
// is intentionally absent. Returns 0 if the recipe does not consume the item.
func inputPerMin(spec recipeSpec, itemClass string, clock float64) float64 {
	want := classTail(itemClass)
	for _, ing := range spec.Ingredients {
		if ing.Class == want {
			return ratePerMin(ing.Amount, spec.DurationSec, clock, 1)
		}
	}
	return 0
}
