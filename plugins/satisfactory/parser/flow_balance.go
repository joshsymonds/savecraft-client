package main

import (
	"math"
	"sort"
)

// maxFlowItems caps the per-base item list in flow_balance, to keep output
// bounded on megafactories. Items are kept most-imbalanced first.
const maxFlowItems = 50

// recipeRate aggregates one recipe's contribution to an item's flow within a
// base: how many machines run it and their combined per-minute rate.
type recipeRate struct {
	machines int
	rate     float64
}

// itemFlow accumulates one item's supply and demand within a base.
type itemFlow struct {
	producedRated    float64
	producedMeasured float64
	consumedRated    float64
	producers        map[string]*recipeRate // recipe display name -> aggregate
	consumers        map[string]*recipeRate
	rawSupplied      bool // an in-base extractor outputs this item
}

func newItemFlow() *itemFlow {
	return &itemFlow{producers: map[string]*recipeRate{}, consumers: map[string]*recipeRate{}}
}

func addRate(m map[string]*recipeRate, recipe string, rate float64) {
	rr := m[recipe]
	if rr == nil {
		rr = &recipeRate{}
		m[recipe] = rr
	}
	rr.machines++
	rr.rate += rate
}

// buildFlowBalanceSection reports, per geography base, each item's supply vs
// demand vs buffer — the per-base flow model. Production/consumption rates are
// computed from recipe durations (somersloop boost amplifies output only;
// consumption is clock-only). An item a base also extracts is flagged
// rawSupplied so a negative net reads as "fed by mining", not a shortfall.
func (s *saveState) buildFlowBalanceSection() map[string]any {
	idx := s.bases()
	storage := s.storageBucketsByBaseID()

	bases := make([]map[string]any, 0, len(idx.bases))
	for id, members := range idx.bases {
		flows := map[string]*itemFlow{}
		flow := func(item string) *itemFlow {
			f := flows[item]
			if f == nil {
				f = newItemFlow()
				flows[item] = f
			}
			return f
		}

		for _, m := range members {
			switch m.kind {
			case "extractor":
				if len(m.outputContents) > 0 {
					flow(classTail(m.outputContents[0].itemClass)).rawSupplied = true
				}
			case "manufacturer":
				accumulateMachineFlow(m, flow)
			}
		}

		// Buffer is the in-base storage stock, keyed by the recipe's short item
		// class (storage stores full paths, recipes use the short class).
		buffer := map[string]int64{}
		for cls, count := range storage[id] {
			buffer[classTail(cls)] += count
		}

		items, omitted := flowItemList(flows, buffer)
		if len(items) == 0 {
			continue
		}
		base := map[string]any{
			"base":  baseName(members, s.mapMarkers),
			"items": items,
		}
		if omitted > 0 {
			base["itemsOmitted"] = omitted
		}
		bases = append(bases, base)
	}

	return map[string]any{"bases": bases}
}

// accumulateMachineFlow folds one manufacturer's recipe into the per-item flows.
func accumulateMachineFlow(m machineRecord, flow func(string) *itemFlow) {
	if m.recipe == "" {
		return
	}
	spec, ok := recipeIO[recipeClassName(m.recipe)]
	if !ok {
		return
	}
	recipe := displayName(m.recipe)
	measured := 1.0
	if m.productivity >= 0 {
		measured = m.productivity
	}
	for _, p := range spec.Products {
		rate := outputPerMin(spec, p.Class, m.clock, m.boost)
		f := flow(p.Class)
		f.producedRated += rate
		f.producedMeasured += rate * measured
		addRate(f.producers, recipe, rate)
	}
	for _, ing := range spec.Ingredients {
		rate := inputPerMin(spec, ing.Class, m.clock)
		f := flow(ing.Class)
		f.consumedRated += rate
		addRate(f.consumers, recipe, rate)
	}
}

// flowItemList renders the items with any production or consumption, ordered by
// most imbalanced (|net|) first and capped at maxFlowItems; returns the entries
// and how many were omitted by the cap.
func flowItemList(flows map[string]*itemFlow, buffer map[string]int64) ([]map[string]any, int) {
	type ranked struct {
		item string
		f    *itemFlow
		net  float64
	}
	list := make([]ranked, 0, len(flows))
	for item, f := range flows {
		if f.producedRated == 0 && f.consumedRated == 0 {
			continue // rawSupplied-only with no in-base flow: nothing to report
		}
		list = append(list, ranked{item, f, f.producedRated - f.consumedRated})
	}
	sort.Slice(list, func(i, j int) bool {
		ai, aj := math.Abs(list[i].net), math.Abs(list[j].net)
		if ai != aj {
			return ai > aj
		}
		return list[i].item < list[j].item
	})

	omitted := 0
	if len(list) > maxFlowItems {
		omitted = len(list) - maxFlowItems
		list = list[:maxFlowItems]
	}

	out := make([]map[string]any, 0, len(list))
	for _, r := range list {
		out = append(out, flowItemEntry(r.item, r.f, buffer[r.item]))
	}
	return out, omitted
}

func flowItemEntry(item string, f *itemFlow, buffer int64) map[string]any {
	entry := map[string]any{
		"item":           displayName(item),
		"classPath":      item,
		"producedPerMin": round2(f.producedRated),
		"measuredPerMin": round2(f.producedMeasured),
		"consumedPerMin": round2(f.consumedRated),
		"net":            round2(f.producedRated - f.consumedRated),
		"buffer":         buffer,
	}
	if f.rawSupplied {
		entry["rawSupplied"] = true
	}
	if len(f.producers) > 0 {
		entry["producers"] = rateList(f.producers)
	}
	if len(f.consumers) > 0 {
		entry["consumers"] = rateList(f.consumers)
	}
	return entry
}

// rateList renders aggregated recipe contributions, highest rate first.
func rateList(m map[string]*recipeRate) []map[string]any {
	type rr struct {
		recipe   string
		machines int
		rate     float64
	}
	list := make([]rr, 0, len(m))
	for recipe, r := range m {
		list = append(list, rr{recipe, r.machines, r.rate})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].rate != list[j].rate {
			return list[i].rate > list[j].rate
		}
		return list[i].recipe < list[j].recipe
	})
	out := make([]map[string]any, 0, len(list))
	for _, r := range list {
		out = append(out, map[string]any{
			"recipe": r.recipe, "machines": r.machines, "ratePerMin": round2(r.rate),
		})
	}
	return out
}
