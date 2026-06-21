package main

import (
	"sort"
	"strings"

	"github.com/joshsymonds/savecraft.gg/plugins/satisfactory/sav"
)

// mapMarker is a player-placed map marker: a name and a world position. These
// are the geographic anchors used to name production lines and bases.
type mapMarker struct {
	name    string
	x, y, z float64
}

// isResourceNode reports whether a class is an extractor-occupiable resource
// node (ore node, geyser, fracking satellite/core) — not a hand-mined deposit.
func isResourceNode(classPath string) bool {
	return strings.Contains(classPath, "BP_ResourceNode") ||
		strings.Contains(classPath, "BP_Fracking")
}

// parseMapMarkers decodes FGMapManager.mMapMarkers. Entries missing a name or a
// usable location are skipped; an absent property yields no markers.
func parseMapMarkers(od *sav.ObjectData) []mapMarker {
	raw, _ := prop[[]any](od, "mMapMarkers")
	out := make([]mapMarker, 0, len(raw))
	for _, r := range raw {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		name, nameOK := m["Name"].(string)
		loc, locOK := m["Location"].(map[string]any)
		if !nameOK || name == "" || !locOK {
			continue
		}
		x, xok := loc["X"].(float64)
		y, yok := loc["Y"].(float64)
		if !xok || !yok {
			continue
		}
		z := 0.0 // Z is optional; absent height defaults to 0.
		if zv, ok := loc["Z"].(float64); ok {
			z = zv
		}
		out = append(out, mapMarker{name: name, x: x, y: y, z: z})
	}
	return out
}

// areaName reduces a map-area class path to its class name, e.g.
// "…/Area_RockyDesert_2.Area_RockyDesert_2_C" → "Area_RockyDesert_2".
func areaName(path string) string {
	tail := path[strings.LastIndex(path, ".")+1:]
	return strings.TrimSuffix(tail, "_C")
}

// visitedAreaNames lists the distinct map areas the player has visited.
func (s *saveState) visitedAreaNames() []string {
	raw, _ := prop[[]any](s.gameState, "mVisitedMapAreas")
	seen := map[string]bool{}
	out := []string{}
	for _, r := range raw {
		ref, ok := r.(sav.ObjectRef)
		if !ok {
			continue
		}
		if name := areaName(ref.Path); name != "" && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}
