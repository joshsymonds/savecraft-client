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
