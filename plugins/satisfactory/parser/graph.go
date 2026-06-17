package main

import (
	"regexp"
	"sort"
	"strings"
)

// productionLine is a connected component of production machines linked through
// pass-through logistics (belts/pipes/splitters/…). terminals are the boundary
// nodes (storage, tanks, sinks, train docking stations) the line attaches to;
// transports lists the link types present ("belt", "pipe").
type productionLine struct {
	machines   []string
	terminals  []string
	transports []string
}

var trailingID = regexp.MustCompile(`_\d+$`)

// classOf reduces an actor instance path to its class name, e.g.
// "…PersistentLevel.Build_ConveyorBeltMk1_C_42" → "Build_ConveyorBeltMk1_C".
func classOf(instance string) string {
	last := instance[strings.LastIndex(instance, ".")+1:]
	return trailingID.ReplaceAllString(last, "")
}

// passthroughPrefixes are the logistics classes the graph contracts THROUGH —
// they carry flow between machines but are not endpoints. Everything else that
// is not a production machine is a boundary (storage, tanks, sinks, train
// docking stations), so an unknown new building defaults to a boundary and
// never wrongly merges two lines.
var passthroughPrefixes = []string{
	"Build_ConveyorBelt", "Build_ConveyorLift", "Build_ConveyorAttachment",
	"Build_ConveyorPole", "Build_ConveyorCeiling",
	"Build_Pipeline", "Build_PipeHyper", "Build_HyperTubeJunction",
}

func isPassthrough(class string) bool {
	for _, p := range passthroughPrefixes {
		if strings.HasPrefix(class, p) {
			return true
		}
	}
	return false
}

// buildProductionLines contracts the belt/pipe adjacency into production lines.
// Pass-through nodes are contracted; boundary nodes stop connectivity and are
// recorded as the line's terminals. A component with no production machine is
// dropped (e.g. a belt run between two storage containers).
func buildProductionLines(edges []connEdge, machines map[string]bool) []productionLine {
	isBoundary := func(n string) bool {
		return !machines[n] && !isPassthrough(classOf(n))
	}

	parent := map[string]string{}
	var find func(string) string
	find = func(x string) string {
		p, ok := parent[x]
		if !ok {
			parent[x] = x
			return x
		}
		if p != x {
			parent[x] = find(p)
		}
		return parent[x]
	}
	union := func(a, b string) { parent[find(a)] = find(b) }

	type boundaryAttach struct{ boundary, other string }
	var attaches []boundaryAttach
	type internalEdge struct{ node, transport string }
	var internals []internalEdge

	for _, e := range edges {
		if e.from == e.to {
			find(e.from)
			continue
		}
		fb, tb := isBoundary(e.from), isBoundary(e.to)
		switch {
		case fb && tb:
			// belt/pipe between two boundaries (e.g. storage→storage): no flow
			// between machines, ignore.
		case fb:
			attaches = append(attaches, boundaryAttach{e.from, e.to})
			find(e.to)
		case tb:
			attaches = append(attaches, boundaryAttach{e.to, e.from})
			find(e.from)
		default:
			union(e.from, e.to)
			internals = append(internals, internalEdge{e.from, e.transport})
		}
	}

	members := map[string][]string{}
	for n := range parent {
		r := find(n)
		members[r] = append(members[r], n)
	}
	transports := map[string]map[string]bool{}
	for _, ie := range internals {
		r := find(ie.node)
		if transports[r] == nil {
			transports[r] = map[string]bool{}
		}
		transports[r][ie.transport] = true
	}
	terminals := map[string]map[string]bool{}
	for _, a := range attaches {
		r := find(a.other)
		if terminals[r] == nil {
			terminals[r] = map[string]bool{}
		}
		terminals[r][a.boundary] = true
	}

	var lines []productionLine
	for root, nodes := range members {
		var mach []string
		for _, n := range nodes {
			if machines[n] {
				mach = append(mach, n)
			}
		}
		if len(mach) == 0 {
			continue
		}
		sort.Strings(mach)
		lines = append(lines, productionLine{
			machines:   mach,
			terminals:  sortedKeys(terminals[root]),
			transports: sortedKeys(transports[root]),
		})
	}
	sort.Slice(lines, func(i, j int) bool {
		if len(lines[i].machines) != len(lines[j].machines) {
			return len(lines[i].machines) > len(lines[j].machines)
		}
		return lines[i].machines[0] < lines[j].machines[0]
	})
	return lines
}

func sortedKeys(set map[string]bool) []string {
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
