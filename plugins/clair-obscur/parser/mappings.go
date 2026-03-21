package main

import "strings"

// attributeNames maps ECharacterAttribute enum values to human-readable names.
var attributeNames = map[string]string{
	"NewEnumerator0": "Vitality",
	"NewEnumerator1": "Strength",
	"NewEnumerator2": "Intelligence",
	"NewEnumerator3": "Agility",
	"NewEnumerator4": "Defense",
	"NewEnumerator5": "Luck",
}

// itemTypeNames maps E_jRPG_ItemType enum values to human-readable names.
var itemTypeNames = map[string]string{
	"NewEnumerator0":  "Weapon",
	"NewEnumerator6":  "N/A",
	"NewEnumerator7":  "Consumable",
	"NewEnumerator10": "Pictos",
	"NewEnumerator11": "Key",
	"NewEnumerator12": "Inventory",
	"NewEnumerator14": "Shard",
	"NewEnumerator15": "Gold",
	"NewEnumerator16": "Cosmetic",
	"NewEnumerator17": "SkillUnlocker",
}

// explorationCapacityNames maps E_ExplorationCapacity enum values.
var explorationCapacityNames = map[string]string{
	"NewEnumerator0": "Esquie",
	"NewEnumerator1": "BreakRocks",
	"NewEnumerator3": "Swim",
	"NewEnumerator4": "CoralSwim",
	"NewEnumerator5": "Fly",
	"NewEnumerator7": "Unknown",
}

// worldMapCapacityNames maps E_WorldMapExplorationCapacity enum values.
var worldMapCapacityNames = map[string]string{
	"NewEnumerator0": "Base",
	"NewEnumerator1": "HardenLands",
	"NewEnumerator2": "Swim",
	"NewEnumerator3": "SwimBoost",
	"NewEnumerator4": "Fly",
}

// formationTypeNames maps E_jRPG_FormationType enum values.
var formationTypeNames = map[string]string{
	"NewEnumerator0": "Default",
}

// questStatusNames maps E_QuestStatus enum values.
var questStatusNames = map[string]string{
	"NewEnumerator0": "NotStarted",
	"NewEnumerator1": "InProgress",
	"NewEnumerator2": "Completed",
}

// difficultyNames maps E_GameDifficulty enum values.
var difficultyNames = map[string]string{
	"NewEnumerator0": "Easy",
	"NewEnumerator1": "Normal",
	"NewEnumerator2": "Hard",
}

// characterDisplayNames maps internal character names to their in-game names.
// "Frey" was the protagonist's development name; the released game uses "Gustave".
var characterDisplayNames = map[string]string{
	"Frey": "Gustave",
}

// displayName returns the player-facing name for a character,
// correcting any internal/development names.
func displayName(internal string) string {
	if name, ok := characterDisplayNames[internal]; ok {
		return name
	}
	return internal
}

// mapEnum resolves "EnumType::Value" to a human name using a mapping table.
// Returns the original string if no mapping is found.
func mapEnum(val string, names map[string]string) string {
	parts := strings.SplitN(val, "::", 2)
	if len(parts) != 2 {
		return val
	}
	if name, ok := names[parts[1]]; ok {
		return name
	}
	return val
}
