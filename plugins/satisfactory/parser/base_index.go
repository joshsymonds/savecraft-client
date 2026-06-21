package main

import (
	"math"
	"sort"
)

// cellOf snaps a world position to its baseCellSize grid cell.
func cellOf(pos [3]float32) cellKey {
	return cellKey{
		cx: int(math.Floor(float64(pos[0]) / baseCellSize)),
		cy: int(math.Floor(float64(pos[1]) / baseCellSize)),
	}
}

// baseIndex is the single source of truth for spatial bases: machines grouped
// into connected cell-regions, plus a cell->base map so any world position can
// be assigned to a base. Both the geography section (machines→base) and storage
// bucketing (containers→base) consume it, so the two never disagree on what a
// base is.
type baseIndex struct {
	bases     [][]machineRecord // ordered size-desc; slice index is the base id
	cellBase  map[cellKey]int   // occupied cell -> base id
	centroids [][3]float32      // per-base centroid, for assign's nearest fallback
}

// newBaseIndex clusters machines into bases: connected regions of occupied grid
// cells (8-neighbor adjacency). Deterministic: bases ordered by size desc then
// smallest member instance; members sorted by instance.
func newBaseIndex(machines []machineRecord) baseIndex {
	if len(machines) == 0 {
		return baseIndex{cellBase: map[cellKey]int{}}
	}
	occupied := map[cellKey][]machineRecord{}
	for _, m := range machines {
		k := cellOf(m.position)
		occupied[k] = append(occupied[k], m)
	}

	uf := newUnionFind[cellKey]()
	for k := range occupied {
		uf.find(k)
		for dx := -1; dx <= 1; dx++ {
			for dy := -1; dy <= 1; dy++ {
				if dx == 0 && dy == 0 {
					continue
				}
				n := cellKey{k.cx + dx, k.cy + dy}
				if _, ok := occupied[n]; ok {
					uf.union(k, n)
				}
			}
		}
	}

	// Collect each union-find component's members and its occupied cells.
	type component struct {
		members []machineRecord
		cells   []cellKey
	}
	byRoot := map[cellKey]*component{}
	for k, ms := range occupied {
		r := uf.find(k)
		c := byRoot[r]
		if c == nil {
			c = &component{}
			byRoot[r] = c
		}
		c.members = append(c.members, ms...)
		c.cells = append(c.cells, k)
	}

	comps := make([]*component, 0, len(byRoot))
	for _, c := range byRoot {
		sort.Slice(
			c.members,
			func(i, j int) bool { return c.members[i].instance < c.members[j].instance },
		)
		comps = append(comps, c)
	}
	sort.Slice(comps, func(i, j int) bool {
		if len(comps[i].members) != len(comps[j].members) {
			return len(comps[i].members) > len(comps[j].members)
		}
		return comps[i].members[0].instance < comps[j].members[0].instance
	})

	idx := baseIndex{
		bases:     make([][]machineRecord, len(comps)),
		cellBase:  map[cellKey]int{},
		centroids: make([][3]float32, len(comps)),
	}
	for id, c := range comps {
		idx.bases[id] = c.members
		positions := make([][3]float32, len(c.members))
		for i, m := range c.members {
			positions[i] = m.position
		}
		idx.centroids[id] = centroid(positions)
		for _, cell := range c.cells {
			idx.cellBase[cell] = id
		}
	}
	return idx
}

// assign maps a world position to a base id: the base owning the position's grid
// cell, or — when that cell holds no machines — the nearest base by squared
// centroid distance (ties broken by lower id). Returns -1 when there are no
// bases.
func (bi baseIndex) assign(pos [3]float32) int {
	if len(bi.bases) == 0 {
		return -1
	}
	if id, ok := bi.cellBase[cellOf(pos)]; ok {
		return id
	}
	best, bestDist := -1, math.Inf(1)
	for id, c := range bi.centroids {
		dx := float64(pos[0] - c[0])
		dy := float64(pos[1] - c[1])
		if d := dx*dx + dy*dy; d < bestDist {
			best, bestDist = id, d
		}
	}
	return best
}
