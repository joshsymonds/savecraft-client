package main

import "strings"

// machineStatus is the operational state inferred for a production machine.
// Exactly one applies to any machine.
type machineStatus string

const (
	statusProducing    machineStatus = "producing"
	statusThrottled    machineStatus = "throttled"
	statusBlocked      machineStatus = "blocked_downstream"
	statusStarved      machineStatus = "starved_upstream"
	statusUnconfigured machineStatus = "unconfigured"
	// statusIdle is the honest residual: not producing, with no detectable
	// blocked/starved cause. The save cannot tell us why (no power-outage
	// flag), so we do not assert one. A future power-corroboration step may
	// refine some idle machines to a likely_unpowered status.
	statusIdle machineStatus = "idle"
)

// throttledProductivityThreshold: a machine whose measured produce/duration
// ratio falls below this is running below capacity (intermittent starvation
// or backup) rather than at full output.
const throttledProductivityThreshold = 0.95

// classifyMachine assigns one status to a manufacturer or extractor from its
// resolved state. kind is "manufacturer" or "extractor"; generators are not
// handled here (fuel-burn logic differs).
//
// The "is it producing" decision keys on MEASURED PRODUCTIVITY over the last
// in-game window, not the instantaneous mIsProducing flag — that flag is false
// in cold/just-loaded saves even for healthy machines. mIsProducing is only a
// fallback when no measurement exists. An idle machine is then diagnosed from
// its inventory: a full output means downstream is backed up; a missing
// ingredient means upstream is starved; otherwise the cause is undetermined
// (statusIdle — never an asserted power state).
func classifyMachine(rec machineRecord, kind string) machineStatus {
	if kind == "manufacturer" && rec.recipe == "" {
		return statusUnconfigured
	}

	if rec.productivity >= 0 {
		// Have a measurement: it is the source of truth.
		switch {
		case rec.productivity >= throttledProductivityThreshold:
			return statusProducing
		case rec.productivity > 0:
			return statusThrottled
		}
		// productivity == 0: not producing this window; fall through to diagnosis.
	} else if rec.producing {
		// No measurement: trust the instantaneous flag.
		return statusProducing
	}

	return diagnoseIdle(rec, kind)
}

// diagnoseIdle determines why a non-producing machine is idle.
func diagnoseIdle(rec machineRecord, kind string) machineStatus {
	if kind == "extractor" {
		if outputAtCapacity(rec.outputContents) {
			return statusBlocked
		}
		return statusIdle
	}
	if spec, known := recipeIO[recipeClassName(rec.recipe)]; known {
		if anyProductFull(spec.Products, rec.outputContents) {
			return statusBlocked
		}
		if anyIngredientAbsent(spec.Ingredients, rec.inputContents) {
			return statusStarved
		}
	}
	return statusIdle
}

// classTail extracts the short class name (e.g. "Recipe_Screw_C",
// "Desc_IronScrew_C") from a full class path. recipeIO and itemStackSize are
// keyed by that short name, but inventory stacks carry the full path, so both
// sides are reduced to the tail before comparison.
func classTail(path string) string {
	return path[strings.LastIndex(path, ".")+1:]
}

// recipeClassName is classTail applied to a recipe path.
func recipeClassName(path string) string { return classTail(path) }

// anyProductFull reports whether any recipe product occupies its output slot
// at or beyond the item's stack limit (so the machine cannot push more). An
// item with no known stack size cannot be judged full.
func anyProductFull(products []itemAmount, output []invStack) bool {
	for _, p := range products {
		max, ok := itemStackSize[p.Class]
		if !ok {
			continue
		}
		for _, st := range output {
			if classTail(st.itemClass) == p.Class && st.count >= int64(max) {
				return true
			}
		}
	}
	return false
}

// anyIngredientAbsent reports whether any recipe ingredient is entirely
// missing from the input inventory. Absence (not quantity) is the test, so it
// stays correct for fluids whose amounts are m³-scaled.
func anyIngredientAbsent(ingredients []itemAmount, input []invStack) bool {
	for _, ing := range ingredients {
		found := false
		for _, st := range input {
			if classTail(st.itemClass) == ing.Class && st.count > 0 {
				found = true
				break
			}
		}
		if !found {
			return true
		}
	}
	return false
}

// outputAtCapacity reports whether any output stack is at or beyond its item's
// stack limit — used for extractors, which have no recipe.
func outputAtCapacity(output []invStack) bool {
	for _, st := range output {
		max, ok := itemStackSize[classTail(st.itemClass)]
		if ok && st.count >= int64(max) {
			return true
		}
	}
	return false
}
