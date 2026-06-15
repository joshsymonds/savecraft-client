package main

import (
	"regexp"
	"strings"
)

// Display names come from the datagen-generated canonicalNames table (the
// authoritative Docs.json names); the class-path heuristic below is only a
// fallback for classes absent from that table (mods, future content). The raw
// class path is always emitted alongside, so nothing downstream depends on the
// heuristic.

var camelBoundary = regexp.MustCompile(`([a-z0-9])([A-Z])`)

// displayName turns a class path or class name into a readable label:
// ".../Desc_IronPlate.Desc_IronPlate_C" -> "Iron Plate". The canonical table
// is authoritative — it resolves the many cases where the class name disagrees
// with the in-game name (".../Desc_SteelPlate_C" -> "Steel Beam").
func displayName(classPath string) string {
	name := classPath
	if i := strings.LastIndex(name, "."); i >= 0 {
		name = name[i+1:]
	}
	if canonical, ok := canonicalNames[name]; ok {
		return canonical
	}
	return heuristicName(classPath)
}

// heuristicName derives a readable label from a class path when the class is
// absent from canonicalNames.
func heuristicName(classPath string) string {
	classPrefixes := []string{
		"BP_EquipmentDescriptor",
		"BP_ItemDescriptor",
		"BP_EquipmentDesc",
		"Desc_",
		"BP_",
		"Schematic_",
		"Recipe_",
		"Build_",
		"ResearchTree_",
		"CustomizerUnlock_",
	}
	name := classPath
	if i := strings.LastIndex(name, "."); i >= 0 {
		name = name[i+1:]
	}
	name = strings.TrimSuffix(name, "_C")
	for _, prefix := range classPrefixes {
		if rest := strings.TrimPrefix(name, prefix); rest != name && rest != "" {
			name = rest
			break
		}
	}
	name = strings.ReplaceAll(name, "_", " ")
	name = camelBoundary.ReplaceAllString(name, "$1 $2")
	return strings.TrimSpace(name)
}

var milestonePattern = regexp.MustCompile(`Schematic_(\d+)-(\d+)_C$`)

// milestoneTier extracts the tier from a purchased milestone schematic class
// name like ".../Schematic_5-1.Schematic_5-1_C". Returns 0 for schematics
// that are not tier milestones (customizer unlocks etc.).
func milestoneTier(classPath string) int {
	m := milestonePattern.FindStringSubmatch(classPath)
	if m == nil {
		return 0
	}
	tier := 0
	for _, c := range m[1] {
		tier = tier*10 + int(c-'0')
	}
	return tier
}

var phasePattern = regexp.MustCompile(`Phase_(\d+)`)

// elevatorPhase extracts N from a game phase path like
// ".../GP_Project_Assembly_Phase_3.GP_Project_Assembly_Phase_3".
func elevatorPhase(phasePath string) int {
	m := phasePattern.FindStringSubmatch(phasePath)
	if m == nil {
		return 0
	}
	n := 0
	for _, c := range m[1] {
		n = n*10 + int(c-'0')
	}
	return n
}
