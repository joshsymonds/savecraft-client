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

func mapHasIdle(status map[machineStatus]int) bool {
	for st, n := range status {
		if st != statusProducing && n > 0 {
			return true
		}
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
				g = &lineGroup{building: rec.building, recipe: rec.recipe, status: map[machineStatus]int{}}
				groups[key] = g
				order = append(order, key)
			}
			g.count++

			if rec.kind != "manufacturer" && rec.kind != "extractor" {
				continue // generators are not status-classified
			}
			st := classifyMachine(*rec, rec.kind)
			g.status[st]++
			if st == statusBlocked || st == statusStarved || st == statusIdle {
				if len(problems) >= maxLineProblems {
					omitted++
					continue
				}
				problem := map[string]any{
					"building": displayName(rec.building),
					"status":   string(st),
					"position": posMap(rec.position),
				}
				if rec.recipe != "" {
					problem["recipe"] = displayName(rec.recipe)
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
			"name":         regionName(float64(c[0]), float64(c[1]), s.mapMarkers),
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
		out = append(out, line)
	}

	return map[string]any{
		"lineCount":           len(lines),
		"lines":               out,
		"unconnectedMachines": len(machineSet) - len(inLine),
	}
}
