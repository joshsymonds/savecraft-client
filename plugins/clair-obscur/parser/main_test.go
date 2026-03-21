package main

import (
	"os"
	"strings"
	"testing"

	"github.com/joshsymonds/savecraft.gg/plugins/gvas"
)

func loadTestSave(t *testing.T) *gvas.Save {
	t.Helper()
	data, err := os.ReadFile("../testdata/EXPEDITION_0.sav")
	if err != nil {
		t.Fatalf("read test file: %v", err)
	}
	save, err := gvas.ParseBytes(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return save
}

func TestBuildAllSections(t *testing.T) {
	save := loadTestSave(t)
	sections := buildAllSections(save)

	requiredSections := []string{
		"overview", "party", "inventory", "progression", "weapons",
		"character:Lune", "character:Maelle", "character:Sciel",
		"character:Monoco", "character:Verso",
	}
	for _, name := range requiredSections {
		if _, ok := sections[name]; !ok {
			t.Errorf("missing section %q", name)
		}
	}

	// Verify no extra unexpected top-level keys (all should be known).
	for key := range sections {
		found := false
		for _, req := range requiredSections {
			if key == req {
				found = true
				break
			}
		}
		if !found {
			// Unknown section is not necessarily wrong, but log it.
			t.Logf("additional section found: %q", key)
		}
	}
}

func TestOverviewSection(t *testing.T) {
	save := loadTestSave(t)
	sections := buildAllSections(save)
	overview, ok := sections["overview"].(map[string]any)
	if !ok {
		t.Fatal("overview section missing or wrong type")
	}
	data, ok := overview["data"].(map[string]any)
	if !ok {
		t.Fatal("overview data missing or wrong type")
	}

	tests := []struct {
		name string
		got  any
		want any
	}{
		{"gold", data["gold"], int32(1225269)},
		{"ng_plus_cycle", data["ng_plus_cycle"], int32(1)},
		{"current_map", data["current_map"], "Level_Camp_Main"},
		{"difficulty", data["difficulty"], "Normal"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("got %v (%T), want %v (%T)", tt.got, tt.got, tt.want, tt.want)
			}
		})
	}

	// Playtime hours should be approximately 57.1.
	hours, ok := data["playtime_hours"].(float64)
	if !ok {
		t.Fatalf("playtime_hours is %T, want float64", data["playtime_hours"])
	}
	if hours < 50 || hours > 65 {
		t.Errorf("playtime_hours = %f, want ~57.1", hours)
	}

	// Characters list should have 5 entries.
	chars, ok := data["characters"].([]string)
	if !ok {
		t.Fatalf("characters is %T, want []string", data["characters"])
	}
	if len(chars) != 5 {
		t.Errorf("characters count = %d, want 5", len(chars))
	}
}

func TestCharacterSection(t *testing.T) {
	save := loadTestSave(t)
	sections := buildAllSections(save)

	luneSection, ok := sections["character:Lune"].(map[string]any)
	if !ok {
		t.Fatal("character:Lune section missing")
	}
	data, ok := luneSection["data"].(map[string]any)
	if !ok {
		t.Fatal("character:Lune data missing")
	}

	t.Run("level", func(t *testing.T) {
		level, ok := data["level"].(int32)
		if !ok {
			t.Fatalf("level is %T, want int32", data["level"])
		}
		if level != 80 {
			t.Errorf("level = %d, want 80", level)
		}
	})

	t.Run("attributes", func(t *testing.T) {
		attrs, ok := data["attributes"].(map[string]int32)
		if !ok {
			t.Fatalf("attributes is %T, want map[string]int32", data["attributes"])
		}
		// Lune has Vitality=42, Strength=99, Agility=99.
		wantAttrs := map[string]int32{
			"Vitality": 42,
			"Strength": 99,
			"Agility":  99,
		}
		for name, want := range wantAttrs {
			got, exists := attrs[name]
			if !exists {
				t.Errorf("attribute %q not found", name)
				continue
			}
			if got != want {
				t.Errorf("attribute %q = %d, want %d", name, got, want)
			}
		}
	})

	t.Run("equipped_skills", func(t *testing.T) {
		skills, ok := data["equipped_skills"].([]string)
		if !ok {
			t.Fatalf("equipped_skills is %T, want []string", data["equipped_skills"])
		}
		if len(skills) != 6 {
			t.Errorf("equipped_skills count = %d, want 6", len(skills))
		}
		// Check first skill.
		if len(skills) > 0 && skills[0] != "Wildfire" {
			t.Errorf("first equipped skill = %q, want Wildfire", skills[0])
		}
	})

	t.Run("equipment_weapon", func(t *testing.T) {
		equip, ok := data["equipment"].(map[string]any)
		if !ok {
			t.Fatalf("equipment is %T, want map[string]any", data["equipment"])
		}
		weapon, ok := equip["weapon"].(string)
		if !ok {
			t.Fatalf("weapon is %T, want string", equip["weapon"])
		}
		if weapon != "Reacherim_1" {
			t.Errorf("weapon = %q, want Reacherim_1", weapon)
		}
	})

	t.Run("equipment_pictos", func(t *testing.T) {
		equip := data["equipment"].(map[string]any)
		pictos, ok := equip["pictos"].([]string)
		if !ok {
			t.Fatalf("pictos is %T, want []string", equip["pictos"])
		}
		if len(pictos) != 3 {
			t.Errorf("pictos count = %d, want 3", len(pictos))
		}
	})
}

func TestPartySection(t *testing.T) {
	save := loadTestSave(t)
	sections := buildAllSections(save)

	partySection, ok := sections["party"].(map[string]any)
	if !ok {
		t.Fatal("party section missing")
	}
	data, ok := partySection["data"].(map[string]any)
	if !ok {
		t.Fatal("party data missing")
	}
	members, ok := data["members"].([]map[string]any)
	if !ok {
		t.Fatalf("members is %T, want []map[string]any", data["members"])
	}
	if len(members) != 3 {
		t.Fatalf("party size = %d, want 3", len(members))
	}

	// First member should be Verso (party leader).
	if members[0]["character"] != "Verso" {
		t.Errorf("party leader = %v, want Verso", members[0]["character"])
	}
	if members[0]["formation"] != "Default" {
		t.Errorf("party leader formation = %v, want Default", members[0]["formation"])
	}
}

func TestWeaponsSection(t *testing.T) {
	save := loadTestSave(t)
	sections := buildAllSections(save)

	weaponsSection, ok := sections["weapons"].(map[string]any)
	if !ok {
		t.Fatal("weapons section missing")
	}
	data, ok := weaponsSection["data"].(map[string]any)
	if !ok {
		t.Fatal("weapons data missing")
	}
	progressions, ok := data["progressions"].([]map[string]any)
	if !ok {
		t.Fatalf("progressions is %T, want []map[string]any", data["progressions"])
	}
	if len(progressions) <= 100 {
		t.Errorf("progressions count = %d, want > 100", len(progressions))
	}

	// Sorted descending by level, so first should have a high level.
	if len(progressions) > 0 {
		firstLevel, ok := progressions[0]["level"].(int32)
		if !ok {
			t.Fatalf("first progression level is %T, want int32", progressions[0]["level"])
		}
		if firstLevel < 20 {
			t.Errorf("highest progression level = %d, want >= 20", firstLevel)
		}
	}
}

func TestProgressionSection(t *testing.T) {
	save := loadTestSave(t)
	sections := buildAllSections(save)

	progSection, ok := sections["progression"].(map[string]any)
	if !ok {
		t.Fatal("progression section missing")
	}
	data, ok := progSection["data"].(map[string]any)
	if !ok {
		t.Fatal("progression data missing")
	}

	t.Run("quests", func(t *testing.T) {
		quests, ok := data["quests"].(map[string]any)
		if !ok {
			t.Fatalf("quests is %T, want map[string]any", data["quests"])
		}
		if len(quests) < 10 {
			t.Errorf("quest count = %d, want >= 10", len(quests))
		}
		// Main_GoldenPath should be InProgress.
		gp, ok := quests["Main_GoldenPath"].(map[string]any)
		if !ok {
			t.Fatal("Main_GoldenPath quest not found")
		}
		if gp["status"] != "InProgress" {
			t.Errorf("Main_GoldenPath status = %v, want InProgress", gp["status"])
		}
	})

	t.Run("exploration", func(t *testing.T) {
		exploration, ok := data["exploration"].(map[string]any)
		if !ok {
			t.Fatalf("exploration is %T, want map[string]any", data["exploration"])
		}
		caps, ok := exploration["exploration_capacities"].([]string)
		if !ok {
			t.Fatalf("exploration_capacities is %T, want []string", exploration["exploration_capacities"])
		}
		if len(caps) < 3 {
			t.Errorf("exploration_capacities count = %d, want >= 3", len(caps))
		}
	})

	t.Run("enemies", func(t *testing.T) {
		battled, ok := data["enemies_battled"].(int)
		if !ok {
			t.Fatalf("enemies_battled is %T, want int", data["enemies_battled"])
		}
		if battled < 50 {
			t.Errorf("enemies_battled = %d, want >= 50", battled)
		}
	})

	t.Run("visited_locations", func(t *testing.T) {
		locations, ok := data["visited_locations"].([]string)
		if !ok {
			t.Fatalf("visited_locations is %T, want []string", data["visited_locations"])
		}
		if len(locations) < 5 {
			t.Errorf("visited_locations count = %d, want >= 5", len(locations))
		}
	})
}

func TestInventorySection(t *testing.T) {
	save := loadTestSave(t)
	sections := buildAllSections(save)

	invSection, ok := sections["inventory"].(map[string]any)
	if !ok {
		t.Fatal("inventory section missing")
	}
	data, ok := invSection["data"].(map[string]any)
	if !ok {
		t.Fatal("inventory data missing")
	}

	gold, ok := data["gold"].(int32)
	if !ok {
		t.Fatalf("gold is %T, want int32", data["gold"])
	}
	if gold != 1225269 {
		t.Errorf("gold = %d, want 1225269", gold)
	}

	totalItems, ok := data["total_items"].(int)
	if !ok {
		t.Fatalf("total_items is %T, want int", data["total_items"])
	}
	if totalItems != 305 {
		t.Errorf("total_items = %d, want 305", totalItems)
	}
}

func TestBuildSummary(t *testing.T) {
	save := loadTestSave(t)
	summary := buildSummary(save)

	// Summary should contain party names, level range, NG+ cycle.
	if summary == "" {
		t.Fatal("summary is empty")
	}
	// Should contain "Verso" (party leader).
	if !strings.Contains(summary, "Verso") {
		t.Errorf("summary %q does not contain Verso", summary)
	}
	// Should contain "NG+" indicator.
	if !strings.Contains(summary, "NG+") {
		t.Errorf("summary %q does not contain NG+", summary)
	}
}

func TestBuildSaveName(t *testing.T) {
	save := loadTestSave(t)
	name := buildSaveName(save)
	if name != "Verso's Expedition" {
		t.Errorf("saveName = %q, want %q", name, "Verso's Expedition")
	}
}

func TestDisplayName(t *testing.T) {
	tests := []struct {
		internal string
		want     string
	}{
		{"Frey", "Gustave"},
		{"Lune", "Lune"},
		{"Maelle", "Maelle"},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.internal, func(t *testing.T) {
			got := displayName(tt.internal)
			if got != tt.want {
				t.Errorf("displayName(%q) = %q, want %q", tt.internal, got, tt.want)
			}
		})
	}
}

func TestEnumMapping(t *testing.T) {
	tests := []struct {
		input string
		table map[string]string
		want  string
	}{
		{"ECharacterAttribute::NewEnumerator0", attributeNames, "Vitality"},
		{"ECharacterAttribute::NewEnumerator1", attributeNames, "Strength"},
		{"ECharacterAttribute::NewEnumerator5", attributeNames, "Luck"},
		{"E_GameDifficulty::NewEnumerator1", difficultyNames, "Normal"},
		{"E_QuestStatus::NewEnumerator2", questStatusNames, "Completed"},
		{"E_jRPG_FormationType::NewEnumerator0", formationTypeNames, "Default"},
		// Unknown enum value should return original string.
		{"SomeEnum::NewEnumerator99", attributeNames, "SomeEnum::NewEnumerator99"},
		// No :: separator returns original.
		{"NotAnEnum", attributeNames, "NotAnEnum"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := mapEnum(tt.input, tt.table)
			if got != tt.want {
				t.Errorf("mapEnum(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
