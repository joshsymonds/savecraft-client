package main

// maxLineProblems caps how many problem machines a single line enumerates, to
// keep the section's output bounded on large factories.
const maxLineProblems = 25

// lineGroup aggregates a production line's machines by building + recipe.
type lineGroup struct {
	building string
	recipe   string
	count    int
	status   map[machineStatus]int
}

// dominantDescriptor labels a line by the recipe (or building, for
// recipe-less extractors) of its largest machine group, so two lines sharing
// the same nearest marker still get distinct names (e.g. "Motor" vs "Screws").
func dominantDescriptor(groups map[string]*lineGroup, order []string) string {
	best := ""
	bestCount := -1
	for _, k := range order {
		g := groups[k]
		label := displayName(g.building)
		if g.recipe != "" {
			label = displayName(g.recipe)
		}
		if g.count > bestCount || (g.count == bestCount && label < best) {
			best, bestCount = label, g.count
		}
	}
	return best
}

func mapHasIdle(status map[machineStatus]int) bool {
	for st, n := range status {
		if st != statusBalanced && n > 0 {
			return true
		}
	}
	return false
}

// isProblem reports whether a status is worth calling out per-machine on a
// line: a stall (blocked/starved/idle) or a below-capacity producer
// (input/output limited). Balanced and unconfigured machines are not problems.
func isProblem(st machineStatus) bool {
	switch st {
	case statusBlocked, statusStarved, statusIdle, statusInputLimited, statusOutputLimited:
		return true
	case statusBalanced, statusUnconfigured:
		return false
	}
	return false
}

func posMap(p [3]float32) map[string]any {
	return map[string]any{"x": p[0], "y": p[1], "z": p[2]}
}

// terminalSummary groups a line's boundary terminals by building type.
func terminalSummary(terminals []string) []map[string]any {
	counts := map[string]int{}
	order := []string{}
	for _, t := range terminals {
		name := displayName(classOf(t))
		if counts[name] == 0 {
			order = append(order, name)
		}
		counts[name]++
	}
	out := make([]map[string]any, 0, len(order))
	for _, name := range order {
		out = append(out, map[string]any{"type": name, "count": counts[name]})
	}
	return out
}

// lineDelivery computes a line's inbound belt throughput ceiling — the slowest
// belt feeding the line's machine inputs — and a delivery-limited flag when the
// line's largest externally-supplied item rate exceeds that ceiling. The
// ceiling is read from feederMinByMachine (machine instance → slowest inbound
// belt feeding it), precomputed once by the caller. Returns the ceiling (and
// whether one was found) plus the flag map (nil when within ceiling, no feeder,
// or no external demand).
//
// The flag is conservative: it cannot map a specific item to a specific belt
// (that needs belt contents), so it compares the heaviest required external
// feed against the slowest inbound belt. A machine fed via a splitter has no
// direct belt→machine edge, so its line may report no ceiling.
func lineDelivery(
	machines []string,
	byInstance map[string]*machineRecord,
	feederMinByMachine map[string]float64,
) (ceiling float64, hasCeiling bool, limited map[string]any) {
	for _, mi := range machines {
		if t, ok := feederMinByMachine[mi]; ok {
			if !hasCeiling || t < ceiling {
				ceiling, hasCeiling = t, true
			}
		}
	}

	produced := map[string]float64{}
	consumed := map[string]float64{}
	for _, mi := range machines {
		rec := byInstance[mi]
		if rec == nil || rec.kind != "manufacturer" || rec.recipe == "" {
			continue
		}
		spec, ok := recipeIO[recipeClassName(rec.recipe)]
		if !ok {
			continue
		}
		for _, p := range spec.Products {
			produced[p.Class] += outputPerMin(spec, p.Class, rec.clock, rec.boost)
		}
		for _, ing := range spec.Ingredients {
			consumed[ing.Class] += inputPerMin(spec, ing.Class, rec.clock)
		}
	}

	worstItem, worstRate := "", 0.0
	for item, c := range consumed {
		if demand := c - produced[item]; demand > worstRate {
			worstItem, worstRate = item, demand
		}
	}

	if hasCeiling && worstRate > ceiling {
		limited = map[string]any{
			"item":           displayName(worstItem),
			"requiredPerMin": round2(worstRate),
			"beltCeiling":    ceiling,
		}
	}
	return ceiling, hasCeiling, limited
}

func (s *saveState) buildProductionLinesSection() map[string]any {
	byInstance := map[string]*machineRecord{}
	machineSet := map[string]bool{}
	index := func(recs []machineRecord) {
		for i := range recs {
			byInstance[recs[i].instance] = &recs[i]
			machineSet[recs[i].instance] = true
		}
	}
	index(s.manufacturers)
	index(s.extractors)
	index(s.generators)

	beltThroughputByInstance := make(map[string]float64, len(s.belts))
	for _, b := range s.belts {
		beltThroughputByInstance[b.instance] = b.throughput
	}

	// feederMinByMachine maps a machine instance to the slowest belt feeding its
	// inputs (a directed belt→machine edge). Built in one pass over the edges so
	// per-line delivery analysis is O(machines), not O(lines × edges).
	feederMinByMachine := map[string]float64{}
	for _, e := range s.connEdges {
		if !e.directed || e.transport != "belt" {
			continue
		}
		t, ok := beltThroughputByInstance[e.from]
		if !ok || t <= 0 {
			continue
		}
		if cur, seen := feederMinByMachine[e.to]; !seen || t < cur {
			feederMinByMachine[e.to] = t
		}
	}

	lines := buildProductionLines(s.connEdges, machineSet)

	inLine := map[string]bool{}
	out := make([]map[string]any, 0, len(lines))
	for _, l := range lines {
		var positions [][3]float32
		groups := map[string]*lineGroup{}
		order := []string{}
		var problems []map[string]any
		omitted := 0

		for _, mi := range l.machines {
			inLine[mi] = true
			rec := byInstance[mi]
			if rec == nil {
				continue
			}
			positions = append(positions, rec.position)

			key := rec.building + "|" + rec.recipe
			g := groups[key]
			if g == nil {
				g = &lineGroup{
					building: rec.building,
					recipe:   rec.recipe,
					status:   map[machineStatus]int{},
				}
				groups[key] = g
				order = append(order, key)
			}
			g.count++

			if rec.kind != "manufacturer" && rec.kind != "extractor" {
				continue // generators are not status-classified
			}
			d := diagnose(*rec, rec.kind)
			g.status[d.status]++
			if isProblem(d.status) {
				if len(problems) >= maxLineProblems {
					omitted++
					continue
				}
				problem := map[string]any{
					"building": displayName(rec.building),
					"status":   string(d.status),
					"position": posMap(rec.position),
				}
				if rec.recipe != "" {
					problem["recipe"] = displayName(rec.recipe)
				}
				if d.limitingInput != "" {
					problem["limitingInput"] = d.limitingInput
				}
				problems = append(problems, problem)
			}
		}

		c := centroid(positions)
		recipes := make([]map[string]any, 0, len(order))
		for _, k := range order {
			g := groups[k]
			entry := map[string]any{"building": displayName(g.building), "count": g.count}
			if g.recipe != "" {
				entry["recipe"] = displayName(g.recipe)
			}
			if mapHasIdle(g.status) {
				entry["status"] = g.status
			}
			recipes = append(recipes, entry)
		}

		line := map[string]any{
			"name": dominantDescriptor(
				groups,
				order,
			) + " " + regionName(
				float64(c[0]),
				float64(c[1]),
				s.mapMarkers,
			),
			"machineCount": len(l.machines),
			"recipes":      recipes,
		}
		if len(l.transports) > 0 {
			line["transports"] = l.transports
		}
		if terms := terminalSummary(l.terminals); len(terms) > 0 {
			line["terminals"] = terms
		}
		if len(problems) > 0 {
			line["problems"] = problems
		}
		if omitted > 0 {
			line["problemsOmitted"] = omitted
		}
		if ceiling, ok, limited := lineDelivery(
			l.machines,
			byInstance,
			feederMinByMachine,
		); ok {
			line["inboundBeltCeiling"] = ceiling
			if limited != nil {
				line["deliveryLimited"] = limited
			}
		}
		out = append(out, line)
	}

	return map[string]any{
		"lineCount":           len(lines),
		"lines":               out,
		"unconnectedMachines": len(machineSet) - len(inLine),
	}
}
