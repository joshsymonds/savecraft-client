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
	"slices"
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
	summary := buildSummary(save, sections)

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
	XMLName                xml.Name       `xml:"SaveGame"`
	Player                 Player         `xml:"player"`
	CurrentSeason          string         `xml:"currentSeason"`
	WhichFarm              int            `xml:"whichFarm"`
	GameVersion            string         `xml:"gameVersion"`
	BundleData             []StringKV     `xml:"bundleData>item"`
	Locations              []GameLocation `xml:"locations>GameLocation"`
	GoldenWalnuts          int            `xml:"goldenWalnuts"`
	GoldenWalnutsFound     int            `xml:"goldenWalnutsFound"`
	CompletedSpecialOrders StringList     `xml:"completedSpecialOrders"`
}

// Player holds the farmer's data from the <player> element.
type Player struct {
	Name               string           `xml:"name"`
	FarmName           string           `xml:"farmName"`
	FavoriteThing      string           `xml:"favoriteThing"`
	Gender             string           `xml:"Gender"`
	Money              int              `xml:"money"`
	TotalMoneyEarned   int              `xml:"totalMoneyEarned"`
	MillisecondsPlayed int64            `xml:"millisecondsPlayed"`
	DayOfMonth         int              `xml:"dayOfMonthForSaveGame"`
	Year               int              `xml:"yearForSaveGame"`
	GameVersion        string           `xml:"gameVersion"`
	ExperiencePoints   IntList          `xml:"experiencePoints"`
	Professions        IntList          `xml:"professions"`
	WhichPetType       string           `xml:"whichPetType"`
	WhichPetBreed      int              `xml:"whichPetBreed"`
	Stats              PlayerStats      `xml:"stats"`
	FriendshipData     []FriendshipItem `xml:"friendshipData>item"`
	Items              []Item           `xml:"items>Item"`
	Spouse             string           `xml:"spouse"`
	DaysMarried        int              `xml:"daysMarried"`
	Children           []Child          `xml:"children>NPC"`
	MailReceived       StringList       `xml:"mailReceived"`
	SecretNotesSeen    IntList          `xml:"secretNotesSeen"`
	FishCaught         []IntArrayKV     `xml:"fishCaught>item"`
	CookingRecipes     []StringIntKV    `xml:"cookingRecipes>item"`
	RecipesCooked      []IntIntKV       `xml:"recipesCooked>item"`
	CraftingRecipes    []StringIntKV    `xml:"craftingRecipes>item"`
	BasicShipped       []IntIntKV       `xml:"basicShipped>item"`
	MineralsFound      []IntIntKV       `xml:"mineralsFound>item"`
	ArchaeologyFound   []IntArrayKV     `xml:"archaeologyFound>item"`
}

// IntList parses <element><int>...</int></element> lists.
type IntList struct {
	Values []int `xml:"int"`
}

// PlayerStats holds stats in either 1.6 key-value format or 1.5 direct elements.
type PlayerStats struct {
	Values                 []StatsItem   `xml:"Values>item"`
	QuestsCompleted        uint64        `xml:"QuestsCompleted"` // 1.5 direct element
	SpecificMonstersKilled []StringIntKV `xml:"specificMonstersKilled>item"`
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

// StringIntKV maps a string key to an int value (cooking/crafting recipes).
type StringIntKV struct {
	Key   string `xml:"key>string"`
	Value int    `xml:"value>int"`
}

// IntIntKV maps an int key to an int value (shipped items, minerals).
// In 1.6, keys may be stored as strings like "(O)131"; we parse both.
type IntIntKV struct {
	KeyInt int    `xml:"key>int"`
	KeyStr string `xml:"key>string"`
	Value  int    `xml:"value>int"`
}

// IntArrayKV maps an int/string key to an ArrayOfInt value (fish caught, archaeology).
type IntArrayKV struct {
	KeyInt int     `xml:"key>int"`
	KeyStr string  `xml:"key>string"`
	Values IntList `xml:"value>ArrayOfInt"`
}

// StringList parses <element><string>...</string></element> lists.
type StringList struct {
	Values []string `xml:"string"`
}

// GameLocation is a location in the game world. xsi:type distinguishes subtypes.
type GameLocation struct {
	Type            string               `xml:"http://www.w3.org/2001/XMLSchema-instance type,attr"`
	AreasComplete   BoolList             `xml:"areasComplete"`
	Bundles         []BundleState        `xml:"bundles>item"`
	Buildings       []Building           `xml:"buildings>Building"`
	TerrainFeatures []TerrainFeatureItem `xml:"terrainFeatures>item"`
	Objects         []FarmObjectItem     `xml:"objects>item"`
}

// Building represents a farm building.
type Building struct {
	Type      string `xml:"buildingType"`
	TileX     int    `xml:"tileX"`
	TileY     int    `xml:"tileY"`
	TilesWide int    `xml:"tilesWide"`
	TilesHigh int    `xml:"tilesHigh"`
}

// TerrainFeatureItem is a positioned terrain feature in a location.
type TerrainFeatureItem struct {
	X       int            `xml:"key>Vector2>X"`
	Y       int            `xml:"key>Vector2>Y"`
	Feature TerrainFeature `xml:"value>TerrainFeature"`
}

// TerrainFeature is a tile-based feature like tilled soil, trees, or grass.
type TerrainFeature struct {
	Type string `xml:"http://www.w3.org/2001/XMLSchema-instance type,attr"`
	Crop Crop   `xml:"crop"`
}

// Crop holds data for a planted crop on a HoeDirt tile.
type Crop struct {
	IndexOfHarvest int  `xml:"indexOfHarvest"`
	CurrentPhase   int  `xml:"currentPhase"`
	Dead           bool `xml:"dead"`
	FullGrown      bool `xml:"fullGrown"`
}

// FarmObjectItem is a positioned object in a location.
type FarmObjectItem struct {
	X      int        `xml:"key>Vector2>X"`
	Y      int        `xml:"key>Vector2>Y"`
	Object FarmObject `xml:"value>Object"`
}

// FarmObject is an placed object (sprinkler, machine, chest, etc.).
type FarmObject struct {
	Name         string `xml:"name"`
	BigCraftable bool   `xml:"bigCraftable"`
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

func buildSummary(save *SaveGame, sections map[string]any) string {
	season := save.CurrentSeason
	if season == "" {
		season = "spring"
	}
	season = capitalizeFirst(season)
	perfData := sections["perfection"].(map[string]any)["data"].(map[string]any)
	pct := int(perfData["percentage"].(float64))
	return fmt.Sprintf("%s, Year %d %s %d, %s Farm (%s), %d%% Perfection",
		save.Player.Name,
		save.Player.Year,
		season,
		save.Player.DayOfMonth,
		save.Player.FarmName,
		farmTypeName(save.WhichFarm),
		pct,
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
	sections := map[string]any{
		"character": map[string]any{
			"description": "Skills, professions, XP, mastery — fetch to evaluate skill builds or answer 'what level is my farming?'",
			"data":        buildCharacterSection(save),
		},
		"social": map[string]any{
			"description": "NPC friendship points, heart levels, gift tracking, marriage, children — fetch for gift/relationship advice",
			"data":        buildSocialSection(save),
		},
		"inventory": map[string]any{
			"description": "Backpack contents with tool upgrade levels, weapons, item stacks and quality — fetch to evaluate gear or plan upgrades",
			"data":        buildInventorySection(save),
		},
		"bundles": map[string]any{
			"description": "Community center (or Joja) bundle completion per room and per item slot — fetch to plan which items to gather next",
			"data":        buildBundlesSection(save),
		},
		"collections": map[string]any{
			"description": "Fish caught, cooking/crafting recipe progress, shipping log, museum donations — fetch to find completion gaps or plan what to collect next",
			"data":        buildCollectionsSection(save),
		},
		"progress": map[string]any{
			"description": "Stardrops, golden walnuts, quests, special orders, monster slayer goals — fetch for endgame/completionist tracking",
			"data":        buildProgressSection(save),
		},
		"perfection": map[string]any{
			"description": "Perfection tracker percentage breakdown by category with points earned — fetch to identify lowest-scoring categories to prioritize",
			"data":        buildPerfectionSection(save),
		},
		"farm": map[string]any{
			"description": "Farm buildings, active crops, sprinkler coverage zones, scarecrows, machines — fetch for farm layout planning or crop advice",
			"data":        buildFarmSection(save),
		},
	}

	// Build the overview section last so it can reference other section names.
	sections["player_summary"] = map[string]any{
		"description": "Farmer identity, date, money, skill levels, key progression milestones, and index of available sections — always shown first",
		"data":        buildPlayerSummarySection(save, sections),
	}

	return sections
}

func buildPlayerSummarySection(save *SaveGame, sections map[string]any) map[string]any {
	p := save.Player
	season := capitalizeFirst(save.CurrentSeason)

	// Skill levels as a compact map.
	skills := map[string]int{}
	for i, xp := range p.ExperiencePoints.Values {
		skills[skillName(i)] = skillLevel(xp)
	}

	// Professions list.
	professions := make([]string, 0, len(p.Professions.Values))
	for _, id := range p.Professions.Values {
		professions = append(professions, professionName(id))
	}

	// Community center / Joja progress summary.
	bundleData := sections["bundles"].(map[string]any)["data"].(map[string]any)
	ccComplete := bundleData["complete"].(bool)
	ccRoute := bundleData["route"].(string)
	roomsComplete := 0
	roomsTotal := 0
	if rooms, ok := bundleData["rooms"].([]map[string]any); ok {
		roomsTotal = len(rooms)
		for _, r := range rooms {
			if r["complete"] == true {
				roomsComplete++
			}
		}
	}

	// Perfection snapshot.
	perfData := sections["perfection"].(map[string]any)["data"].(map[string]any)
	perfPct := perfData["percentage"].(float64)

	// Key relationship summary: spouse + heart counts.
	spouse := p.Spouse
	maxedFriends := 0
	totalFriends := 0
	for _, item := range p.FriendshipData {
		if ignoredNPCs[item.Key] {
			continue
		}
		totalFriends++
		threshold := 2500
		if datableNPCs[item.Key] {
			threshold = 2000
		}
		if item.Friendship.Points >= threshold {
			maxedFriends++
		}
	}

	// Farm summary.
	farmData := sections["farm"].(map[string]any)["data"].(map[string]any)
	farmSummary, _ := farmData["summary"].(map[string]any)

	// Build section index: name + description for each sibling section.
	type sectionEntry struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	sectionIndex := make([]sectionEntry, 0, len(sections))
	for name, sec := range sections {
		if name == "player_summary" {
			continue
		}
		if secMap, ok := sec.(map[string]any); ok {
			desc, _ := secMap["description"].(string)
			sectionIndex = append(sectionIndex, sectionEntry{Name: name, Description: desc})
		}
	}
	// Sort for deterministic output.
	for i := 0; i < len(sectionIndex); i++ {
		for j := i + 1; j < len(sectionIndex); j++ {
			if sectionIndex[j].Name < sectionIndex[i].Name {
				sectionIndex[i], sectionIndex[j] = sectionIndex[j], sectionIndex[i]
			}
		}
	}

	hours := float64(p.MillisecondsPlayed) / 3600000.0

	result := map[string]any{
		"farmer":   p.Name,
		"farmName": p.FarmName,
		"farmType": farmTypeName(save.WhichFarm),
		"date": map[string]any{
			"year":   p.Year,
			"season": season,
			"day":    p.DayOfMonth,
		},
		"money":            p.Money,
		"totalMoneyEarned": p.TotalMoneyEarned,
		"playtimeHours":    fmt.Sprintf("%.1f", hours),
		"gameVersion":      save.GameVersion,
		"skills":           skills,
		"professions":      professions,
		"communityCenter": map[string]any{
			"route":         ccRoute,
			"complete":      ccComplete,
			"roomsComplete": roomsComplete,
			"roomsTotal":    roomsTotal,
		},
		"perfectionPct": perfPct,
		"social": map[string]any{
			"spouse":       spouse,
			"maxedFriends": maxedFriends,
			"totalFriends": totalFriends,
		},
		"farm": map[string]any{
			"totalBuildings":  farmSummary["totalBuildings"],
			"totalCrops":      farmSummary["totalCrops"],
			"totalSprinklers": farmSummary["totalSprinklers"],
		},
		"stardropsFound": len(stardropFlagsFound(save)),
		"stardropsTotal": len(stardropFlags),
		"sections":       sectionIndex,
	}

	return result
}

// stardropFlagsFound returns the list of stardrop sources the player has found.
func stardropFlagsFound(save *SaveGame) []string {
	mailSet := map[string]bool{}
	for _, m := range save.Player.MailReceived.Values {
		mailSet[m] = true
	}
	found := make([]string, 0)
	for _, sd := range stardropFlags {
		if mailSet[sd.flag] {
			found = append(found, sd.source)
		}
	}
	return found
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
			entry["quality"] = qualityName(item.Quality)
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
	isJoja := slices.Contains(save.Player.MailReceived.Values, "JojaMember")

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
			} else if n, ok := objectNames[itemID]; ok {
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

// resolveItemID extracts the numeric item ID from 1.5 (int key) or 1.6 (string key like "(O)131") format.
func resolveItemID(keyInt int, keyStr string) int {
	if keyInt != 0 {
		return keyInt
	}
	s := strings.TrimPrefix(keyStr, "(O)")
	id, _ := strconv.Atoi(s)
	return id
}

// itemName returns the human-readable name for an item ID, with a fallback.
func itemName(id int) string {
	if name, ok := objectNames[id]; ok {
		return name
	}
	return fmt.Sprintf("Item #%d", id)
}

func buildCollectionsSection(save *SaveGame) map[string]any {
	p := save.Player

	// Fish
	fishCaught := make([]map[string]any, 0, len(p.FishCaught))
	for _, fc := range p.FishCaught {
		id := resolveItemID(fc.KeyInt, fc.KeyStr)
		entry := map[string]any{
			"name": itemName(id),
		}
		if len(fc.Values.Values) > 0 {
			entry["timesCaught"] = fc.Values.Values[0]
		}
		if len(fc.Values.Values) > 1 && fc.Values.Values[1] > 0 {
			entry["maxSize"] = fc.Values.Values[1]
		}
		fishCaught = append(fishCaught, entry)
	}

	// Cooking: cookingRecipes has names of learned recipes.
	// recipesCooked (keyed by item ID) tracks actual cook counts.
	cookedIDs := map[int]bool{}
	for _, rc := range p.RecipesCooked {
		id := resolveItemID(rc.KeyInt, rc.KeyStr)
		if rc.Value > 0 {
			cookedIDs[id] = true
		}
	}
	recipesLearned := len(p.CookingRecipes)
	recipesCooked := len(cookedIDs)
	notCooked := make([]string, 0)
	// In 1.6+, CookingRecipes.Value tracks cook count so we can identify uncooked recipes.
	// In 1.5, Value is always 0 regardless of cooking — skip notCooked for those saves.
	is16 := save.GameVersion >= "1.6"
	for _, cr := range p.CookingRecipes {
		if !is16 {
			continue
		}
		if cr.Value == 0 {
			notCooked = append(notCooked, cr.Key)
		}
	}

	// Crafting
	craftLearned := len(p.CraftingRecipes)
	craftCrafted := 0
	notCrafted := make([]string, 0)
	for _, cr := range p.CraftingRecipes {
		if cr.Value > 0 {
			craftCrafted++
		} else {
			notCrafted = append(notCrafted, cr.Key)
		}
	}

	// Shipping
	uniqueShipped := len(p.BasicShipped)
	totalShipped := 0
	for _, bs := range p.BasicShipped {
		totalShipped += bs.Value
	}

	// Museum
	mineralsFound := len(p.MineralsFound)
	artifactsFound := len(p.ArchaeologyFound)

	return map[string]any{
		"fish": map[string]any{
			"caught":        fishCaught,
			"speciesCaught": len(fishCaught),
		},
		"cooking": map[string]any{
			"recipesLearned": recipesLearned,
			"recipesCooked":  recipesCooked,
			"notYetCooked":   notCooked,
		},
		"crafting": map[string]any{
			"recipesLearned": craftLearned,
			"recipesCrafted": craftCrafted,
			"notYetCrafted":  notCrafted,
		},
		"shipping": map[string]any{
			"uniqueItemsShipped": uniqueShipped,
			"totalItemsShipped":  totalShipped,
		},
		"museum": map[string]any{
			"mineralsFound":  mineralsFound,
			"artifactsFound": artifactsFound,
		},
	}
}

// stardropFlags maps mail flags to their human-readable sources.
var stardropFlags = []struct {
	flag   string
	source string
}{
	{"CF_Fair", "Stardew Valley Fair"},
	{"CF_Mines", "The Mines (Floor 100)"},
	{"CF_Spouse", "Spouse/Roommate"},
	{"CF_Sewer", "Old Master Cannoli"},
	{"CF_Fish", "Master Angler"},
	{"CF_Statue", "Grandpa's Shrine"},
	{"museumComplete", "Museum Collection"},
}

// monsterGoal defines a Monster Eradication Goal category.
type monsterGoal struct {
	Name     string
	Target   int
	Monsters []string
}

// monsterGoals lists the Adventurer's Guild Monster Eradication Goals.
var monsterGoals = []monsterGoal{
	{"Slimes", 1000, []string{"Green Slime", "Frost Jelly", "Sludge", "Tiger Slime", "Prismatic Slime"}},
	{"Void Spirits", 150, []string{"Shadow Brute", "Shadow Shaman", "Shadow Sniper"}},
	{"Bats", 200, []string{"Bat", "Frost Bat", "Lava Bat", "Iridium Bat"}},
	{"Skeletons", 50, []string{"Skeleton", "Skeleton Mage"}},
	{"Cave Insects", 125, []string{"Bug", "Cave Fly", "Grub", "Mutant Fly", "Mutant Grub", "Armored Bug"}},
	{"Duggies", 30, []string{"Duggy", "Magma Duggy"}},
	{"Dust Sprites", 500, []string{"Dust Spirit"}},
	{"Rock Crabs", 60, []string{"Rock Crab", "Lava Crab", "Iridium Crab"}},
	{"Mummies", 100, []string{"Mummy"}},
	{"Pepper Rex", 50, []string{"Pepper Rex"}},
	{"Serpents", 250, []string{"Serpent", "Royal Serpent"}},
	{"Magma Sprites", 150, []string{"Magma Sprite", "Magma Sparker"}},
}

// statValue retrieves a stat from either 1.6 key-value or 1.5 direct element format.
func statValue(stats PlayerStats, key string) uint64 {
	for _, item := range stats.Values {
		if item.Key == key {
			return item.Value
		}
	}
	switch key {
	case "questsCompleted":
		return stats.QuestsCompleted
	}
	return 0
}

func buildProgressSection(save *SaveGame) map[string]any {
	p := save.Player

	// Stardrops
	mailSet := map[string]bool{}
	for _, m := range p.MailReceived.Values {
		mailSet[m] = true
	}

	foundStardrops := make([]string, 0)
	missingStardrops := make([]string, 0)
	for _, sd := range stardropFlags {
		if mailSet[sd.flag] {
			foundStardrops = append(foundStardrops, sd.source)
		} else {
			missingStardrops = append(missingStardrops, sd.source)
		}
	}

	// Monster slayer goals
	monsterKillMap := map[string]int{}
	for _, mk := range p.Stats.SpecificMonstersKilled {
		monsterKillMap[mk.Key] = mk.Value
	}

	goals := make([]map[string]any, 0, len(monsterGoals))
	for _, goal := range monsterGoals {
		killed := 0
		for _, m := range goal.Monsters {
			killed += monsterKillMap[m]
		}
		goals = append(goals, map[string]any{
			"category": goal.Name,
			"killed":   killed,
			"target":   goal.Target,
			"complete": killed >= goal.Target,
		})
	}

	return map[string]any{
		"stardrops": map[string]any{
			"count":    len(foundStardrops),
			"total":    len(stardropFlags),
			"obtained": foundStardrops,
			"missing":  missingStardrops,
		},
		"goldenWalnuts": map[string]any{
			"found": save.GoldenWalnutsFound,
			"total": 130,
		},
		"secretNotesSeen":        len(p.SecretNotesSeen.Values),
		"questsCompleted":        int(statValue(p.Stats, "questsCompleted")),
		"specialOrdersCompleted": len(save.CompletedSpecialOrders.Values),
		"monsterSlayer": map[string]any{
			"goals": goals,
		},
	}
}

// gameTotals returns version-aware totals for shipping, cooking, crafting, fishing.
func gameTotals(version string) (shipping, cooking, crafting, fishing int) {
	if strings.HasPrefix(version, "1.6") {
		return 156, 84, 148, 73
	}
	return 145, 74, 129, 67
}

func buildPerfectionSection(save *SaveGame) map[string]any {
	p := save.Player
	shippingTotal, cookingTotal, craftingTotal, fishingTotal := gameTotals(save.GameVersion)

	// Shipping
	shippingCount := len(p.BasicShipped)

	// Cooking: count distinct cooked recipes
	cookedIDs := map[int]bool{}
	for _, rc := range p.RecipesCooked {
		id := resolveItemID(rc.KeyInt, rc.KeyStr)
		if rc.Value > 0 {
			cookedIDs[id] = true
		}
	}
	cookingCount := len(cookedIDs)

	// Crafting
	craftingCount := 0
	for _, cr := range p.CraftingRecipes {
		if cr.Value > 0 {
			craftingCount++
		}
	}

	// Fishing
	fishingCount := len(p.FishCaught)

	// Great Friends: datable NPCs maxed at 2000 pts, others at 2500
	friendsMaxed := 0
	friendsTotal := 0
	for _, item := range p.FriendshipData {
		if ignoredNPCs[item.Key] {
			continue
		}
		friendsTotal++
		threshold := 2500
		if datableNPCs[item.Key] {
			threshold = 2000
		}
		if item.Friendship.Points >= threshold {
			friendsMaxed++
		}
	}

	// Skills: farmer level = floor(sum_of_levels / 2), max 25
	skillSum := 0
	for _, xp := range p.ExperiencePoints.Values {
		skillSum += skillLevel(xp)
	}
	farmerLevel := skillSum / 2

	// Stardrops
	mailSet := map[string]bool{}
	for _, m := range p.MailReceived.Values {
		mailSet[m] = true
	}
	stardropCount := 0
	for _, sd := range stardropFlags {
		if mailSet[sd.flag] {
			stardropCount++
		}
	}
	allStardrops := stardropCount >= len(stardropFlags)

	// Monster Slayer: all goals complete
	monsterKillMap := map[string]int{}
	for _, mk := range p.Stats.SpecificMonstersKilled {
		monsterKillMap[mk.Key] = mk.Value
	}
	allGoalsComplete := true
	for _, goal := range monsterGoals {
		killed := 0
		for _, m := range goal.Monsters {
			killed += monsterKillMap[m]
		}
		if killed < goal.Target {
			allGoalsComplete = false
			break
		}
	}

	// Golden Walnuts
	walnutCount := save.GoldenWalnutsFound

	// Farm buildings: obelisks and gold clock
	obeliskSet := map[string]bool{
		"Earth Obelisk": true, "Water Obelisk": true,
		"Desert Obelisk": true, "Island Obelisk": true,
	}
	obeliskCount := 0
	hasGoldClock := false
	for i := range save.Locations {
		if save.Locations[i].Type == "Farm" {
			for _, b := range save.Locations[i].Buildings {
				if obeliskSet[b.Type] {
					obeliskCount++
				}
				if b.Type == "Gold Clock" {
					hasGoldClock = true
				}
			}
			break
		}
	}

	// Compute category percentages and earned points
	pctShipping := clampRatio(shippingCount, shippingTotal)
	pctCooking := clampRatio(cookingCount, cookingTotal)
	pctCrafting := clampRatio(craftingCount, craftingTotal)
	pctFishing := clampRatio(fishingCount, fishingTotal)
	pctFriends := clampRatio(friendsMaxed, friendsTotal)
	pctSkills := clampRatio(farmerLevel, 25)
	pctWalnuts := clampRatio(walnutCount, 130)

	goldClockPts := 0.0
	if hasGoldClock {
		goldClockPts = 10
	}
	monsterPts := 0.0
	if allGoalsComplete {
		monsterPts = 10
	}
	stardropPts := 0.0
	if allStardrops {
		stardropPts = 10
	}

	overall := float64(obeliskCount) + goldClockPts + monsterPts + stardropPts +
		15*pctShipping + 11*pctFriends + 10*pctCooking + 10*pctCrafting + 10*pctFishing +
		5*pctWalnuts + 5*pctSkills

	// Round to 1 decimal place
	overall = float64(int(overall*10+0.5)) / 10

	categories := []map[string]any{
		{"name": "Shipping", "weight": 15, "current": shippingCount, "target": shippingTotal, "earned": 15 * pctShipping},
		{"name": "Obelisks", "weight": 4, "current": obeliskCount, "target": 4, "earned": float64(obeliskCount)},
		{"name": "Gold Clock", "weight": 10, "complete": hasGoldClock, "earned": goldClockPts},
		{"name": "Monster Slayer Hero", "weight": 10, "complete": allGoalsComplete, "earned": monsterPts},
		{"name": "Great Friends", "weight": 11, "current": friendsMaxed, "target": friendsTotal, "earned": 11 * pctFriends},
		{"name": "Farmer Level", "weight": 5, "current": farmerLevel, "target": 25, "earned": 5 * pctSkills},
		{"name": "Stardrops", "weight": 10, "complete": allStardrops, "earned": stardropPts},
		{"name": "Cooking", "weight": 10, "current": cookingCount, "target": cookingTotal, "earned": 10 * pctCooking},
		{"name": "Crafting", "weight": 10, "current": craftingCount, "target": craftingTotal, "earned": 10 * pctCrafting},
		{"name": "Fishing", "weight": 10, "current": fishingCount, "target": fishingTotal, "earned": 10 * pctFishing},
		{"name": "Golden Walnuts", "weight": 5, "current": walnutCount, "target": 130, "earned": 5 * pctWalnuts},
	}

	return map[string]any{
		"percentage": overall,
		"categories": categories,
	}
}

// clampRatio returns min(current/total, 1.0), handling zero total.
func clampRatio(current, total int) float64 {
	if total <= 0 {
		return 0
	}
	r := float64(current) / float64(total)
	if r > 1 {
		return 1
	}
	return r
}

// sprinklerNames is the set of object names that are sprinklers.
var sprinklerNames = map[string]bool{
	"Sprinkler": true, "Quality Sprinkler": true, "Iridium Sprinkler": true,
}

// sprinklerRadius returns the watering radius for a sprinkler type.
func sprinklerRadius(name string) int {
	switch name {
	case "Sprinkler":
		return 1
	case "Quality Sprinkler":
		return 1
	case "Iridium Sprinkler":
		return 2
	default:
		return 0
	}
}

// scarecrowNames is the set of object names that provide crop protection.
var scarecrowNames = map[string]bool{
	"Scarecrow": true, "Deluxe Scarecrow": true, "Rarecrow": true,
}

// machineNames is the set of craftable machines placed on the farm.
var machineNames = map[string]bool{
	"Keg": true, "Preserves Jar": true, "Furnace": true, "Recycling Machine": true,
	"Seed Maker": true, "Crystalarium": true, "Mayonnaise Machine": true,
	"Cheese Press": true, "Oil Maker": true, "Loom": true, "Cask": true,
	"Bee House": true, "Lightning Rod": true, "Tapper": true, "Heavy Tapper": true,
	"Worm Bin": true, "Bone Mill": true, "Charcoal Kiln": true,
	"Slime Egg-Press": true, "Mushroom Box": true, "Dehydrator": true,
	"Fish Smoker": true, "Bait Maker": true,
}

func buildFarmSection(save *SaveGame) map[string]any {
	// Find Farm location
	var farm *GameLocation
	for i := range save.Locations {
		if save.Locations[i].Type == "Farm" {
			farm = &save.Locations[i]
			break
		}
	}
	if farm == nil {
		return map[string]any{}
	}

	buildings := parseFarmBuildings(farm)
	crops, tilledTiles := parseFarmCrops(farm)
	sprinklers, scarecrows, machines := parseFarmObjects(farm)
	zones := buildSprinklerZones(sprinklers, farm)

	totalCrops := 0
	for _, c := range crops {
		totalCrops += c["count"].(int)
	}

	return map[string]any{
		"farmType":       farmTypeName(save.WhichFarm),
		"buildings":      buildings,
		"crops":          crops,
		"tilledTiles":    tilledTiles,
		"sprinklerZones": zones,
		"scarecrows":     scarecrows,
		"machines":       machines,
		"summary": map[string]any{
			"totalBuildings":   len(buildings),
			"totalCrops":       totalCrops,
			"totalTilledTiles": tilledTiles,
			"totalSprinklers":  len(sprinklers),
			"totalScarecrows":  len(scarecrows),
		},
	}
}

func parseFarmBuildings(farm *GameLocation) []map[string]any {
	buildings := make([]map[string]any, 0, len(farm.Buildings))
	for _, b := range farm.Buildings {
		buildings = append(buildings, map[string]any{
			"type":     b.Type,
			"position": map[string]any{"x": b.TileX, "y": b.TileY},
			"size":     map[string]any{"width": b.TilesWide, "height": b.TilesHigh},
		})
	}
	return buildings
}

func parseFarmCrops(farm *GameLocation) ([]map[string]any, int) {
	cropCounts := map[string]int{}
	tilledTiles := 0
	for _, tf := range farm.TerrainFeatures {
		if tf.Feature.Type != "HoeDirt" {
			continue
		}
		tilledTiles++
		if tf.Feature.Crop.IndexOfHarvest > 0 && !tf.Feature.Crop.Dead {
			name := itemName(tf.Feature.Crop.IndexOfHarvest)
			cropCounts[name]++
		}
	}

	crops := make([]map[string]any, 0, len(cropCounts))
	for name, count := range cropCounts {
		crops = append(crops, map[string]any{"name": name, "count": count})
	}
	// Sort by count descending for deterministic output
	for i := 0; i < len(crops); i++ {
		for j := i + 1; j < len(crops); j++ {
			if crops[j]["count"].(int) > crops[i]["count"].(int) {
				crops[i], crops[j] = crops[j], crops[i]
			}
		}
	}
	return crops, tilledTiles
}

type sprinklerInfo struct {
	name string
	x, y int
}

func parseFarmObjects(farm *GameLocation) ([]sprinklerInfo, []map[string]any, []map[string]any) {
	var sprinklers []sprinklerInfo
	scarecrows := make([]map[string]any, 0)
	machineCounts := map[string]int{}

	for _, obj := range farm.Objects {
		name := obj.Object.Name
		switch {
		case sprinklerNames[name]:
			sprinklers = append(sprinklers, sprinklerInfo{name: name, x: obj.X, y: obj.Y})
		case scarecrowNames[name]:
			scarecrows = append(scarecrows, map[string]any{
				"type":     name,
				"position": map[string]any{"x": obj.X, "y": obj.Y},
			})
		case machineNames[name]:
			machineCounts[name]++
		}
	}

	machines := make([]map[string]any, 0, len(machineCounts))
	for name, count := range machineCounts {
		machines = append(machines, map[string]any{"name": name, "count": count})
	}
	// Sort machines by count descending, then name for deterministic output
	for i := 0; i < len(machines); i++ {
		for j := i + 1; j < len(machines); j++ {
			ci := machines[i]["count"].(int)
			cj := machines[j]["count"].(int)
			if cj > ci || (cj == ci && machines[j]["name"].(string) < machines[i]["name"].(string)) {
				machines[i], machines[j] = machines[j], machines[i]
			}
		}
	}
	return sprinklers, scarecrows, machines
}

func buildSprinklerZones(sprinklers []sprinklerInfo, farm *GameLocation) []map[string]any {
	if len(sprinklers) == 0 {
		return make([]map[string]any, 0)
	}

	// Build crop map: position -> crop name
	type cropTile struct {
		x, y int
		name string
	}
	var cropTiles []cropTile
	for _, tf := range farm.TerrainFeatures {
		if tf.Feature.Type != "HoeDirt" || tf.Feature.Crop.IndexOfHarvest <= 0 || tf.Feature.Crop.Dead {
			continue
		}
		cropTiles = append(cropTiles, cropTile{
			x: tf.X, y: tf.Y,
			name: itemName(tf.Feature.Crop.IndexOfHarvest),
		})
	}

	// Assign each crop to nearest sprinkler
	type zoneData struct {
		sprinkler sprinklerInfo
		crops     map[string]int
	}
	zones := make([]zoneData, len(sprinklers))
	for i, s := range sprinklers {
		zones[i] = zoneData{sprinkler: s, crops: map[string]int{}}
	}

	for _, ct := range cropTiles {
		bestIdx := 0
		bestDist := distSq(ct.x, ct.y, sprinklers[0].x, sprinklers[0].y)
		for i := 1; i < len(sprinklers); i++ {
			d := distSq(ct.x, ct.y, sprinklers[i].x, sprinklers[i].y)
			if d < bestDist {
				bestDist = d
				bestIdx = i
			}
		}
		zones[bestIdx].crops[ct.name]++
	}

	// Sort by total crops descending, cap at 100
	for i := range len(zones) {
		for j := i + 1; j < len(zones); j++ {
			if zoneCropTotal(zones[j]) > zoneCropTotal(zones[i]) {
				zones[i], zones[j] = zones[j], zones[i]
			}
		}
	}
	cap := min(100, len(zones))

	result := make([]map[string]any, 0, cap)
	for i := range cap {
		z := zones[i]
		cropList := make([]map[string]any, 0, len(z.crops))
		for name, count := range z.crops {
			cropList = append(cropList, map[string]any{"name": name, "count": count})
		}
		// Sort zone crops by count descending, then name for deterministic output
		for ci := 0; ci < len(cropList); ci++ {
			for cj := ci + 1; cj < len(cropList); cj++ {
				a := cropList[ci]["count"].(int)
				b := cropList[cj]["count"].(int)
				if b > a || (b == a && cropList[cj]["name"].(string) < cropList[ci]["name"].(string)) {
					cropList[ci], cropList[cj] = cropList[cj], cropList[ci]
				}
			}
		}
		result = append(result, map[string]any{
			"sprinkler":  z.sprinkler.name,
			"position":   map[string]any{"x": z.sprinkler.x, "y": z.sprinkler.y},
			"radius":     sprinklerRadius(z.sprinkler.name),
			"crops":      cropList,
			"totalCrops": zoneCropTotal(zones[i]),
		})
	}
	return result
}

func distSq(x1, y1, x2, y2 int) int {
	dx := x1 - x2
	dy := y1 - y2
	return dx*dx + dy*dy
}

func zoneCropTotal(z struct {
	sprinkler sprinklerInfo
	crops     map[string]int
}) int {
	total := 0
	for _, c := range z.crops {
		total += c
	}
	return total
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
	return strings.ToUpper(s[:1]) + s[1:]
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
