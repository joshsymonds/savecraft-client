// SDV plugin: parses Stardew Valley 1.6 XML save files into structured GameState.
// The main save file is extensionless XML inside a directory named FarmerName_ID.
//
// Build: GOOS=wasip1 GOARCH=wasm go build -o parser.wasm ./parser
package main

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"os"
)

func main() {
	enc := json.NewEncoder(os.Stdout)

	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		writeError(enc, "read_error", "failed to read stdin: "+err.Error())
		os.Exit(1)
	}

	writeStatusf(enc, "Stardew Valley save, %d bytes", len(data))

	save, err := parseSave(data)
	if err != nil {
		writeError(enc, "parse_error", err.Error())
		os.Exit(1)
	}

	writeStatusf(enc, "Farm: %s, %s", save.Player.FarmName, save.Player.Name)

	sections := buildSections(save)
	summary := buildSummary(save)

	if err := enc.Encode(map[string]any{
		"type": "result",
		"identity": map[string]any{
			"saveName": save.Player.Name,
			"gameId":   "sdv",
		},
		"summary":  summary,
		"sections": sections,
	}); err != nil {
		os.Exit(1)
	}
}

// SaveGame is the root XML element of a Stardew Valley save file.
type SaveGame struct {
	XMLName       xml.Name `xml:"SaveGame"`
	Player        Player   `xml:"player"`
	CurrentSeason string   `xml:"currentSeason"`
	WhichFarm     int      `xml:"whichFarm"`
	GameVersion   string   `xml:"gameVersion"`
}

// Player holds the farmer's data from the <player> element.
type Player struct {
	Name               string      `xml:"name"`
	FarmName           string      `xml:"farmName"`
	FavoriteThing      string      `xml:"favoriteThing"`
	Gender             string      `xml:"Gender"`
	Money              int         `xml:"money"`
	TotalMoneyEarned   int         `xml:"totalMoneyEarned"`
	MillisecondsPlayed int64       `xml:"millisecondsPlayed"`
	DayOfMonth         int         `xml:"dayOfMonthForSaveGame"`
	Year               int         `xml:"yearForSaveGame"`
	GameVersion        string      `xml:"gameVersion"`
	ExperiencePoints   IntList     `xml:"experiencePoints"`
	Professions        IntList     `xml:"professions"`
	WhichPetType       string           `xml:"whichPetType"`
	WhichPetBreed      int              `xml:"whichPetBreed"`
	Stats              PlayerStats      `xml:"stats"`
	FriendshipData     []FriendshipItem `xml:"friendshipData>item"`
	Items              []Item           `xml:"items>Item"`
	Spouse             string           `xml:"spouse"`
	DaysMarried        int              `xml:"daysMarried"`
	Children           []Child          `xml:"children>NPC"`
}

// IntList parses <element><int>...</int></element> lists.
type IntList struct {
	Values []int `xml:"int"`
}

// PlayerStats holds the 1.6 key-value stats dictionary.
type PlayerStats struct {
	Values []StatsItem `xml:"Values>item"`
}

// StatsItem is a single key-value entry in the stats dictionary.
type StatsItem struct {
	Key   string `xml:"key>string"`
	Value uint64 `xml:"value>unsignedInt"`
}

// FriendshipItem is a key-value pair in the friendshipData dictionary.
type FriendshipItem struct {
	Key        string     `xml:"key>string"`
	Friendship Friendship `xml:"value>Friendship"`
}

// Friendship holds relationship data with an NPC.
type Friendship struct {
	Points           int    `xml:"Points"`
	GiftsThisWeek    int    `xml:"GiftsThisWeek"`
	GiftsToday       int    `xml:"GiftsToday"`
	TalkedToToday    bool   `xml:"TalkedToToday"`
	Status           string `xml:"Status"`
	RoommateMarriage bool   `xml:"RoommateMarriage"`
}

// Child holds child NPC data.
type Child struct {
	Name string `xml:"name"`
	Age  int    `xml:"daysOld"`
}

// Item represents any item in the player's inventory.
// The xsi:type attribute distinguishes tools, weapons, and objects but all share common fields.
type Item struct {
	Type         string `xml:"http://www.w3.org/2001/XMLSchema-instance type,attr"`
	Name         string `xml:"name"`
	Stack        int    `xml:"stack"`
	Quality      int    `xml:"quality"`
	UpgradeLevel int    `xml:"upgradeLevel"`
	Category     int    `xml:"category"`
}

func parseSave(data []byte) (*SaveGame, error) {
	var save SaveGame
	if err := xml.Unmarshal(data, &save); err != nil {
		return nil, fmt.Errorf("XML parse error: %w", err)
	}
	return &save, nil
}

func buildSummary(save *SaveGame) string {
	season := save.CurrentSeason
	if season == "" {
		season = "spring"
	}
	season = capitalizeFirst(season)
	return fmt.Sprintf("%s, Year %d %s %d, %s Farm (%s)",
		save.Player.Name,
		save.Player.Year,
		season,
		save.Player.DayOfMonth,
		save.Player.FarmName,
		farmTypeName(save.WhichFarm),
	)
}

func farmTypeName(id int) string {
	switch id {
	case 0:
		return "Standard"
	case 1:
		return "Riverland"
	case 2:
		return "Forest"
	case 3:
		return "Hill-top"
	case 4:
		return "Wilderness"
	case 5:
		return "Four Corners"
	case 6:
		return "Beach"
	case 7:
		return "Meadowlands"
	default:
		return fmt.Sprintf("Unknown(%d)", id)
	}
}

// skillName returns the human-readable name for a skill index.
func skillName(idx int) string {
	switch idx {
	case 0:
		return "Farming"
	case 1:
		return "Fishing"
	case 2:
		return "Foraging"
	case 3:
		return "Mining"
	case 4:
		return "Combat"
	case 5:
		return "Luck"
	default:
		return fmt.Sprintf("Skill#%d", idx)
	}
}

// professionName returns the human-readable name for a profession ID.
func professionName(id int) string {
	names := map[int]string{
		// Farming
		0: "Rancher", 1: "Tiller",
		2: "Coopmaster", 3: "Shepherd",
		4: "Artisan", 5: "Agriculturist",
		// Fishing
		6: "Fisher", 7: "Trapper",
		8: "Angler", 9: "Pirate",
		10: "Mariner", 11: "Luremaster",
		// Foraging
		12: "Forester", 13: "Gatherer",
		14: "Lumberjack", 15: "Tapper",
		16: "Botanist", 17: "Tracker",
		// Mining
		18: "Miner", 19: "Geologist",
		20: "Blacksmith", 21: "Prospector",
		22: "Excavator", 23: "Gemologist",
		// Combat
		24: "Fighter", 25: "Scout",
		26: "Brute", 27: "Defender",
		28: "Acrobat", 29: "Desperado",
	}
	if name, ok := names[id]; ok {
		return name
	}
	return fmt.Sprintf("Profession#%d", id)
}

// skillLevel returns the level for a given XP amount.
func skillLevel(xp int) int {
	// Stardew Valley skill level thresholds
	thresholds := []int{0, 100, 380, 770, 1300, 2150, 3300, 4800, 6900, 10000, 15000}
	for i := len(thresholds) - 1; i >= 0; i-- {
		if xp >= thresholds[i] {
			return i
		}
	}
	return 0
}

func buildSections(save *SaveGame) map[string]any {
	return map[string]any{
		"character": map[string]any{
			"description": "Character overview, skills, and basic stats",
			"data":        buildCharacterSection(save),
		},
		"social": map[string]any{
			"description": "NPC relationships, friendship levels, marriage, and children",
			"data":        buildSocialSection(save),
		},
		"inventory": map[string]any{
			"description": "Backpack contents, tools with upgrade levels, and weapons",
			"data":        buildInventorySection(save),
		},
	}
}

// ignoredNPCs are non-social characters that appear in friendshipData but shouldn't be shown.
var ignoredNPCs = map[string]bool{
	"Henchman": true, "Bouncer": true, "Mister Qi": true,
	"Gunther": true, "Marlon": true, "Birdie": true,
	"Fizz": true, "Pet": true, "Raccoon": true, "Truffle Crab": true,
}

// datableNPCs is the set of NPCs that can be romanced (max 10 hearts, or 14 if married).
var datableNPCs = map[string]bool{
	"Abigail": true, "Alex": true, "Elliott": true, "Emily": true,
	"Haley": true, "Harvey": true, "Leah": true, "Maru": true,
	"Penny": true, "Sam": true, "Sebastian": true, "Shane": true,
}

func buildSocialSection(save *SaveGame) map[string]any {
	p := save.Player

	relationships := make([]map[string]any, 0, len(p.FriendshipData))
	for _, item := range p.FriendshipData {
		if ignoredNPCs[item.Key] {
			continue
		}
		f := item.Friendship
		heartLevel := f.Points / 250

		maxHearts := 8
		if datableNPCs[item.Key] {
			maxHearts = 10
		}

		status := f.Status
		if status == "" {
			status = "Friendly"
		}

		switch status {
		case "Married":
			maxHearts = 14
		case "Dating", "Engaged":
			maxHearts = 10
		case "Roommate":
			maxHearts = 10
		}

		if heartLevel > maxHearts {
			heartLevel = maxHearts
		}

		rel := map[string]any{
			"name":             item.Key,
			"friendshipPoints": f.Points,
			"heartLevel":       heartLevel,
			"maxHearts":        maxHearts,
			"status":           status,
		}

		if f.GiftsThisWeek > 0 {
			rel["giftsThisWeek"] = f.GiftsThisWeek
		}
		if f.TalkedToToday {
			rel["talkedToToday"] = true
		}

		relationships = append(relationships, rel)
	}

	spouse := p.Spouse
	children := make([]map[string]any, 0, len(p.Children))
	for _, child := range p.Children {
		children = append(children, map[string]any{
			"name": child.Name,
			"age":  child.Age,
		})
	}

	return map[string]any{
		"relationships": relationships,
		"spouse":        spouse,
		"children":      children,
	}
}

// toolTypes is the set of xsi:type values that represent tools.
var toolTypes = map[string]bool{
	"Hoe": true, "Pickaxe": true, "Axe": true, "WateringCan": true,
	"FishingRod": true, "Pan": true, "Shears": true, "MilkPail": true,
	"Wand": true,
}

// qualityName returns a human-readable quality name for a quality ID.
func qualityName(id int) string {
	switch id {
	case 1:
		return "Silver"
	case 2:
		return "Gold"
	case 4:
		return "Iridium"
	default:
		return "Normal"
	}
}

// upgradeLevelName returns a human-readable tool upgrade level name.
func upgradeLevelName(id int) string {
	switch id {
	case 1:
		return "Copper"
	case 2:
		return "Steel"
	case 3:
		return "Gold"
	case 4:
		return "Iridium"
	default:
		return "Basic"
	}
}

func buildInventorySection(save *SaveGame) map[string]any {
	p := save.Player

	tools := make([]map[string]any, 0)
	weapons := make([]map[string]any, 0)
	items := make([]map[string]any, 0)

	for _, item := range p.Items {
		if item.Name == "" {
			continue
		}

		switch {
		case toolTypes[item.Type]:
			tools = append(tools, map[string]any{
				"name":         item.Name,
				"upgradeLevel": upgradeLevelName(item.UpgradeLevel),
			})
		case item.Type == "MeleeWeapon":
			weapons = append(weapons, map[string]any{
				"name": item.Name,
			})
		default:
			entry := map[string]any{
				"name":  item.Name,
				"stack": item.Stack,
			}
			if q := qualityName(item.Quality); q != "Normal" {
				entry["quality"] = q
			} else {
				entry["quality"] = "Normal"
			}
			items = append(items, entry)
		}
	}

	return map[string]any{
		"tools":   tools,
		"weapons": weapons,
		"items":   items,
	}
}

func buildCharacterSection(save *SaveGame) map[string]any {
	p := save.Player
	hours := float64(p.MillisecondsPlayed) / 3600000.0

	season := capitalizeFirst(save.CurrentSeason)

	skills := make([]map[string]any, 0, len(p.ExperiencePoints.Values))
	for i, xp := range p.ExperiencePoints.Values {
		skills = append(skills, map[string]any{
			"name":  skillName(i),
			"level": skillLevel(xp),
			"xp":    xp,
		})
	}

	professions := make([]string, 0, len(p.Professions.Values))
	for _, id := range p.Professions.Values {
		professions = append(professions, professionName(id))
	}

	var masteryXP int
	for _, item := range p.Stats.Values {
		if item.Key == "MasteryExp" {
			masteryXP = int(item.Value)
			break
		}
	}

	result := map[string]any{
		"name":             p.Name,
		"farmName":         p.FarmName,
		"farmType":         farmTypeName(save.WhichFarm),
		"favoriteThing":    p.FavoriteThing,
		"gender":           p.Gender,
		"gameVersion":      save.GameVersion,
		"date":             fmt.Sprintf("Year %d, %s %d", p.Year, season, p.DayOfMonth),
		"season":           season,
		"day":              p.DayOfMonth,
		"year":             p.Year,
		"playtimeHours":    fmt.Sprintf("%.1f", hours),
		"money":            p.Money,
		"totalMoneyEarned": p.TotalMoneyEarned,
		"skills":           skills,
		"professions":      professions,
		"masteryXP":        masteryXP,
	}

	if p.WhichPetType != "" {
		result["petType"] = p.WhichPetType
		result["petBreed"] = p.WhichPetBreed
	}

	return result
}

func capitalizeFirst(s string) string {
	if len(s) == 0 {
		return s
	}
	return string(s[0]-32) + s[1:]
}

func writeStatusf(enc *json.Encoder, format string, args ...any) {
	if err := enc.Encode(map[string]any{
		"type":    "status",
		"message": fmt.Sprintf(format, args...),
	}); err != nil {
		os.Exit(1)
	}
}

func writeError(enc *json.Encoder, errType, message string) {
	if err := enc.Encode(map[string]any{
		"type":      "error",
		"errorType": errType,
		"message":   message,
	}); err != nil {
		os.Exit(1)
	}
}
