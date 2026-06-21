package main

import (
	"math"
)

// baseCellSize is the grid resolution (cm, ≈240 m) for clustering machines
// into bases: machines in the same or an 8-adjacent occupied cell join one
// base. Tunable.
const baseCellSize = 24000.0

// maxOccupiedNodes caps the per-section list of occupied resource nodes.
const maxOccupiedNodes = 50

type cellKey struct{ cx, cy int }

// topLabel returns the most common key (ties broken lexicographically) — used
// to descriptor-prefix a base name with its dominant building type.
func topLabel(counts map[string]int) string {
	best := ""
	bestCount := -1
	for label, n := range counts {
		if n > bestCount || (n == bestCount && label < best) {
			best, bestCount = label, n
		}
	}
	return best
}

func (s *saveState) buildGeographySection() map[string]any {
	all := make([]machineRecord, 0, len(s.manufacturers)+len(s.extractors)+len(s.generators))
	all = append(all, s.manufacturers...)
	all = append(all, s.extractors...)
	all = append(all, s.generators...)

	groups := newBaseIndex(all).bases
	bases := make([]map[string]any, 0, len(groups))
	for _, group := range groups {
		positions := make([][3]float32, len(group))
		byKind := map[string]int{}
		buildings := map[string]int{}
		minX, minY := math.Inf(1), math.Inf(1)
		maxX, maxY := math.Inf(-1), math.Inf(-1)
		for i, m := range group {
			positions[i] = m.position
			byKind[m.kind]++
			buildings[displayName(m.building)]++
			x, y := float64(m.position[0]), float64(m.position[1])
			minX, minY = math.Min(minX, x), math.Min(minY, y)
			maxX, maxY = math.Max(maxX, x), math.Max(maxY, y)
		}
		c := centroid(positions)
		bases = append(bases, map[string]any{
			"name": topLabel(
				buildings,
			) + " " + regionName(
				float64(c[0]),
				float64(c[1]),
				s.mapMarkers,
			),
			"machineCount": len(group),
			"byKind":       byKind,
			"centroid":     posMap(c),
			"bounds":       map[string]any{"minX": minX, "minY": minY, "maxX": maxX, "maxY": maxY},
		})
	}

	markers := make([]map[string]any, 0, len(s.mapMarkers))
	for _, m := range s.mapMarkers {
		markers = append(markers, map[string]any{"name": m.name, "x": m.x, "y": m.y})
	}

	return map[string]any{
		"bases":         bases,
		"markers":       markers,
		"visitedAreas":  s.visitedAreaNames(),
		"resourceNodes": s.resourceNodesGeo(),
	}
}

// resourceNodesGeo summarizes resource-node usage: the total node count and the
// extractors occupying a node, with the resource each yields (inferred from the
// extractor's output) — purity is not in the save and is not reported.
func (s *saveState) resourceNodesGeo() map[string]any {
	occupied := make([]map[string]any, 0)
	omitted := 0
	add := func(recs []machineRecord) {
		for _, r := range recs {
			if r.node == "" {
				continue
			}
			pos, ok := s.resourceNodePos[r.node]
			if !ok {
				continue
			}
			if len(occupied) >= maxOccupiedNodes {
				omitted++
				continue
			}
			resource := "unknown"
			if len(r.outputContents) > 0 {
				resource = displayName(r.outputContents[0].itemClass)
			}
			occupied = append(occupied, map[string]any{
				"resource":  resource,
				"extractor": displayName(r.building),
				"position":  posMap(pos),
			})
		}
	}
	add(s.extractors)

	out := map[string]any{
		"total":    len(s.resourceNodePos),
		"occupied": occupied,
		"note":     "node purity requires static map reference data, not in the save",
	}
	if omitted > 0 {
		out["occupiedOmitted"] = omitted
	}
	return out
}
