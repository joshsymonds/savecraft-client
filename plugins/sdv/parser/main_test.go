package main

import (
	"encoding/json"
	"os"
	"testing"
)

func TestParseTestSave(t *testing.T) {
	data, err := os.ReadFile("../testdata/TestSave")
	if err != nil {
		t.Fatalf("reading test fixture: %v", err)
	}

	save, err := parseSave(data)
	if err != nil {
		t.Fatalf("parseSave: %v", err)
	}

	// Basic identity
	if save.Player.Name != "Test" {
		t.Errorf("player name = %q, want %q", save.Player.Name, "Test")
	}
	if save.Player.FarmName != "Test" {
		t.Errorf("farm name = %q, want %q", save.Player.FarmName, "Test")
	}
	if save.GameVersion != "1.6.8" {
		t.Errorf("game version = %q, want %q", save.GameVersion, "1.6.8")
	}

	// Date
	if save.CurrentSeason != "spring" {
		t.Errorf("season = %q, want %q", save.CurrentSeason, "spring")
	}
	if save.Player.DayOfMonth != 6 {
		t.Errorf("day = %d, want %d", save.Player.DayOfMonth, 6)
	}
	if save.Player.Year != 1 {
		t.Errorf("year = %d, want %d", save.Player.Year, 1)
	}

	// Farm type (3 = Hill-top)
	if save.WhichFarm != 3 {
		t.Errorf("farm type = %d, want %d", save.WhichFarm, 3)
	}

	// Money
	if save.Player.Money != 1285 {
		t.Errorf("money = %d, want %d", save.Player.Money, 1285)
	}
	if save.Player.TotalMoneyEarned != 1125 {
		t.Errorf("total money earned = %d, want %d", save.Player.TotalMoneyEarned, 1125)
	}

	// Experience points (6 skills)
	if len(save.Player.ExperiencePoints.Values) != 6 {
		t.Fatalf("experience points count = %d, want 6", len(save.Player.ExperiencePoints.Values))
	}
	// Farming XP = 120
	if save.Player.ExperiencePoints.Values[0] != 120 {
		t.Errorf("farming XP = %d, want %d", save.Player.ExperiencePoints.Values[0], 120)
	}

	// Summary
	summary := buildSummary(save)
	expected := "Test, Year 1 Spring 6, Test Farm (Hill-top)"
	if summary != expected {
		t.Errorf("summary = %q, want %q", summary, expected)
	}

	// Sections produce valid JSON
	sections := buildSections(save)
	sectionJSON, err := json.Marshal(sections)
	if err != nil {
		t.Fatalf("marshaling sections: %v", err)
	}
	if len(sectionJSON) == 0 {
		t.Error("sections JSON is empty")
	}

	// Character section data
	charSection := sections["character"].(map[string]any)
	charData := charSection["data"].(map[string]any)
	if charData["name"] != "Test" {
		t.Errorf("character section name = %v, want %q", charData["name"], "Test")
	}
	if charData["farmType"] != "Hill-top" {
		t.Errorf("character section farmType = %v, want %q", charData["farmType"], "Hill-top")
	}

	// Skills in character section
	skills := charData["skills"].([]map[string]any)
	if len(skills) != 6 {
		t.Fatalf("skills count = %d, want 6", len(skills))
	}
	if skills[0]["name"] != "Farming" {
		t.Errorf("first skill name = %v, want %q", skills[0]["name"], "Farming")
	}
	if skills[0]["level"] != 1 {
		t.Errorf("farming level = %v, want 1", skills[0]["level"])
	}
}

func TestCharacterSectionFields(t *testing.T) {
	data, err := os.ReadFile("../testdata/TestSave")
	if err != nil {
		t.Fatalf("reading test fixture: %v", err)
	}

	save, err := parseSave(data)
	if err != nil {
		t.Fatalf("parseSave: %v", err)
	}

	sections := buildSections(save)
	charSection := sections["character"].(map[string]any)
	charData := charSection["data"].(map[string]any)

	// Pet type
	if charData["petType"] != "Cat" {
		t.Errorf("petType = %v, want %q", charData["petType"], "Cat")
	}
	if charData["petBreed"] != 4 {
		t.Errorf("petBreed = %v, want 4", charData["petBreed"])
	}

	// Professions (empty in early-game save, but field must exist)
	profs, ok := charData["professions"].([]string)
	if !ok {
		t.Fatalf("professions type = %T, want []string", charData["professions"])
	}
	if len(profs) != 0 {
		t.Errorf("professions count = %d, want 0 (early game)", len(profs))
	}

	// Mastery (0 in early-game save)
	if charData["masteryXP"] != 0 {
		t.Errorf("masteryXP = %v, want 0", charData["masteryXP"])
	}

	// Favorite thing
	if charData["favoriteThing"] != "Unit Tests" {
		t.Errorf("favoriteThing = %v, want %q", charData["favoriteThing"], "Unit Tests")
	}

	// Gender
	if charData["gender"] != "Male" {
		t.Errorf("gender = %v, want %q", charData["gender"], "Male")
	}
}

func TestSocialSection(t *testing.T) {
	data, err := os.ReadFile("../testdata/TestSave")
	if err != nil {
		t.Fatalf("reading test fixture: %v", err)
	}

	save, err := parseSave(data)
	if err != nil {
		t.Fatalf("parseSave: %v", err)
	}

	sections := buildSections(save)
	socialSection, ok := sections["social"].(map[string]any)
	if !ok {
		t.Fatal("social section missing")
	}
	socialData := socialSection["data"].(map[string]any)

	// Relationships array
	relationships, ok := socialData["relationships"].([]map[string]any)
	if !ok {
		t.Fatalf("relationships type = %T, want []map[string]any", socialData["relationships"])
	}
	if len(relationships) == 0 {
		t.Fatal("relationships is empty")
	}

	// Find Lewis (should be in TestSave)
	var lewis map[string]any
	for _, r := range relationships {
		if r["name"] == "Lewis" {
			lewis = r
			break
		}
	}
	if lewis == nil {
		t.Fatal("Lewis not found in relationships")
	}
	if lewis["status"] != "Friendly" {
		t.Errorf("Lewis status = %v, want %q", lewis["status"], "Friendly")
	}
	if lewis["friendshipPoints"].(int) < 0 {
		t.Errorf("Lewis friendshipPoints = %v, should be >= 0", lewis["friendshipPoints"])
	}
	heartLevel := lewis["heartLevel"].(int)
	if heartLevel < 0 || heartLevel > 14 {
		t.Errorf("Lewis heartLevel = %d, should be 0-14", heartLevel)
	}

	// Spouse should be empty in both test saves
	if socialData["spouse"] != "" {
		t.Errorf("spouse = %v, want empty string", socialData["spouse"])
	}

	// Children should be empty array
	children, ok := socialData["children"].([]map[string]any)
	if !ok {
		t.Fatalf("children type = %T, want []map[string]any", socialData["children"])
	}
	if len(children) != 0 {
		t.Errorf("children count = %d, want 0", len(children))
	}
}

func TestSocialSectionPerfectionSave(t *testing.T) {
	data, err := os.ReadFile("../testdata/PerfectionSave")
	if err != nil {
		t.Fatalf("reading test fixture: %v", err)
	}

	save, err := parseSave(data)
	if err != nil {
		t.Fatalf("parseSave: %v", err)
	}

	sections := buildSections(save)
	socialData := sections["social"].(map[string]any)["data"].(map[string]any)
	relationships := socialData["relationships"].([]map[string]any)

	// Perfection save has 35 NPCs
	if len(relationships) < 30 {
		t.Errorf("relationships count = %d, want >= 30", len(relationships))
	}

	// All should have Points >= 2000 (perfection requirement is 8+ hearts with everyone)
	for _, r := range relationships {
		pts := r["friendshipPoints"].(int)
		if pts < 2000 {
			t.Errorf("%s friendshipPoints = %d, want >= 2000 for perfection", r["name"], pts)
		}
	}
}

func TestProfessionName(t *testing.T) {
	tests := []struct {
		id   int
		want string
	}{
		{0, "Rancher"},
		{4, "Artisan"},
		{6, "Fisher"},
		{8, "Angler"},
		{24, "Fighter"},
		{29, "Desperado"},
		{99, "Profession#99"},
	}
	for _, tt := range tests {
		got := professionName(tt.id)
		if got != tt.want {
			t.Errorf("professionName(%d) = %q, want %q", tt.id, got, tt.want)
		}
	}
}

func TestSkillLevel(t *testing.T) {
	tests := []struct {
		xp   int
		want int
	}{
		{0, 0},
		{99, 0},
		{100, 1},
		{379, 1},
		{380, 2},
		{15000, 10},
		{20000, 10},
	}
	for _, tt := range tests {
		got := skillLevel(tt.xp)
		if got != tt.want {
			t.Errorf("skillLevel(%d) = %d, want %d", tt.xp, got, tt.want)
		}
	}
}

func TestInventorySection(t *testing.T) {
	data, err := os.ReadFile("../testdata/TestSave")
	if err != nil {
		t.Fatalf("reading test fixture: %v", err)
	}

	save, err := parseSave(data)
	if err != nil {
		t.Fatalf("parseSave: %v", err)
	}

	sections := buildSections(save)
	invSection, ok := sections["inventory"].(map[string]any)
	if !ok {
		t.Fatal("inventory section missing")
	}
	invData := invSection["data"].(map[string]any)

	// Tools
	tools, ok := invData["tools"].([]map[string]any)
	if !ok {
		t.Fatalf("tools type = %T, want []map[string]any", invData["tools"])
	}
	// TestSave has Pickaxe and Watering Can (both Basic)
	if len(tools) < 2 {
		t.Fatalf("tools count = %d, want >= 2", len(tools))
	}
	// Find Pickaxe
	var pickaxe map[string]any
	for _, tool := range tools {
		if tool["name"] == "Pickaxe" {
			pickaxe = tool
			break
		}
	}
	if pickaxe == nil {
		t.Fatal("Pickaxe not found in tools")
	}
	if pickaxe["upgradeLevel"] != "Basic" {
		t.Errorf("Pickaxe upgradeLevel = %v, want %q", pickaxe["upgradeLevel"], "Basic")
	}

	// Backpack items (non-tool, non-weapon items)
	items, ok := invData["items"].([]map[string]any)
	if !ok {
		t.Fatalf("items type = %T, want []map[string]any", invData["items"])
	}
	// TestSave has a silver Parsnip
	var parsnip map[string]any
	for _, item := range items {
		if item["name"] == "Parsnip" {
			parsnip = item
			break
		}
	}
	if parsnip == nil {
		t.Fatal("Parsnip not found in items")
	}
	if parsnip["quality"] != "Silver" {
		t.Errorf("Parsnip quality = %v, want %q", parsnip["quality"], "Silver")
	}

	// Weapons
	weapons, ok := invData["weapons"].([]map[string]any)
	if !ok {
		t.Fatalf("weapons type = %T, want []map[string]any", invData["weapons"])
	}
	if len(weapons) < 1 {
		t.Fatal("weapons is empty, want Rusty Sword")
	}
	if weapons[0]["name"] != "Rusty Sword" {
		t.Errorf("first weapon = %v, want %q", weapons[0]["name"], "Rusty Sword")
	}
}

func TestInventorySectionPerfection(t *testing.T) {
	data, err := os.ReadFile("../testdata/PerfectionSave")
	if err != nil {
		t.Fatalf("reading test fixture: %v", err)
	}

	save, err := parseSave(data)
	if err != nil {
		t.Fatalf("parseSave: %v", err)
	}

	sections := buildSections(save)
	invData := sections["inventory"].(map[string]any)["data"].(map[string]any)

	// All tools should be Iridium in perfection save
	tools := invData["tools"].([]map[string]any)
	if len(tools) < 4 {
		t.Fatalf("tools count = %d, want >= 4", len(tools))
	}
	for _, tool := range tools {
		name := tool["name"].(string)
		level := tool["upgradeLevel"].(string)
		// Scythe stays at Basic, all others should be Iridium
		if name == "Scythe" {
			if level != "Basic" {
				t.Errorf("Scythe upgradeLevel = %q, want %q", level, "Basic")
			}
		} else {
			if level != "Iridium" {
				t.Errorf("%s upgradeLevel = %q, want %q", name, level, "Iridium")
			}
		}
	}
}

func TestQualityName(t *testing.T) {
	tests := []struct {
		id   int
		want string
	}{
		{0, "Normal"},
		{1, "Silver"},
		{2, "Gold"},
		{4, "Iridium"},
		{99, "Normal"},
	}
	for _, tt := range tests {
		got := qualityName(tt.id)
		if got != tt.want {
			t.Errorf("qualityName(%d) = %q, want %q", tt.id, got, tt.want)
		}
	}
}

func TestUpgradeLevelName(t *testing.T) {
	tests := []struct {
		id   int
		want string
	}{
		{0, "Basic"},
		{1, "Copper"},
		{2, "Steel"},
		{3, "Gold"},
		{4, "Iridium"},
		{99, "Basic"},
	}
	for _, tt := range tests {
		got := upgradeLevelName(tt.id)
		if got != tt.want {
			t.Errorf("upgradeLevelName(%d) = %q, want %q", tt.id, got, tt.want)
		}
	}
}

func TestBundlesSection(t *testing.T) {
	data, err := os.ReadFile("../testdata/TestSave")
	if err != nil {
		t.Fatalf("reading test fixture: %v", err)
	}

	save, err := parseSave(data)
	if err != nil {
		t.Fatalf("parseSave: %v", err)
	}

	sections := buildSections(save)
	bundleSection, ok := sections["bundles"].(map[string]any)
	if !ok {
		t.Fatal("bundles section missing")
	}
	bundleData := bundleSection["data"].(map[string]any)

	// Route should be "Community Center" (not Joja)
	if bundleData["route"] != "Community Center" {
		t.Errorf("route = %v, want %q", bundleData["route"], "Community Center")
	}

	// Should not be complete (early game)
	if bundleData["complete"] != false {
		t.Errorf("complete = %v, want false", bundleData["complete"])
	}

	// Rooms
	rooms, ok := bundleData["rooms"].([]map[string]any)
	if !ok {
		t.Fatalf("rooms type = %T, want []map[string]any", bundleData["rooms"])
	}
	if len(rooms) < 6 {
		t.Fatalf("rooms count = %d, want >= 6", len(rooms))
	}

	// Find Pantry
	var pantry map[string]any
	for _, r := range rooms {
		if r["name"] == "Pantry" {
			pantry = r
			break
		}
	}
	if pantry == nil {
		t.Fatal("Pantry not found in rooms")
	}
	if pantry["complete"] != false {
		t.Errorf("Pantry complete = %v, want false", pantry["complete"])
	}

	// Pantry should have bundles
	bundles, ok := pantry["bundles"].([]map[string]any)
	if !ok {
		t.Fatalf("bundles type = %T, want []map[string]any", pantry["bundles"])
	}
	if len(bundles) < 1 {
		t.Fatal("Pantry has no bundles")
	}

	// Spring Crops bundle should have items
	var springCrops map[string]any
	for _, b := range bundles {
		if b["name"] == "Spring Crops" {
			springCrops = b
			break
		}
	}
	if springCrops == nil {
		t.Fatal("Spring Crops bundle not found")
	}

	// Items should have names (not raw IDs)
	items, ok := springCrops["items"].([]map[string]any)
	if !ok {
		t.Fatalf("items type = %T, want []map[string]any", springCrops["items"])
	}
	if len(items) != 4 {
		t.Fatalf("Spring Crops items = %d, want 4", len(items))
	}
	// First item should be Parsnip
	if items[0]["name"] != "Parsnip" {
		t.Errorf("first item name = %v, want %q", items[0]["name"], "Parsnip")
	}
	if items[0]["completed"] != false {
		t.Errorf("first item completed = %v, want false", items[0]["completed"])
	}
}

func TestBundlesSectionPerfection(t *testing.T) {
	data, err := os.ReadFile("../testdata/PerfectionSave")
	if err != nil {
		t.Fatalf("reading test fixture: %v", err)
	}

	save, err := parseSave(data)
	if err != nil {
		t.Fatalf("parseSave: %v", err)
	}

	sections := buildSections(save)
	bundleData := sections["bundles"].(map[string]any)["data"].(map[string]any)

	// Should be complete
	if bundleData["complete"] != true {
		t.Errorf("complete = %v, want true", bundleData["complete"])
	}

	// All rooms should be complete
	rooms := bundleData["rooms"].([]map[string]any)
	for _, r := range rooms {
		if r["complete"] != true {
			t.Errorf("room %v complete = %v, want true", r["name"], r["complete"])
		}
	}
}

func TestCollectionsSection(t *testing.T) {
	data, err := os.ReadFile("../testdata/TestSave")
	if err != nil {
		t.Fatalf("reading test fixture: %v", err)
	}

	save, err := parseSave(data)
	if err != nil {
		t.Fatalf("parseSave: %v", err)
	}

	sections := buildSections(save)
	colSection, ok := sections["collections"].(map[string]any)
	if !ok {
		t.Fatal("collections section missing")
	}
	colData := colSection["data"].(map[string]any)

	// Fish sub-collection
	fish, ok := colData["fish"].(map[string]any)
	if !ok {
		t.Fatal("fish sub-collection missing")
	}
	fishCaught := fish["caught"].([]map[string]any)
	if len(fishCaught) != 4 {
		t.Errorf("fish caught = %d, want 4", len(fishCaught))
	}
	// All should have string names (not raw IDs)
	for _, f := range fishCaught {
		name, ok := f["name"].(string)
		if !ok || name == "" {
			t.Errorf("fish entry missing name: %v", f)
		}
	}

	// Cooking
	cooking, ok := colData["cooking"].(map[string]any)
	if !ok {
		t.Fatal("cooking sub-collection missing")
	}
	recipesLearned := cooking["recipesLearned"].(int)
	if recipesLearned != 1 {
		t.Errorf("cooking recipesLearned = %d, want 1", recipesLearned)
	}

	// Crafting
	crafting, ok := colData["crafting"].(map[string]any)
	if !ok {
		t.Fatal("crafting sub-collection missing")
	}
	craftLearned := crafting["recipesLearned"].(int)
	if craftLearned != 14 {
		t.Errorf("crafting recipesLearned = %d, want 14", craftLearned)
	}

	// Shipping
	shipping, ok := colData["shipping"].(map[string]any)
	if !ok {
		t.Fatal("shipping sub-collection missing")
	}
	uniqueShipped := shipping["uniqueItemsShipped"].(int)
	if uniqueShipped != 6 {
		t.Errorf("uniqueItemsShipped = %d, want 6", uniqueShipped)
	}

	// Museum
	museum, ok := colData["museum"].(map[string]any)
	if !ok {
		t.Fatal("museum sub-collection missing")
	}
	// Early game - should have minerals found
	mineralsFound := museum["mineralsFound"].(int)
	if mineralsFound != 2 {
		t.Errorf("mineralsFound = %d, want 2", mineralsFound)
	}
}

func TestCollectionsSectionPerfection(t *testing.T) {
	data, err := os.ReadFile("../testdata/PerfectionSave")
	if err != nil {
		t.Fatalf("reading test fixture: %v", err)
	}

	save, err := parseSave(data)
	if err != nil {
		t.Fatalf("parseSave: %v", err)
	}

	sections := buildSections(save)
	colData := sections["collections"].(map[string]any)["data"].(map[string]any)

	// Fish: perfection save should have many species
	fish := colData["fish"].(map[string]any)
	fishCaught := fish["caught"].([]map[string]any)
	if len(fishCaught) < 60 {
		t.Errorf("fish caught species = %d, want >= 60", len(fishCaught))
	}

	// Cooking: should have many recipes
	cooking := colData["cooking"].(map[string]any)
	if cooking["recipesLearned"].(int) < 70 {
		t.Errorf("cooking recipesLearned = %d, want >= 70", cooking["recipesLearned"].(int))
	}
	if cooking["recipesCooked"].(int) < 70 {
		t.Errorf("cooking recipesCooked = %d, want >= 70", cooking["recipesCooked"].(int))
	}

	// Crafting
	crafting := colData["crafting"].(map[string]any)
	if crafting["recipesLearned"].(int) < 100 {
		t.Errorf("crafting recipesLearned = %d, want >= 100", crafting["recipesLearned"].(int))
	}

	// Shipping: many items
	shipping := colData["shipping"].(map[string]any)
	if shipping["uniqueItemsShipped"].(int) < 150 {
		t.Errorf("uniqueItemsShipped = %d, want >= 150", shipping["uniqueItemsShipped"].(int))
	}

	// Museum
	museum := colData["museum"].(map[string]any)
	if museum["mineralsFound"].(int) < 40 {
		t.Errorf("mineralsFound = %d, want >= 40", museum["mineralsFound"].(int))
	}
	if museum["artifactsFound"].(int) < 30 {
		t.Errorf("artifactsFound = %d, want >= 30", museum["artifactsFound"].(int))
	}
}

func TestProgressSection(t *testing.T) {
	data, err := os.ReadFile("../testdata/TestSave")
	if err != nil {
		t.Fatalf("reading test fixture: %v", err)
	}

	save, err := parseSave(data)
	if err != nil {
		t.Fatalf("parseSave: %v", err)
	}

	sections := buildSections(save)
	progSection, ok := sections["progress"].(map[string]any)
	if !ok {
		t.Fatal("progress section missing")
	}
	progData := progSection["data"].(map[string]any)

	// Stardrops: early game, none found
	stardrops := progData["stardrops"].(map[string]any)
	if stardrops["count"].(int) != 0 {
		t.Errorf("stardrops count = %v, want 0", stardrops["count"])
	}
	if stardrops["total"].(int) != 7 {
		t.Errorf("stardrops total = %v, want 7", stardrops["total"])
	}
	missing := stardrops["missing"].([]string)
	if len(missing) != 7 {
		t.Errorf("stardrops missing = %d, want 7", len(missing))
	}

	// Golden walnuts: early game, 0
	walnuts := progData["goldenWalnuts"].(map[string]any)
	if walnuts["found"].(int) != 0 {
		t.Errorf("golden walnuts found = %v, want 0", walnuts["found"])
	}

	// Quests: 1 (from stats)
	if progData["questsCompleted"].(int) != 1 {
		t.Errorf("questsCompleted = %v, want 1", progData["questsCompleted"])
	}

	// Secret notes: 0
	if progData["secretNotesSeen"].(int) != 0 {
		t.Errorf("secretNotesSeen = %v, want 0", progData["secretNotesSeen"])
	}

	// Special orders: 0
	if progData["specialOrdersCompleted"].(int) != 0 {
		t.Errorf("specialOrdersCompleted = %v, want 0", progData["specialOrdersCompleted"])
	}

	// Monster slayer goals
	monsterSlayer := progData["monsterSlayer"].(map[string]any)
	goals := monsterSlayer["goals"].([]map[string]any)
	if len(goals) != 12 {
		t.Errorf("monster slayer goals = %d, want 12", len(goals))
	}

	// Rock Crabs goal should have 1 kill (Rock Crab=1 in TestSave)
	var rockCrabs map[string]any
	for _, g := range goals {
		if g["category"] == "Rock Crabs" {
			rockCrabs = g
			break
		}
	}
	if rockCrabs == nil {
		t.Fatal("Rock Crabs goal not found")
	}
	if rockCrabs["killed"].(int) != 1 {
		t.Errorf("Rock Crabs killed = %v, want 1", rockCrabs["killed"])
	}
	if rockCrabs["target"].(int) != 60 {
		t.Errorf("Rock Crabs target = %v, want 60", rockCrabs["target"])
	}
	if rockCrabs["complete"].(bool) != false {
		t.Errorf("Rock Crabs complete = %v, want false", rockCrabs["complete"])
	}
}

func TestProgressSectionPerfection(t *testing.T) {
	data, err := os.ReadFile("../testdata/PerfectionSave")
	if err != nil {
		t.Fatalf("reading test fixture: %v", err)
	}

	save, err := parseSave(data)
	if err != nil {
		t.Fatalf("parseSave: %v", err)
	}

	sections := buildSections(save)
	progData := sections["progress"].(map[string]any)["data"].(map[string]any)

	// Stardrops: perfection save should have at least 6
	stardrops := progData["stardrops"].(map[string]any)
	count := stardrops["count"].(int)
	if count < 6 {
		t.Errorf("stardrops count = %d, want >= 6", count)
	}

	// Golden walnuts: 130 found
	walnuts := progData["goldenWalnuts"].(map[string]any)
	if walnuts["found"].(int) != 130 {
		t.Errorf("golden walnuts found = %v, want 130", walnuts["found"])
	}

	// Secret notes: 36
	if progData["secretNotesSeen"].(int) != 36 {
		t.Errorf("secretNotesSeen = %v, want 36", progData["secretNotesSeen"])
	}

	// Quests: 48
	if progData["questsCompleted"].(int) != 48 {
		t.Errorf("questsCompleted = %v, want 48", progData["questsCompleted"])
	}

	// Special orders: 25
	if progData["specialOrdersCompleted"].(int) != 25 {
		t.Errorf("specialOrdersCompleted = %v, want 25", progData["specialOrdersCompleted"])
	}

	// Monster slayer: all goals should be complete in perfection save
	monsterSlayer := progData["monsterSlayer"].(map[string]any)
	goals := monsterSlayer["goals"].([]map[string]any)
	for _, g := range goals {
		killed := g["killed"].(int)
		target := g["target"].(int)
		if killed < target {
			t.Errorf("monster goal %v: killed=%d < target=%d", g["category"], killed, target)
		}
	}
}

func TestPerfectionSection(t *testing.T) {
	data, err := os.ReadFile("../testdata/TestSave")
	if err != nil {
		t.Fatalf("reading test fixture: %v", err)
	}

	save, err := parseSave(data)
	if err != nil {
		t.Fatalf("parseSave: %v", err)
	}

	sections := buildSections(save)
	perfSection, ok := sections["perfection"].(map[string]any)
	if !ok {
		t.Fatal("perfection section missing")
	}
	perfData := perfSection["data"].(map[string]any)

	// Overall percentage should be very low for early game
	pct := perfData["percentage"].(float64)
	if pct < 0 || pct > 10 {
		t.Errorf("perfection percentage = %.1f, want 0-10 for early game", pct)
	}

	// Categories array
	categories := perfData["categories"].([]map[string]any)
	if len(categories) != 11 {
		t.Errorf("perfection categories = %d, want 11", len(categories))
	}

	// All categories should have name and weight
	for _, cat := range categories {
		if _, ok := cat["name"].(string); !ok {
			t.Errorf("category missing name: %v", cat)
		}
		if _, ok := cat["weight"].(int); !ok {
			t.Errorf("category %v missing weight", cat["name"])
		}
	}

	// Obelisks: 0 in early game
	var obelisks map[string]any
	for _, cat := range categories {
		if cat["name"] == "Obelisks" {
			obelisks = cat
			break
		}
	}
	if obelisks == nil {
		t.Fatal("Obelisks category not found")
	}
	if obelisks["current"].(int) != 0 {
		t.Errorf("obelisks current = %v, want 0", obelisks["current"])
	}

	// Gold Clock: false in early game
	var goldClock map[string]any
	for _, cat := range categories {
		if cat["name"] == "Gold Clock" {
			goldClock = cat
			break
		}
	}
	if goldClock == nil {
		t.Fatal("Gold Clock category not found")
	}
	if goldClock["complete"].(bool) != false {
		t.Errorf("gold clock complete = %v, want false", goldClock["complete"])
	}
}

func TestPerfectionSectionPerfection(t *testing.T) {
	data, err := os.ReadFile("../testdata/PerfectionSave")
	if err != nil {
		t.Fatalf("reading test fixture: %v", err)
	}

	save, err := parseSave(data)
	if err != nil {
		t.Fatalf("parseSave: %v", err)
	}

	sections := buildSections(save)
	perfData := sections["perfection"].(map[string]any)["data"].(map[string]any)

	// Overall percentage should be very high for perfection save
	pct := perfData["percentage"].(float64)
	if pct < 90 {
		t.Errorf("perfection percentage = %.1f, want >= 90", pct)
	}

	categories := perfData["categories"].([]map[string]any)

	// Check key perfection categories
	catMap := map[string]map[string]any{}
	for _, cat := range categories {
		catMap[cat["name"].(string)] = cat
	}

	// Obelisks: 4/4
	if catMap["Obelisks"]["current"].(int) != 4 {
		t.Errorf("obelisks current = %v, want 4", catMap["Obelisks"]["current"])
	}

	// Gold Clock: true
	if catMap["Gold Clock"]["complete"].(bool) != true {
		t.Errorf("gold clock complete = %v, want true", catMap["Gold Clock"]["complete"])
	}

	// Monster Slayer: true
	if catMap["Monster Slayer Hero"]["complete"].(bool) != true {
		t.Errorf("monster slayer complete = %v, want true", catMap["Monster Slayer Hero"]["complete"])
	}

	// Stardrops: true (7/7)
	if catMap["Stardrops"]["complete"].(bool) != true {
		t.Errorf("stardrops complete = %v, want true", catMap["Stardrops"]["complete"])
	}

	// Golden Walnuts: 130/130
	if catMap["Golden Walnuts"]["current"].(int) != 130 {
		t.Errorf("golden walnuts current = %v, want 130", catMap["Golden Walnuts"]["current"])
	}

	// Farmer Level: 25/25 (all skills maxed)
	if catMap["Farmer Level"]["current"].(int) != 25 {
		t.Errorf("farmer level current = %v, want 25", catMap["Farmer Level"]["current"])
	}
}

func TestFarmTypeName(t *testing.T) {
	tests := []struct {
		id   int
		want string
	}{
		{0, "Standard"},
		{3, "Hill-top"},
		{6, "Beach"},
		{7, "Meadowlands"},
		{99, "Unknown(99)"},
	}
	for _, tt := range tests {
		got := farmTypeName(tt.id)
		if got != tt.want {
			t.Errorf("farmTypeName(%d) = %q, want %q", tt.id, got, tt.want)
		}
	}
}
