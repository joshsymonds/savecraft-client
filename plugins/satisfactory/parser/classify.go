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
	// statusUnpowered is INFERRED by elimination — the save does not store a
	// power-outage flag. It means: idle, with a recipe set, inputs present,
	// and output not full, so the most likely cause is missing power.
	statusUnpowered machineStatus = "likely_unpowered"
)

// throttledProductivityThreshold: a producing machine whose measured
// produce/duration ratio falls below this is running below capacity
// (intermittent starvation or backup) rather than at full output.
const throttledProductivityThreshold = 0.95

// classifyMachine assigns one status to a manufacturer or extractor from its
// resolved state. kind is "manufacturer" or "extractor"; generators are not
// handled here (fuel-burn logic differs).
//
// The save records mIsProducing but never WHY a machine is idle, so an idle
// machine is diagnosed from its inventory: a full output means downstream is
// backed up; a missing ingredient means upstream is starved; otherwise the
// residual cause is most likely power (statusUnpowered — an inference).
func classifyMachine(rec machineRecord, kind string) machineStatus {
	if kind == "manufacturer" && rec.recipe == "" {
		return statusUnconfigured
	}
	if rec.producing {
		if rec.productivity >= 0 && rec.productivity < throttledProductivityThreshold {
			return statusThrottled
		}
		return statusProducing
	}

	// Idle. Diagnose the cause.
	if kind == "extractor" {
		if outputAtCapacity(rec.outputContents) {
			return statusBlocked
		}
		return statusUnpowered
	}

	spec, known := recipeIO[recipeClassName(rec.recipe)]
	if known {
		if anyProductFull(spec.Products, rec.outputContents) {
			return statusBlocked
		}
		if anyIngredientAbsent(spec.Ingredients, rec.inputContents) {
			return statusStarved
		}
	}
	return statusUnpowered
}

// recipeClassName extracts the recipe class (e.g. "Recipe_Screw_C") from a
// full class path; recipeIO and itemStackSize are keyed by that short name.
func recipeClassName(path string) string {
	return path[strings.LastIndex(path, ".")+1:]
}

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
			if st.itemClass == p.Class && st.count >= int64(max) {
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
			if st.itemClass == ing.Class && st.count > 0 {
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
		max, ok := itemStackSize[st.itemClass]
		if ok && st.count >= int64(max) {
			return true
		}
	}
	return false
}
