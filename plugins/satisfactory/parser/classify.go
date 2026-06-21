package main

import "strings"

// machineStatus is the operational state inferred for a production machine.
// Exactly one applies to any machine.
type machineStatus string

const (
	statusBalanced      machineStatus = "balanced"       // producing at (or near) full capacity
	statusInputLimited  machineStatus = "input_limited"  // below capacity, constrained by a thin input
	statusOutputLimited machineStatus = "output_limited" // below capacity, constrained on the output side
	statusBlocked       machineStatus = "blocked_downstream"
	statusStarved       machineStatus = "starved_upstream"
	statusUnconfigured  machineStatus = "unconfigured"
	// statusIdle is the honest residual: not producing, with no detectable
	// blocked/starved cause. The save cannot tell us why (no power-outage
	// flag), so we do not assert one. A future power-corroboration step may
	// refine some idle machines to a likely_unpowered status.
	statusIdle machineStatus = "idle"
)

// throttledProductivityThreshold: a machine whose measured produce/duration
// ratio falls below this is running below capacity (input-thin or backed up
// on the output side) rather than at full output.
const throttledProductivityThreshold = 0.95

// diagnosis is a machine's status plus, when a single input is the binding
// constraint, that input's display name. limitingInput is set only for
// statusInputLimited and statusStarved; it is empty for every other status.
type diagnosis struct {
	status        machineStatus
	limitingInput string
}

// classifyMachine assigns one status to a manufacturer or extractor. It is the
// status-only view of diagnose, kept for callers that aggregate status counts
// and do not need the limiting input.
func classifyMachine(rec machineRecord, kind string) machineStatus {
	return diagnose(rec, kind).status
}

// diagnose assigns one status (and, where applicable, the limiting input) to a
// manufacturer or extractor from its resolved state. kind is "manufacturer" or
// "extractor"; generators are not handled here (fuel-burn logic differs).
//
// The "is it producing" decision keys on MEASURED PRODUCTIVITY over the last
// in-game window, not the instantaneous mIsProducing flag — that flag is false
// in cold/just-loaded saves even for healthy machines. mIsProducing is only a
// fallback when no measurement exists. A below-capacity machine is split into
// input_limited vs output_limited by buffer fill ratios; a fully stalled
// machine is diagnosed from its inventory (full output → blocked, missing
// ingredient → starved, otherwise the honest residual idle).
func diagnose(rec machineRecord, kind string) diagnosis {
	if kind == "manufacturer" && rec.recipe == "" {
		return diagnosis{status: statusUnconfigured}
	}

	if rec.productivity >= 0 {
		// Have a measurement: it is the source of truth.
		switch {
		case rec.productivity >= throttledProductivityThreshold:
			return diagnosis{status: statusBalanced}
		case rec.productivity > 0:
			return diagnoseThrottled(rec, kind)
		}
		// productivity == 0: not producing this window; fall through to diagnosis.
	} else if rec.producing {
		// No measurement: trust the instantaneous flag.
		return diagnosis{status: statusBalanced}
	}

	return diagnoseIdle(rec, kind)
}

// diagnoseThrottled splits a producing-but-below-capacity machine into
// input_limited vs output_limited. The binding side is whichever shows the
// greater observable constraint: the thinnest input's depletion (how far below
// one craft's worth it is buffered) versus the fullest output's fill fraction.
// Input wins only when STRICTLY more constrained, so ties and the
// all-plentiful case (inputs stocked, output not backed up) resolve to
// output_limited — when inputs are not short, the machine is not input-starved
// by definition, and no misleading limitingInput is emitted.
func diagnoseThrottled(rec machineRecord, kind string) diagnosis {
	if kind == "extractor" {
		// Extractors have no recipe inputs; the only observable constraint is
		// the output side.
		return diagnosis{status: statusOutputLimited}
	}
	spec, known := recipeIO[recipeClassName(rec.recipe)]
	if !known {
		// Unknown recipe (mod/future content): no IO to inspect.
		return diagnosis{status: statusOutputLimited}
	}
	item, inputConstraint := thinnestInput(spec.Ingredients, rec.inputContents)
	outputConstraint := maxOutputFullness(spec.Products, rec.outputContents)
	if inputConstraint > outputConstraint {
		return diagnosis{status: statusInputLimited, limitingInput: item}
	}
	return diagnosis{status: statusOutputLimited}
}

// thinnestInput finds the recipe ingredient with the least buffered relative to
// one craft's need and returns its display name plus an input-constraint score
// in [0,1]: 0 when at least one craft is buffered, rising to 1 when the input
// is empty. The score is 1 - min(1, bufferCount/perCraftNeed).
//
// Ratios are per-item (the same item's buffer count and recipe amount share a
// unit — solids ×1, fluids ×1000 on BOTH sides — verified against live saves),
// so the ratio is dimensionless and comparable across solid and fluid inputs.
// Ties break on the smaller class name for determinism.
func thinnestInput(ingredients []itemAmount, input []invStack) (string, float64) {
	worstClass := ""
	worstRatio := 0.0
	for _, ing := range ingredients {
		if ing.Amount <= 0 {
			continue
		}
		var have int64
		for _, st := range input {
			if classTail(st.itemClass) == ing.Class {
				have = st.count
				break
			}
		}
		ratio := float64(have) / float64(ing.Amount)
		if worstClass == "" || ratio < worstRatio ||
			(ratio == worstRatio && ing.Class < worstClass) {
			worstRatio, worstClass = ratio, ing.Class
		}
	}
	if worstClass == "" {
		return "", 0
	}
	if worstRatio > 1 {
		worstRatio = 1
	}
	return displayName(worstClass), 1 - worstRatio
}

// maxOutputFullness returns the highest fill fraction (count/stackMax, clamped
// to 1) across a recipe's products. Products with no known stack size cannot be
// judged and are skipped; a recipe with no judgeable output yields 0.
func maxOutputFullness(products []itemAmount, output []invStack) float64 {
	worst := 0.0
	for _, p := range products {
		stackMax, ok := itemStackSize[p.Class]
		if !ok || stackMax <= 0 {
			continue
		}
		for _, st := range output {
			if classTail(st.itemClass) == p.Class {
				f := float64(st.count) / float64(stackMax)
				if f > worst {
					worst = f
				}
			}
		}
	}
	if worst > 1 {
		worst = 1
	}
	return worst
}

// diagnoseIdle determines why a non-producing machine is idle.
func diagnoseIdle(rec machineRecord, kind string) diagnosis {
	if kind == "extractor" {
		if outputAtCapacity(rec.outputContents) {
			return diagnosis{status: statusBlocked}
		}
		return diagnosis{status: statusIdle}
	}
	if spec, known := recipeIO[recipeClassName(rec.recipe)]; known {
		if anyProductFull(spec.Products, rec.outputContents) {
			return diagnosis{status: statusBlocked}
		}
		if missing := firstAbsentIngredient(spec.Ingredients, rec.inputContents); missing != "" {
			return diagnosis{status: statusStarved, limitingInput: displayName(missing)}
		}
	}
	return diagnosis{status: statusIdle}
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
		stackMax, ok := itemStackSize[p.Class]
		if !ok {
			continue
		}
		for _, st := range output {
			if classTail(st.itemClass) == p.Class && st.count >= int64(stackMax) {
				return true
			}
		}
	}
	return false
}

// firstAbsentIngredient returns the short class name of the first recipe
// ingredient (in recipe order) entirely missing from the input inventory, or
// "" if all are present. Absence (not quantity) is the test, so it stays
// correct for fluids whose amounts are m³-scaled.
func firstAbsentIngredient(ingredients []itemAmount, input []invStack) string {
	for _, ing := range ingredients {
		found := false
		for _, st := range input {
			if classTail(st.itemClass) == ing.Class && st.count > 0 {
				found = true
				break
			}
		}
		if !found {
			return ing.Class
		}
	}
	return ""
}

// outputAtCapacity reports whether any output stack is at or beyond its item's
// stack limit — used for extractors, which have no recipe.
func outputAtCapacity(output []invStack) bool {
	for _, st := range output {
		stackMax, ok := itemStackSize[classTail(st.itemClass)]
		if ok && st.count >= int64(stackMax) {
			return true
		}
	}
	return false
}
