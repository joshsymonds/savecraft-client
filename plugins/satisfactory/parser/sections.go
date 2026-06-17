package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/joshsymonds/savecraft.gg/plugins/satisfactory/sav"
)

// saveState accumulates the decoded objects the section builders need.
// One streaming Extract pass fills it; builders read from it afterwards.
type saveState struct {
	header *sav.Header

	gameState  *sav.ObjectData
	gameRules  *sav.ObjectData
	schematics *sav.ObjectData
	gamePhase  *sav.ObjectData
	research   *sav.ObjectData
	unlocks    *sav.ObjectData

	playerCount     int
	playerPosition  [3]float32
	playerInventory map[string]*sav.ObjectData // slot suffix -> decoded component

	manufacturers       []machineRecord
	extractors          []machineRecord
	generators          []machineRecord
	powerStorageCharges []float64
	powerCircuits       int

	// machineInventories holds factory-machine input/output inventory
	// components keyed by their instance path; resolve() joins them onto
	// the machine records after the extract pass.
	machineInventories map[string]*sav.ObjectData

	// connEdges are belt/pipe links between actors, from connection
	// components' mConnectedComponent refs; the logistics graph is built
	// from them.
	connEdges []connEdge

	// mapMarkers are player-placed named markers (geographic anchors).
	// resourceNodePos maps a resource-node actor instance to its world
	// position, for joining with the extractors that occupy them.
	mapMarkers      []mapMarker
	resourceNodePos map[string][3]float32

	containerCounts   map[string]int
	storedItems       map[string]int64 // item class -> total across containers
	centralStorage    *sav.ObjectData
	trains            int
	locomotives       int
	wagons            int
	trainStations     int
	trainStationNames []string
	timetables        int
	timetableStops    int
	drones            int
	droneStationNames []string
	truckStations     int
	vehicleCounts     map[string]int
}

func newSaveState(header *sav.Header) *saveState {
	return &saveState{
		header:             header,
		playerInventory:    map[string]*sav.ObjectData{},
		containerCounts:    map[string]int{},
		storedItems:        map[string]int64{},
		vehicleCounts:      map[string]int{},
		machineInventories: map[string]*sav.ObjectData{},
		resourceNodePos:    map[string][3]float32{},
	}
}

// want selects the objects the current sections need: progression manager
// singletons, the (first) player pawn, and the player's inventory
// components. Class checks use suffixes — paths are stable, mod prefixes
// are not.
func (s *saveState) want(o sav.ObjectHeader) bool {
	switch {
	case strings.HasSuffix(o.ClassPath, ".BP_GameState_C"),
		strings.HasSuffix(o.ClassPath, ".FGGameRulesSubsystem"),
		strings.HasSuffix(o.ClassPath, ".BP_SchematicManager_C"),
		strings.HasSuffix(o.ClassPath, ".BP_GamePhaseManager_C"),
		strings.HasSuffix(o.ClassPath, ".BP_ResearchManager_C"),
		strings.HasSuffix(o.ClassPath, ".BP_UnlockSubsystem_C"),
		strings.HasSuffix(o.ClassPath, ".Char_Player_C"):
		return true
	}
	if strings.Contains(o.ClassPath, "FGInventoryComponent") &&
		strings.Contains(o.InstanceName, "Char_Player") {
		return true
	}
	if isMachineInventory(o) {
		return true
	}
	if _, ok := isConnectionComponent(o.ClassPath); ok {
		return true
	}
	if strings.HasSuffix(o.ClassPath, ".FGPowerCircuit") {
		return true
	}
	if strings.HasSuffix(o.ClassPath, ".FGMapManager") || isResourceNode(o.ClassPath) {
		return true
	}
	return factoryKind(o.ClassPath) != "" || logisticsKind(o) != ""
}

// isMachineInventory reports whether o is a factory machine's input or output
// inventory component. Manufacturers own both; extractors own an output;
// generators own a fuel input. Storage buffers (.StorageInventory) and player
// slots use different suffixes and are routed elsewhere.
func isMachineInventory(o sav.ObjectHeader) bool {
	return strings.Contains(o.ClassPath, "FGInventoryComponent") &&
		(strings.HasSuffix(o.InstanceName, ".InputInventory") ||
			strings.HasSuffix(o.InstanceName, ".OutputInventory"))
}

func (s *saveState) collect(o sav.Object) error {
	od, err := sav.ParseObjectData(o)
	if err != nil {
		// One undecodable object must not kill the whole parse; sections
		// degrade to whatever was collected.
		fmt.Fprintf(stderr(), "satisfactory: decode %s (%s): %v\n", o.InstanceName, o.ClassPath, err)
		return nil
	}

	if transport, ok := isConnectionComponent(o.ClassPath); ok {
		if e, ok := connEdgeFrom(o.InstanceName, od, transport); ok {
			s.connEdges = append(s.connEdges, e)
		}
		return nil
	}

	switch {
	case strings.HasSuffix(o.ClassPath, ".BP_GameState_C"):
		s.gameState = od
	case strings.HasSuffix(o.ClassPath, ".FGGameRulesSubsystem"):
		s.gameRules = od
	case strings.HasSuffix(o.ClassPath, ".BP_SchematicManager_C"):
		s.schematics = od
	case strings.HasSuffix(o.ClassPath, ".BP_GamePhaseManager_C"):
		s.gamePhase = od
	case strings.HasSuffix(o.ClassPath, ".BP_ResearchManager_C"):
		s.research = od
	case strings.HasSuffix(o.ClassPath, ".BP_UnlockSubsystem_C"):
		s.unlocks = od
	case strings.HasSuffix(o.ClassPath, ".Char_Player_C"):
		if s.playerCount == 0 {
			s.playerPosition = o.Translation
		}
		s.playerCount++
	case strings.HasSuffix(o.ClassPath, ".FGPowerCircuit"):
		s.powerCircuits++
	case strings.HasSuffix(o.ClassPath, ".FGMapManager"):
		s.mapMarkers = parseMapMarkers(od)
	case isResourceNode(o.ClassPath):
		s.resourceNodePos[o.InstanceName] = o.Translation
	case isMachineInventory(o.ObjectHeader):
		s.machineInventories[o.InstanceName] = od
	case factoryKind(o.ClassPath) != "":
		s.collectFactory(factoryKind(o.ClassPath), o, od)
	case logisticsKind(o.ObjectHeader) != "":
		s.collectLogistics(logisticsKind(o.ObjectHeader), o, od)
	default: // player inventory component
		slot := o.InstanceName[strings.LastIndex(o.InstanceName, ".")+1:]
		// Multiplayer: keep the first (host) player's components only.
		if _, exists := s.playerInventory[slot]; !exists {
			s.playerInventory[slot] = od
		}
	}
	return nil
}

// prop fetches a property from a possibly-nil ObjectData.
func prop[T any](od *sav.ObjectData, name string) (T, bool) {
	var zero T
	if od == nil {
		return zero, false
	}
	v, ok := od.Properties[name].(T)
	if !ok {
		return zero, false
	}
	return v, true
}

// schematicGroups buckets purchased schematics by their path taxonomy.
type schematicGroups struct {
	milestones []string // class paths under /Schematics/Progression/
	mam        int
	shop       int
	alternates []string // class paths under /Schematics/Alternate/
	other      int
}

func (s *saveState) groupSchematics() schematicGroups {
	var g schematicGroups
	purchased, _ := prop[[]any](s.schematics, "mPurchasedSchematics")
	for _, raw := range purchased {
		ref, ok := raw.(sav.ObjectRef)
		if !ok {
			continue
		}
		switch {
		case strings.Contains(ref.Path, "/Schematics/Progression/"):
			g.milestones = append(g.milestones, ref.Path)
		case strings.Contains(ref.Path, "/Schematics/Research/"):
			g.mam++
		case strings.Contains(ref.Path, "/Schematics/ResourceSink/"):
			g.shop++
		case strings.Contains(ref.Path, "/Schematics/Alternate/"):
			g.alternates = append(g.alternates, ref.Path)
		default:
			g.other++
		}
	}
	return g
}

// currentTier is the highest tier among purchased milestones.
func (g schematicGroups) currentTier() int {
	tier := 0
	for _, m := range g.milestones {
		if t := milestoneTier(m); t > tier {
			tier = t
		}
	}
	return tier
}

func (s *saveState) buildProgressionSection() map[string]any {
	groups := s.groupSchematics()

	perTier := map[int]int{}
	tiers := make([]int, 0, 10)
	for _, m := range groups.milestones {
		if t := milestoneTier(m); t > 0 {
			if perTier[t] == 0 {
				tiers = append(tiers, t)
			}
			perTier[t]++
		}
	}
	sort.Ints(tiers)
	tierCounts := make([]map[string]any, 0, len(tiers))
	for _, tier := range tiers {
		tierCounts = append(tierCounts, map[string]any{"tier": tier, "milestonesPurchased": perTier[tier]})
	}

	alternates := make([]string, 0, len(groups.alternates))
	alternateClasses := make([]string, 0, len(groups.alternates))
	for _, a := range groups.alternates {
		alternates = append(alternates, displayName(a))
		alternateClasses = append(alternateClasses, a[strings.LastIndex(a, ".")+1:])
	}
	sort.Strings(alternates)
	sort.Strings(alternateClasses)

	milestoneClasses := make([]string, 0, len(groups.milestones))
	for _, m := range groups.milestones {
		milestoneClasses = append(milestoneClasses, m[strings.LastIndex(m, ".")+1:])
	}
	sort.Strings(milestoneClasses)

	data := map[string]any{
		"currentTier":          groups.currentTier(),
		"milestonesPurchased":  len(groups.milestones),
		"milestoneClassNames":  milestoneClasses,
		"milestonesPerTier":    tierCounts,
		"mamResearchCompleted": groups.mam,
		"shopPurchases":        groups.shop,
		"alternateRecipes": map[string]any{
			"count": len(alternates),
			"names": alternates,
			// Schematic class names, for the production_planner's
			// unlocked-alternates resolution via reference data.
			"schematicClassNames": alternateClasses,
		},
	}

	if phase, ok := prop[sav.ObjectRef](s.gamePhase, "mCurrentGamePhase"); ok {
		data["spaceElevatorPhase"] = elevatorPhase(phase.Path)
	}
	if active, ok := prop[sav.ObjectRef](s.schematics, "mActiveSchematic"); ok {
		data["activeMilestone"] = displayName(active.Path)
	}
	if built, ok := prop[bool](s.gameState, "mIsSpaceElevatorBuilt"); ok {
		data["spaceElevatorBuilt"] = built
	}
	if trees, ok := prop[[]any](s.research, "mUnlockedResearchTrees"); ok {
		data["mamTreesUnlocked"] = len(trees)
	}
	if ongoing, ok := prop[[]any](s.research, "mSavedOngoingResearch"); ok && len(ongoing) > 0 {
		data["mamResearchInProgress"] = len(ongoing)
	}
	return data
}

// inventoryItems flattens an inventory component's stacks into item entries.
func inventoryItems(od *sav.ObjectData) []map[string]any {
	stacks, _ := prop[[]any](od, "mInventoryStacks")
	items := make([]map[string]any, 0, len(stacks))
	for _, raw := range stacks {
		stack, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		item, ok := stack["Item"].(sav.InventoryItem)
		if !ok || item.ItemClass == "" {
			continue
		}
		count, ok := stack["NumItems"].(int64)
		if !ok {
			count = 0
		}
		items = append(items, map[string]any{
			"name":      displayName(item.ItemClass),
			"classPath": item.ItemClass,
			"count":     count,
		})
	}
	return items
}

// inventoryStacks decodes an inventory component's non-empty slots into
// typed item/count pairs.
func inventoryStacks(od *sav.ObjectData) []invStack {
	raw, _ := prop[[]any](od, "mInventoryStacks")
	out := make([]invStack, 0, len(raw))
	for _, r := range raw {
		stack, ok := r.(map[string]any)
		if !ok {
			continue
		}
		item, ok := stack["Item"].(sav.InventoryItem)
		if !ok || item.ItemClass == "" {
			continue
		}
		count, _ := stack["NumItems"].(int64)
		out = append(out, invStack{itemClass: item.ItemClass, count: count})
	}
	return out
}

// resolve joins each machine record with its input/output inventory contents,
// looked up by instance path. Idempotent: safe to call more than once.
func (s *saveState) resolve() {
	fill := func(records []machineRecord) {
		for i := range records {
			if od, ok := s.machineInventories[records[i].instance+".InputInventory"]; ok {
				records[i].inputContents = inventoryStacks(od)
			}
			if od, ok := s.machineInventories[records[i].instance+".OutputInventory"]; ok {
				records[i].outputContents = inventoryStacks(od)
			}
		}
	}
	fill(s.manufacturers)
	fill(s.extractors)
	fill(s.generators)
}

func (s *saveState) buildPlayerSection() map[string]any {
	// The player inventory components that hold worn gear.
	equipmentSlotNames := []string{"ArmSlot", "BackSlot", "BodySlot", "HeadSlot", "LegsSlot"}
	data := map[string]any{
		"playerCount": s.playerCount,
		"position": map[string]any{
			"x": s.playerPosition[0], "y": s.playerPosition[1], "z": s.playerPosition[2],
		},
	}

	if inv, ok := s.playerInventory["inventory"]; ok {
		data["inventory"] = map[string]any{"items": inventoryItems(inv)}
	}

	equipment := map[string]any{}
	for _, slot := range equipmentSlotNames {
		od, ok := s.playerInventory[slot]
		if !ok {
			continue
		}
		for _, item := range inventoryItems(od) {
			equipment[slot] = item["name"]
			break
		}
	}
	if len(equipment) > 0 {
		data["equipment"] = equipment
	}

	if slots, ok := prop[int64](s.unlocks, "mNumTotalInventorySlots"); ok {
		data["totalInventorySlots"] = slots
	}
	return data
}

func (s *saveState) buildOverviewSection() map[string]any {
	h := s.header
	data := map[string]any{
		"sessionName":     h.SessionName,
		"saveName":        h.SaveName,
		"mapName":         h.MapName,
		"playTimeSeconds": int32(h.PlayDuration.Seconds()),
		"playTimeHours":   fmt.Sprintf("%.1f", h.PlayDuration.Hours()),
		"savedAt":         h.SaveTime.Format("2006-01-02T15:04:05Z"),
		"gameBuild":       h.BuildVersion,
		"saveVersion":     h.SaveVersion,
		"creativeMode":    h.CreativeMode,
		"modded":          h.Modded,
	}
	if built, ok := prop[bool](s.gameState, "mIsSpaceElevatorBuilt"); ok {
		data["spaceElevatorBuilt"] = built
	}
	if s.playerCount > 0 {
		data["players"] = s.playerCount
	}
	if gm := s.gameModeSettings(); len(gm) > 0 {
		data["gameMode"] = gm
	}
	return data
}

// gameModeSettings reads the 1.2 Game Mode economy multipliers, node
// settings, and AGS session rules from the game state and rules subsystem.
// UE omits default-valued properties from the save, so presence implies
// non-default; an empty map means a vanilla session.
func (s *saveState) gameModeSettings() map[string]any {
	gm := map[string]any{}
	for key, name := range map[string]string{
		"partsCostMultiplier":      "mPartsCostMultiplier",
		"energyCostMultiplier":     "mEnergyCostMultiplier",
		"spacePartsCostMultiplier": "mSpacePartsCostMultiplier",
	} {
		if v, ok := prop[float64](s.gameState, name); ok {
			gm[key] = v
		}
	}
	if v, ok := prop[string](s.gameState, "mNodeRandomization"); ok {
		gm["nodeRandomization"] = enumShort(v)
	}
	if v, ok := prop[string](s.gameState, "mNodePuritySettings"); ok {
		gm["nodePurity"] = enumShort(v)
	}
	if v, ok := prop[int64](s.gameState, "mNodeRandomizationSeed"); ok {
		gm["nodeRandomizationSeed"] = v
	}
	for key, name := range map[string]string{
		"cheatNoPower": "mCheatNoPower",
		"cheatNoCost":  "mCheatNoCost",
		"cheatNoFuel":  "mCheatNoFuel",
	} {
		if v, ok := prop[bool](s.gameState, name); ok && v {
			gm[key] = true
		}
	}
	if v, ok := prop[int64](s.gameRules, "mStartingTier"); ok {
		gm["startingTier"] = v
	}
	for key, name := range map[string]string{
		"noUnlockCost":            "mNoUnlockCost",
		"unlockInstantAltRecipes": "mUnlockInstantAltRecipes",
	} {
		if v, ok := prop[bool](s.gameRules, name); ok && v {
			gm[key] = true
		}
	}
	return gm
}

// enumShort trims the UE enum type prefix: "ENodeRandomizationMode::NRM_Strict"
// → "NRM_Strict". Unknown future values pass through untouched.
func enumShort(v string) string {
	if i := strings.LastIndex(v, "::"); i >= 0 {
		return v[i+2:]
	}
	return v
}

func (s *saveState) buildSummary() string {
	summary := s.header.SessionName
	if tier := s.groupSchematics().currentTier(); tier > 0 {
		summary += fmt.Sprintf(", Tier %d", tier)
	}
	summary += fmt.Sprintf(", %.1f hours played", s.header.PlayDuration.Hours())
	if s.header.CreativeMode {
		summary += " (creative)"
	}
	if s.header.Modded {
		summary += " (modded)"
	}
	return summary
}

func (s *saveState) buildResult() map[string]any {
	sections := map[string]any{
		"game_overview": map[string]any{
			"description": "Save metadata: session name, playtime, game build, save timestamp, creative/modded flags, space elevator status, and gameMode (1.2 Game Mode economy multipliers, node randomization, AGS cheats; absent = vanilla) — fetch first to orient on which factory world this is. When gameMode multipliers are present, vanilla recipe ratios do NOT apply; production_planner accounts for them automatically",
			"data":        s.buildOverviewSection(),
		},
		"progression": map[string]any{
			"description": "Unlock progress: current tier, milestones purchased per tier, active milestone, MAM research, AWESOME shop purchases, unlocked alternate recipes, space elevator phase — use to ground what the player can and cannot build yet",
			"data":        s.buildProgressionSection(),
		},
		"player": map[string]any{
			"description": "Player state: inventory items with counts, equipped gear, position, unlocked inventory size — use to check what materials are on hand",
			"data":        s.buildPlayerSection(),
		},
		"machines": map[string]any{
			"description": "Built production machines aggregated by building + recipe + clock speed, with producing counts, somersloop-amplified counts, measured productivity (rolling in-game window), and a status breakdown per group; extractors by type — use to assess factory layout and find idle machines. Status is derived from measured productivity, not the instantaneous producing flag (which reads false in saves taken while stopped). status keys: producing, throttled (running below capacity), blocked_downstream (output backed up), starved_upstream (an ingredient ran out), unconfigured (no recipe set), idle (not producing, cause undetermined — the save has no power-outage flag, so a power cause is NOT asserted). A group with no status key is fully producing.",
			"data":        s.buildMachinesSection(),
		},
		"production_summary": map[string]any{
			"description": "Machines aggregated per recipe with effective capacity (clock x somersloop boost, in 100%-clock machine equivalents), somersloop counts, measured productivity, and a status breakdown (see the machines section for status keys; idle means cause-undetermined, not a power claim). Do not invent per-minute item rates; effectiveCapacity x recipe base rate from the production_planner reference module gives max output",
			"data":        s.buildProductionSection(),
		},
		"storage": map[string]any{
			"description": "Stored materials: per-item totals across all storage containers, container counts, and the dimensional depot's uploaded items — use to find available materials beyond the player's pockets",
			"data":        s.buildStorageSection(),
		},
		"logistics": map[string]any{
			"description": "Transport networks: trains with named stations and timetables, drone routes with station tags, trucks and truck stations — use to understand how materials move between factories",
			"data":        s.buildLogisticsSection(),
		},
		"resource_nodes": map[string]any{
			"description": "Resource extraction: count of occupied resource nodes and extractors by type. Node resource types and purities require static map reference data (not yet included)",
			"data":        s.buildResourceNodesSection(),
		},
		"power": map[string]any{
			"description": "Power grid: circuit count, generators by type and fuel with producing counts, battery storage charge — use to assess generation mix; MW capacity requires reference data, so counts are what the save provides",
			"data":        s.buildPowerSection(),
		},
	}
	return map[string]any{
		"type": "result",
		"identity": map[string]any{
			"saveName": s.header.SessionName,
			"gameId":   "satisfactory",
		},
		"summary":  s.buildSummary(),
		"sections": sections,
	}
}
