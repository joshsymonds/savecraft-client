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
	"strconv"
	"strings"
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
	XMLName       xml.Name         `xml:"SaveGame"`
	Player        Player           `xml:"player"`
	CurrentSeason string           `xml:"currentSeason"`
	WhichFarm     int              `xml:"whichFarm"`
	GameVersion   string           `xml:"gameVersion"`
	BundleData    []StringKV       `xml:"bundleData>item"`
	Locations     []GameLocation   `xml:"locations>GameLocation"`
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
	MailReceived       StringList       `xml:"mailReceived"`
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

// StringKV is a key-value pair where both are strings (used in bundleData).
type StringKV struct {
	Key   string `xml:"key>string"`
	Value string `xml:"value>string"`
}

// StringList parses <element><string>...</string></element> lists.
type StringList struct {
	Values []string `xml:"string"`
}

// GameLocation is a location in the game world. xsi:type distinguishes subtypes.
type GameLocation struct {
	Type          string        `xml:"http://www.w3.org/2001/XMLSchema-instance type,attr"`
	AreasComplete BoolList      `xml:"areasComplete"`
	Bundles       []BundleState `xml:"bundles>item"`
}

// BoolList parses <element><boolean>...</boolean></element> lists.
type BoolList struct {
	Values []bool `xml:"boolean"`
}

// BundleState holds completion state for a single bundle.
type BundleState struct {
	Key       int      `xml:"key>int"`
	Completed BoolList `xml:"value>ArrayOfBoolean"`
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
		"bundles": map[string]any{
			"description": "Community center bundles or Joja route, per-room and per-bundle completion",
			"data":        buildBundlesSection(save),
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

// roomOrder defines the canonical room ordering for the community center.
var roomOrder = []string{"Pantry", "Crafts Room", "Fish Tank", "Boiler Room", "Vault", "Bulletin Board", "Abandoned Joja Mart"}

// roomIndex maps room names to their areasComplete index.
var roomIndex = map[string]int{
	"Pantry": 0, "Crafts Room": 1, "Fish Tank": 2,
	"Boiler Room": 3, "Vault": 4, "Bulletin Board": 5,
}

func buildBundlesSection(save *SaveGame) map[string]any {
	// Detect Joja route
	isJoja := false
	for _, m := range save.Player.MailReceived.Values {
		if m == "JojaMember" {
			isJoja = true
			break
		}
	}

	route := "Community Center"
	if isJoja {
		route = "Joja"
	}

	// Find CommunityCenter location
	var cc *GameLocation
	for i := range save.Locations {
		if save.Locations[i].Type == "CommunityCenter" {
			cc = &save.Locations[i]
			break
		}
	}

	// Build completion state: bundleID -> []bool (which item slots are filled)
	bundleComplete := map[int][]bool{}
	if cc != nil {
		for _, bs := range cc.Bundles {
			bundleComplete[bs.Key] = bs.Completed.Values
		}
	}

	// Parse bundleData definitions, grouped by room
	type bundleItem struct {
		name      string
		quantity  int
		quality   int
		slotIndex int
	}
	type bundleDef struct {
		name     string
		id       int
		required int
		items    []bundleItem
	}
	roomBundles := map[string][]bundleDef{}

	for _, kv := range save.BundleData {
		keyParts := strings.SplitN(kv.Key, "/", 2)
		if len(keyParts) != 2 {
			continue
		}
		room := keyParts[0]
		bundleID, _ := strconv.Atoi(keyParts[1])

		valParts := strings.Split(kv.Value, "/")
		if len(valParts) < 3 {
			continue
		}
		bundleName := valParts[0]
		itemsStr := valParts[2]
		requiredStr := ""
		if len(valParts) > 4 {
			requiredStr = valParts[4]
		}

		// Parse item triplets: itemID quantity quality
		triplets := strings.Fields(itemsStr)
		var items []bundleItem
		for i := 0; i+2 < len(triplets); i += 3 {
			itemID, _ := strconv.Atoi(triplets[i])
			qty, _ := strconv.Atoi(triplets[i+1])
			qual, _ := strconv.Atoi(triplets[i+2])

			var name string
			if itemID == -1 {
				name = formatGold(qty)
			} else if n, ok := bundleItemNames[itemID]; ok {
				name = n
			} else {
				name = fmt.Sprintf("Item #%d", itemID)
			}

			items = append(items, bundleItem{
				name:      name,
				quantity:  qty,
				quality:   qual,
				slotIndex: i / 3,
			})
		}

		required := len(items)
		if requiredStr != "" {
			if r, err := strconv.Atoi(requiredStr); err == nil {
				required = r
			}
		}

		roomBundles[room] = append(roomBundles[room], bundleDef{
			name:     bundleName,
			id:       bundleID,
			required: required,
			items:    items,
		})
	}

	// Build rooms output
	allRoomsComplete := true
	rooms := make([]map[string]any, 0)
	for _, roomName := range roomOrder {
		bundles, ok := roomBundles[roomName]
		if !ok {
			continue
		}

		roomComplete := false
		if idx, ok := roomIndex[roomName]; ok && cc != nil {
			if idx < len(cc.AreasComplete.Values) {
				roomComplete = cc.AreasComplete.Values[idx]
			}
		} else {
			// Rooms not in areasComplete (e.g. Abandoned Joja Mart):
			// derive completion from whether all bundles are complete.
			roomComplete = true
			for _, b := range bundles {
				completedSlots := bundleComplete[b.id]
				count := 0
				for _, item := range b.items {
					if item.slotIndex < len(completedSlots) && completedSlots[item.slotIndex] {
						count++
					}
				}
				if count < b.required {
					roomComplete = false
					break
				}
			}
		}
		if !roomComplete {
			allRoomsComplete = false
		}

		bundleOut := make([]map[string]any, 0, len(bundles))
		for _, b := range bundles {
			completedSlots := bundleComplete[b.id]
			completedCount := 0
			itemsOut := make([]map[string]any, 0, len(b.items))

			for _, item := range b.items {
				completed := false
				if item.slotIndex < len(completedSlots) {
					completed = completedSlots[item.slotIndex]
				}
				if completed {
					completedCount++
				}

				entry := map[string]any{
					"name":      item.name,
					"completed": completed,
				}
				if item.quantity > 1 {
					entry["quantity"] = item.quantity
				}
				if q := qualityName(item.quality); q != "Normal" {
					entry["quality"] = q
				}
				itemsOut = append(itemsOut, entry)
			}

			bundleOut = append(bundleOut, map[string]any{
				"name":      b.name,
				"completed": completedCount >= b.required,
				"have":      completedCount,
				"need":      b.required,
				"items":     itemsOut,
			})
		}

		rooms = append(rooms, map[string]any{
			"name":     roomName,
			"complete": roomComplete,
			"bundles":  bundleOut,
		})
	}

	return map[string]any{
		"route":    route,
		"complete": allRoomsComplete,
		"rooms":    rooms,
	}
}

// formatGold formats a gold amount with comma separators.
func formatGold(n int) string {
	s := strconv.Itoa(n)
	if len(s) <= 3 {
		return s + "g"
	}
	// Insert commas from right
	var result []byte
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}
	return string(result) + "g"
}

// bundleItemNames maps item IDs to human-readable names for community center bundles.
var bundleItemNames = map[int]string{
	16: "Wild Horseradish", 18: "Daffodil", 20: "Leek", 22: "Dandelion",
	24: "Parsnip", 62: "Aquamarine", 74: "Prismatic Shard", 78: "Cave Carrot",
	80: "Quartz", 82: "Fire Quartz", 84: "Frozen Tear", 86: "Earth Crystal",
	88: "Coconut", 90: "Cactus Fruit",
	128: "Pufferfish", 130: "Tuna", 131: "Sardine", 132: "Bream",
	136: "Largemouth Bass", 140: "Walleye", 142: "Carp", 143: "Catfish",
	145: "Sunfish", 148: "Eel", 150: "Red Snapper", 156: "Ghostfish",
	164: "Sandfish", 174: "Large Egg (White)", 178: "Hay",
	182: "Large Egg (Brown)", 186: "Large Milk",
	188: "Green Bean", 190: "Cauliflower", 192: "Potato", 194: "Fried Egg",
	228: "Maki Roll", 254: "Melon", 256: "Tomato", 257: "Morel",
	258: "Blueberry", 259: "Fiddlehead Fern", 260: "Hot Pepper",
	262: "Wheat", 266: "Red Cabbage", 270: "Corn", 272: "Eggplant",
	276: "Pumpkin", 280: "Yam",
	334: "Copper Bar", 335: "Iron Bar", 336: "Gold Bar",
	340: "Honey", 344: "Jelly", 348: "Wine",
	372: "Clam", 376: "Poppy", 388: "Wood", 390: "Stone",
	392: "Nautilus Shell", 396: "Spice Berry", 397: "Sea Urchin",
	398: "Grape", 402: "Sweet Pea",
	404: "Common Mushroom", 406: "Wild Plum", 408: "Hazelnut", 410: "Blackberry",
	412: "Winter Root", 414: "Crystal Fruit", 416: "Snow Yam", 418: "Crocus",
	420: "Red Mushroom", 421: "Sunflower", 422: "Purple Mushroom",
	424: "Cheese", 426: "Goat Cheese", 428: "Cloth", 430: "Truffle",
	432: "Truffle Oil", 438: "L. Goat Milk", 440: "Wool", 442: "Duck Egg",
	444: "Duck Feather", 445: "Caviar", 446: "Rabbit's Foot",
	454: "Ancient Fruit", 536: "Frozen Geode",
	613: "Apple", 634: "Apricot", 635: "Orange", 636: "Peach",
	637: "Pomegranate", 638: "Cherry",
	698: "Sturgeon", 699: "Tiger Trout", 700: "Bullhead", 701: "Tilapia",
	702: "Chub", 706: "Shad", 709: "Hardwood",
	715: "Lobster", 716: "Crayfish", 717: "Crab", 718: "Cockle",
	719: "Mussel", 720: "Shrimp", 721: "Snail", 722: "Periwinkle",
	723: "Oyster", 724: "Maple Syrup", 725: "Oak Resin", 726: "Pine Tar",
	734: "Woodskip", 766: "Slime", 767: "Bat Wing",
	768: "Solar Essence", 769: "Void Essence",
	795: "Void Salmon", 807: "Dinosaur Mayonnaise",
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
