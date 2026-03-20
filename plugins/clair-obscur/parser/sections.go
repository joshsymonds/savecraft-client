package main

import (
	"fmt"
	"math"
	"sort"

	"github.com/joshsymonds/savecraft.gg/plugins/gvas"
)

// buildAllSections constructs all save sections from the parsed GVAS save.
func buildAllSections(save *gvas.Save) map[string]any {
	sections := map[string]any{
		"overview":    buildOverviewSection(save),
		"party":       buildPartySection(save),
		"inventory":   buildInventorySection(save),
		"progression": buildProgressionSection(save),
		"weapons":     buildWeaponsSection(save),
	}

	for _, entry := range save.Properties.GetMap("CharactersCollection") {
		name := valueString(entry.Key)
		if name == "" {
			continue
		}
		sv, ok := entry.Value.(gvas.StructValue)
		if !ok {
			continue
		}
		charData := buildCharacterSection(name, sv.Properties)
		level := sv.Properties.GetIntPrefix("CurrentLevel")
		sectionKey := fmt.Sprintf("character:%s", name)
		sections[sectionKey] = map[string]any{
			"description": fmt.Sprintf("%s -- Level %d build: attributes, skills, Lumina, equipment", name, level),
			"data":        charData,
		}
	}

	return sections
}

// buildOverviewSection builds the save metadata section.
func buildOverviewSection(save *gvas.Save) map[string]any {
	props := save.Properties
	timePlayed := props.GetFloat64("TimePlayed")
	hours := math.Round(timePlayed/3600*10) / 10
	ngPlus := props.GetInt("FinishedGameCount")
	gold := props.GetInt("Gold")
	mapName := props.GetString("MapToLoad")

	var characters []string
	for _, entry := range props.GetMap("CharactersCollection") {
		name := valueString(entry.Key)
		if name != "" {
			characters = append(characters, name)
		}
	}

	difficulty := "Unknown"
	diffStruct := props.GetStruct("GameDifficultyData")
	if diffStruct != nil {
		diffEnum := diffStruct.GetByteEnumPrefix("LowestDifficulty")
		if diffEnum != "" {
			difficulty = mapEnum(diffEnum, difficultyNames)
		}
	}

	return map[string]any{
		"description": "Save metadata: playtime, NG+ cycle, current location, gold, characters",
		"data": map[string]any{
			"playtime_hours":   hours,
			"playtime_seconds": int(timePlayed),
			"ng_plus_cycle":    ngPlus,
			"current_map":      mapName,
			"gold":             gold,
			"characters":       characters,
			"difficulty":       difficulty,
		},
	}
}

// buildCharacterSection builds a single character's section data.
func buildCharacterSection(name string, props gvas.Properties) map[string]any {
	level := props.GetIntPrefix("CurrentLevel")
	experience := props.GetIntPrefix("CurrentExperience")
	hp := props.GetFloat64Prefix("CurrentHP")
	mp := props.GetFloat64Prefix("CurrentMP")
	ap := props.GetIntPrefix("AvailableActionPoints")
	excluded := props.GetBoolPrefix("IsExcluded")
	lumina := props.GetIntPrefix("LuminaFromConsumables")

	// Attributes
	attributes := map[string]int32{}
	for _, entry := range props.GetMapPrefix("AssignedAttributePoints") {
		attrEnum := valueString(entry.Key)
		attrName := mapEnum(attrEnum, attributeNames)
		if v, ok := entry.Value.(gvas.IntValue); ok {
			attributes[attrName] = v.V
		}
	}

	// Equipped skills
	var equippedSkills []string
	for _, elem := range props.GetArrayPrefix("EquippedSkills") {
		if s := valueString(elem); s != "" {
			equippedSkills = append(equippedSkills, s)
		}
	}

	// Unlocked skills
	var unlockedSkills []string
	for _, elem := range props.GetArrayPrefix("UnlockedSkills") {
		if s := valueString(elem); s != "" {
			unlockedSkills = append(unlockedSkills, s)
		}
	}

	// Equipped Lumina passives
	var equippedPassives []string
	for _, elem := range props.GetArrayPrefix("EquippedPassiveEffects") {
		if s := valueString(elem); s != "" {
			equippedPassives = append(equippedPassives, s)
		}
	}

	// Equipment (weapon + pictos)
	equipment := buildEquipment(props)

	result := map[string]any{
		"name":                    name,
		"level":                   level,
		"experience":              experience,
		"hp":                      hp,
		"mp":                      mp,
		"available_action_points": ap,
		"excluded":                excluded,
		"lumina_from_consumables": lumina,
		"attributes":              attributes,
		"equipped_skills":         equippedSkills,
		"unlocked_skills":         unlockedSkills,
		"equipped_lumina_passives": equippedPassives,
		"equipment":               equipment,
	}
	return result
}

// buildEquipment extracts weapon and pictos from EquippedItemsPerSlot.
func buildEquipment(props gvas.Properties) map[string]any {
	result := map[string]any{}
	var pictos []string

	for _, entry := range props.GetMapPrefix("EquippedItemsPerSlot") {
		itemName := valueString(entry.Value)
		if itemName == "" {
			continue
		}

		// Key is a struct with ItemType (ByteEnum) and SlotIndex (Int).
		keySV, ok := entry.Key.(gvas.StructValue)
		if !ok {
			continue
		}
		itemTypeEnum := keySV.Properties.GetByteEnumPrefix("ItemType")
		itemType := mapEnum(itemTypeEnum, itemTypeNames)

		switch itemType {
		case "Weapon":
			result["weapon"] = itemName
		case "Pictos":
			pictos = append(pictos, itemName)
		}
	}

	if len(pictos) > 0 {
		result["pictos"] = pictos
	}
	return result
}

// buildPartySection builds the active party composition section.
func buildPartySection(save *gvas.Save) map[string]any {
	var members []map[string]any
	for _, elem := range save.Properties.GetArray("CurrentParty") {
		sv, ok := elem.(gvas.StructValue)
		if !ok {
			continue
		}
		charName := sv.Properties.GetStringPrefix("CharacterHardcodedName")
		formationEnum := sv.Properties.GetByteEnumPrefix("Formation")
		formation := mapEnum(formationEnum, formationTypeNames)

		members = append(members, map[string]any{
			"character": charName,
			"formation": formation,
		})
	}

	return map[string]any{
		"description": "Active party composition",
		"data": map[string]any{
			"members": members,
		},
	}
}

// buildInventorySection builds the inventory section with all items as a flat map.
func buildInventorySection(save *gvas.Save) map[string]any {
	gold := save.Properties.GetInt("Gold")
	items := map[string]int32{}
	for _, entry := range save.Properties.GetMap("InventoryItems") {
		name := valueString(entry.Key)
		if name == "" {
			continue
		}
		if v, ok := entry.Value.(gvas.IntValue); ok {
			items[name] = v.V
		}
	}

	return map[string]any{
		"description": "All items and quantities",
		"data": map[string]any{
			"gold":        gold,
			"total_items": len(items),
			"items":       items,
		},
	}
}

// buildProgressionSection builds the quest progress and exploration section.
func buildProgressionSection(save *gvas.Save) map[string]any {
	props := save.Properties

	// Quests
	quests := map[string]any{}
	for _, entry := range props.GetMap("QuestStatuses") {
		questName := valueString(entry.Key)
		if questName == "" {
			continue
		}
		sv, ok := entry.Value.(gvas.StructValue)
		if !ok {
			continue
		}

		questStatusEnum := sv.Properties.GetByteEnumPrefix("QuestStatus")
		status := mapEnum(questStatusEnum, questStatusNames)

		objectivesCompleted := 0
		for _, objEntry := range sv.Properties.GetMapPrefix("ObjectivesStatus") {
			objStatusEnum := valueString(objEntry.Value)
			objStatus := mapEnum(objStatusEnum, questStatusNames)
			if objStatus == "Completed" {
				objectivesCompleted++
			}
		}

		quests[questName] = map[string]any{
			"status":               status,
			"objectives_completed": objectivesCompleted,
		}
	}

	// Exploration capacities
	var explorationCaps []string
	var worldMapCaps []string

	exploStruct := props.GetStruct("ExplorationProgression")
	if exploStruct != nil {
		for _, elem := range exploStruct.GetArrayPrefix("ExplorationCapacities") {
			if enumVal := valueString(elem); enumVal != "" {
				explorationCaps = append(explorationCaps, mapEnum(enumVal, explorationCapacityNames))
			}
		}
		for _, elem := range exploStruct.GetArrayPrefix("WorldMapCapacities") {
			if enumVal := valueString(elem); enumVal != "" {
				worldMapCaps = append(worldMapCaps, mapEnum(enumVal, worldMapCapacityNames))
			}
		}
	}

	// Enemy counts
	battledEnemies := len(props.GetMap("BattledEnemies"))
	encounteredEnemies := len(props.GetMap("EncounteredEnemies"))

	// Visited locations
	var visitedLocations []string
	for _, elem := range props.GetArray("VisitedLevelRowNames") {
		if s := valueString(elem); s != "" {
			visitedLocations = append(visitedLocations, s)
		}
	}

	return map[string]any{
		"description": "Quest progress, exploration unlocks, enemy encounters",
		"data": map[string]any{
			"quests": quests,
			"exploration": map[string]any{
				"exploration_capacities": explorationCaps,
				"world_map_capacities":   worldMapCaps,
			},
			"enemies_battled":     battledEnemies,
			"enemies_encountered": encounteredEnemies,
			"visited_locations":   visitedLocations,
		},
	}
}

// buildWeaponsSection builds the weapon/picto progression section.
func buildWeaponsSection(save *gvas.Save) map[string]any {
	var progressions []map[string]any
	for _, elem := range save.Properties.GetArray("WeaponProgressions") {
		sv, ok := elem.(gvas.StructValue)
		if !ok {
			continue
		}
		name := sv.Properties.GetStringPrefix("DefinitionID")
		level := sv.Properties.GetIntPrefix("CurrentLevel")
		progressions = append(progressions, map[string]any{
			"name":  name,
			"level": level,
		})
	}

	// Sort by level descending for readability.
	sort.Slice(progressions, func(i, j int) bool {
		li, _ := progressions[i]["level"].(int32)
		lj, _ := progressions[j]["level"].(int32)
		return li > lj
	})

	return map[string]any{
		"description": "Weapon and picto progression levels",
		"data": map[string]any{
			"progressions": progressions,
		},
	}
}

// buildSummary builds the save summary string.
func buildSummary(save *gvas.Save) string {
	props := save.Properties

	// Party member names.
	var partyNames []string
	for _, elem := range props.GetArray("CurrentParty") {
		sv, ok := elem.(gvas.StructValue)
		if !ok {
			continue
		}
		name := sv.Properties.GetStringPrefix("CharacterHardcodedName")
		if name != "" {
			partyNames = append(partyNames, name)
		}
	}

	// Level range.
	var minLevel, maxLevel int32
	first := true
	for _, entry := range props.GetMap("CharactersCollection") {
		sv, ok := entry.Value.(gvas.StructValue)
		if !ok {
			continue
		}
		lvl := sv.Properties.GetIntPrefix("CurrentLevel")
		if first {
			minLevel = lvl
			maxLevel = lvl
			first = false
		} else {
			if lvl < minLevel {
				minLevel = lvl
			}
			if lvl > maxLevel {
				maxLevel = lvl
			}
		}
	}

	ngPlus := props.GetInt("FinishedGameCount")
	timePlayed := props.GetFloat64("TimePlayed")
	hours := math.Round(timePlayed/3600*10) / 10

	party := ""
	for i, name := range partyNames {
		if i > 0 {
			party += ", "
		}
		party += name
	}

	levelRange := fmt.Sprintf("%d", minLevel)
	if minLevel != maxLevel {
		levelRange = fmt.Sprintf("%d-%d", minLevel, maxLevel)
	}

	return fmt.Sprintf("%s -- Level %s, NG+%d, %.1fh", party, levelRange, ngPlus, hours)
}

// buildSaveName derives a save name from the party leader.
func buildSaveName(save *gvas.Save) string {
	partyArr := save.Properties.GetArray("CurrentParty")
	if len(partyArr) > 0 {
		sv, ok := partyArr[0].(gvas.StructValue)
		if ok {
			name := sv.Properties.GetStringPrefix("CharacterHardcodedName")
			if name != "" {
				return name + "'s Expedition"
			}
		}
	}
	return "Unknown Expedition"
}

// valueString extracts a string from a Value (NameValue, StrValue, ByteEnumValue, EnumValue).
func valueString(v gvas.Value) string {
	switch val := v.(type) {
	case gvas.NameValue:
		return val.V
	case gvas.StrValue:
		return val.V
	case gvas.ByteEnumValue:
		return val.V
	case gvas.EnumValue:
		return val.V
	}
	return ""
}
