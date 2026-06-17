package main

import (
	"fmt"
	"math"
)

// markerNearRadius is how close (cm, 2D) a map marker must be for a region to
// be named after it (~800 m). Beyond this, a synthetic compass-sector name is
// used instead. Tunable.
const markerNearRadius = 80000.0

// centerEpsilon: a centroid within this 2D distance of the world origin is
// labelled "map center" rather than a compass sector.
const centerEpsilon = 1.0

// centroid is the component-wise mean of the positions; the zero vector for an
// empty input.
func centroid(positions [][3]float32) [3]float32 {
	if len(positions) == 0 {
		return [3]float32{}
	}
	var sx, sy, sz float64
	for _, p := range positions {
		sx += float64(p[0])
		sy += float64(p[1])
		sz += float64(p[2])
	}
	n := float64(len(positions))
	return [3]float32{float32(sx / n), float32(sy / n), float32(sz / n)}
}

// nearestMarker returns the marker closest to (x,y) by 2D distance. Ties break
// on the lexicographically smaller name, so the result is stable. ok is false
// when there are no markers.
func nearestMarker(x, y float64, markers []mapMarker) (mapMarker, float64, bool) {
	var best mapMarker
	bestDist := math.Inf(1)
	found := false
	for _, m := range markers {
		d := math.Hypot(x-m.x, y-m.y)
		if d < bestDist || (d == bestDist && m.name < best.name) {
			best, bestDist, found = m, d, true
		}
	}
	return best, bestDist, found
}

// regionName names a location by its nearest map marker when one is within
// markerNearRadius, else by a deterministic compass sector + rounded-km coords.
func regionName(x, y float64, markers []mapMarker) string {
	if m, dist, ok := nearestMarker(x, y, markers); ok && dist <= markerNearRadius {
		return "near '" + m.name + "'"
	}
	sector := sectorOf(x, y)
	if sector == "map center" {
		return "map center"
	}
	return fmt.Sprintf("%s sector (%.1fkm, %.1fkm)", sector, x/100000, y/100000)
}

// sectorOf names the 8-way compass sector of (x,y) under Satisfactory's
// convention (north is −Y, east is +X), or "map center" near the origin.
func sectorOf(x, y float64) string {
	if math.Hypot(x, y) < centerEpsilon {
		return "map center"
	}
	// atan2(-y, x): east=0°, north(−Y)=90°, west=180°, south=270°.
	deg := math.Atan2(-y, x) * 180 / math.Pi
	if deg < 0 {
		deg += 360
	}
	// Sectors centred on each direction, 45° wide, starting at E going CCW.
	sectors := []string{"E", "NE", "N", "NW", "W", "SW", "S", "SE"}
	idx := int(math.Floor((deg+22.5)/45)) % 8
	return sectors[idx]
}
