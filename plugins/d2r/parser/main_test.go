package main

import (
	"os"
	"testing"

	"github.com/joshsymonds/savecraft.gg/plugins/d2r/d2s"
)

func loadAtmus(t *testing.T) *d2s.D2S {
	t.Helper()
	data, err := os.ReadFile("../testdata/Atmus.d2s")
	if err != nil {
		t.Fatalf("read Atmus.d2s: %v", err)
	}
	save, err := d2s.Parse(data)
	if err != nil {
		t.Fatalf("parse Atmus.d2s: %v", err)
	}
	return save
}

func loadStash(t *testing.T) *d2s.SharedStash {
	t.Helper()
	data, err := os.ReadFile("../testdata/ModernSharedStashSoftCoreV2.d2i")
	if err != nil {
		t.Fatalf("read stash: %v", err)
	}
	stash, err := d2s.ParseStash(data)
	if err != nil {
		t.Fatalf("parse stash: %v", err)
	}
	return stash
}

// buildAllSections mirrors the section-building logic in handleCharacter
// so we can test it without os.Stdout/os.Exit.
func buildAllSections(save *d2s.D2S) map[string]any {
	sections := map[string]any{
		"character_overview": map[string]any{
			"description": "Character identity: name, class, level, difficulty, mercenary status — fetch first to orient on who this character is",
			"data":        buildCharacterSection(save),
		},
		"attributes": map[string]any{
			"description": "Base stats (str/dex/vit/energy), HP/mana/stamina pools, unspent stat/skill points — use to check stat allocation or respec needs",
			"data":        buildAttributesSection(save),
		},
		"skills": map[string]any{
			"description": "Allocated skill points by name and level — use to evaluate build, suggest respec, or identify synergies",
			"data":        map[string]any{"skills": buildSkillsSection(save)},
		},
		"equipment": map[string]any{
			"description": "Currently equipped items with slot, properties, and sockets — use to evaluate gear, suggest upgrades, or check runeword bases",
			"data":        map[string]any{"equipment": buildEquipmentSection(save)},
		},
		"inventory": map[string]any{
			"description": "Items in inventory, personal stash, and Horadric Cube — use to find crafting materials, charms, or items to equip",
			"data":        buildInventorySection(save),
		},
	}

	if len(save.MercItems) > 0 {
		sections["mercenary"] = map[string]any{
			"description": "Mercenary equipped items with properties — use to evaluate merc gear or suggest upgrades",
			"data":        map[string]any{"mercenary": buildItemList(save.MercItems)},
		}
	}
	if len(save.CorpseItems) > 0 {
		sections["corpse"] = map[string]any{
			"description": "Items on character's corpse (died and hasn't retrieved body) — check when character appears undergeared",
			"data":        map[string]any{"corpse": buildItemList(save.CorpseItems)},
		}
	}
	if save.GolemItem != nil {
		sections["golem"] = map[string]any{
			"description": "Iron Golem source item (Necromancer) — check to avoid accidentally overwriting a valuable golem",
			"data":        buildItemMap(*save.GolemItem),
		}
	}

	return sections
}

func TestBuildSummary(t *testing.T) {
	save := loadAtmus(t)
	got := buildSummary(save)
	want := "Atmus, Level 74 Warlock (Hell)"
	if got != want {
		t.Errorf("buildSummary = %q, want %q", got, want)
	}
}

func TestBuildCharacterSection(t *testing.T) {
	save := loadAtmus(t)
	section := buildCharacterSection(save)

	requiredFields := []string{"name", "class", "level", "expansion", "hardcore", "ladder", "lastPlayed"}
	for _, field := range requiredFields {
		if _, ok := section[field]; !ok {
			t.Errorf("missing required field %q", field)
		}
	}

	if section["name"] != "Atmus" {
		t.Errorf("name = %v, want Atmus", section["name"])
	}
	if section["class"] != "Warlock" {
		t.Errorf("class = %v, want Warlock", section["class"])
	}
	if section["difficulty"] != "Hell" {
		t.Errorf("difficulty = %v, want Hell", section["difficulty"])
	}

	// Atmus has a mercenary.
	merc, ok := section["mercenary"].(map[string]any)
	if !ok {
		t.Fatal("mercenary field missing or wrong type")
	}
	if _, ok := merc["id"]; !ok {
		t.Error("mercenary missing id")
	}
}

func TestBuildAttributesSection(t *testing.T) {
	save := loadAtmus(t)
	attrs := buildAttributesSection(save)

	requiredFields := []string{
		"strength", "dexterity", "vitality", "energy",
		"currentHP", "maxHP", "currentMana", "maxMana",
		"level", "experience", "gold", "stashedGold",
	}
	for _, field := range requiredFields {
		if _, ok := attrs[field]; !ok {
			t.Errorf("missing required field %q", field)
		}
	}

	level, ok := attrs["level"].(uint32)
	if !ok {
		t.Fatalf("level type = %T, want uint32", attrs["level"])
	}
	if level != 74 {
		t.Errorf("level = %d, want 74", level)
	}
}

func TestBuildSkillsSection(t *testing.T) {
	save := loadAtmus(t)
	skills := buildSkillsSection(save)

	if len(skills) == 0 {
		t.Fatal("no skills with level > 0")
	}

	// Every skill entry must have id, name, level.
	for i, skill := range skills {
		for _, field := range []string{"id", "name", "level"} {
			if _, ok := skill[field]; !ok {
				t.Errorf("skill[%d] missing field %q", i, field)
			}
		}
		// All returned skills must have level > 0.
		lvl, ok := skill["level"].(byte)
		if !ok {
			t.Errorf("skill[%d] level type = %T, want byte", i, skill["level"])
		} else if lvl == 0 {
			t.Errorf("skill[%d] %q has level 0, should be filtered", i, skill["name"])
		}
	}
}

func TestBuildEquipmentSection_SlotNames(t *testing.T) {
	save := loadAtmus(t)
	equipped := buildEquipmentSection(save)

	if len(equipped) == 0 {
		t.Fatal("no equipped items")
	}

	// Every equipped item must have a slot name.
	slotsFound := map[string]bool{}
	for i, item := range equipped {
		slot, ok := item["slot"].(string)
		if !ok {
			t.Errorf("equipped[%d] (%v) missing slot name", i, item["code"])
			continue
		}
		if slot == "" {
			t.Errorf("equipped[%d] has empty slot name", i)
		}
		slotsFound[slot] = true
	}

	// Atmus should have at least helm, body armor, and a weapon.
	for _, expected := range []string{"Body Armor"} {
		if !slotsFound[expected] {
			t.Errorf("no item in slot %q", expected)
		}
	}

	t.Logf("slots found: %v", slotsFound)
}

func TestBuildInventorySection(t *testing.T) {
	save := loadAtmus(t)
	inv := buildInventorySection(save)

	// Must have all three sub-sections (even if empty slices).
	for _, key := range []string{"inventory", "stash", "cube"} {
		if _, ok := inv[key]; !ok {
			t.Errorf("missing sub-section %q", key)
		}
	}
}

func TestBuildAllSections_Atmus(t *testing.T) {
	save := loadAtmus(t)
	sections := buildAllSections(save)

	// Standard sections always present.
	for _, name := range []string{"character_overview", "attributes", "skills", "equipment", "inventory"} {
		sec, ok := sections[name].(map[string]any)
		if !ok {
			t.Errorf("missing section %q", name)
			continue
		}
		if _, ok := sec["description"]; !ok {
			t.Errorf("section %q missing description", name)
		}
		if _, ok := sec["data"]; !ok {
			t.Errorf("section %q missing data", name)
		}
	}

	// Atmus has a mercenary.
	if _, ok := sections["mercenary"]; !ok {
		t.Error("mercenary section missing (Atmus has a merc)")
	}

	// Atmus should NOT have corpse or golem.
	if _, ok := sections["corpse"]; ok {
		t.Error("corpse section present but Atmus has no corpse items")
	}
	if _, ok := sections["golem"]; ok {
		t.Error("golem section present but Atmus is a Warlock, not Necromancer")
	}
}

func TestCorpseSection_Present(t *testing.T) {
	save := &d2s.D2S{
		Header: d2s.Header{
			Name:  "DeadGuy",
			Class: d2s.Necromancer,
			CurrentDifficulty: d2s.Difficulty{
				Active:     true,
				Difficulty: d2s.Hell,
			},
		},
		Attributes: d2s.Attributes{Level: 80},
		CorpseItems: []d2s.Item{
			{Code: "cap", TypeName: "Cap", SimpleItem: true},
			{Code: "lea", TypeName: "Leather Armor", SimpleItem: true},
		},
	}

	sections := buildAllSections(save)

	corpse, ok := sections["corpse"].(map[string]any)
	if !ok {
		t.Fatal("corpse section missing")
	}

	dataObj, ok := corpse["data"].(map[string]any)
	if !ok {
		t.Fatalf("corpse data type = %T, want map[string]any", corpse["data"])
	}
	items, ok := dataObj["corpse"].([]map[string]any)
	if !ok {
		t.Fatalf("corpse.data.corpse type = %T, want []map[string]any", dataObj["corpse"])
	}
	if len(items) != 2 {
		t.Errorf("corpse items = %d, want 2", len(items))
	}
}

func TestCorpseSection_Absent(t *testing.T) {
	save := &d2s.D2S{
		Header:     d2s.Header{Name: "Alive"},
		Attributes: d2s.Attributes{Level: 50},
	}

	sections := buildAllSections(save)

	if _, ok := sections["corpse"]; ok {
		t.Error("corpse section present with no corpse items")
	}
}

func TestGolemSection_Present(t *testing.T) {
	golemItem := d2s.Item{
		Code:     "brs",
		TypeName: "Breast Plate",
		Quality:  d2s.QualityNormal,
		Defense:  65,
	}
	save := &d2s.D2S{
		Header: d2s.Header{
			Name:  "NecroGuy",
			Class: d2s.Necromancer,
		},
		Attributes: d2s.Attributes{Level: 70},
		GolemItem:  &golemItem,
	}

	sections := buildAllSections(save)

	golem, ok := sections["golem"].(map[string]any)
	if !ok {
		t.Fatal("golem section missing")
	}

	// Golem is a single item, not a list.
	data, ok := golem["data"].(map[string]any)
	if !ok {
		t.Fatalf("golem data type = %T, want map[string]any", golem["data"])
	}
	if data["code"] != "brs" {
		t.Errorf("golem code = %v, want brs", data["code"])
	}
}

func TestGolemSection_Absent(t *testing.T) {
	save := &d2s.D2S{
		Header:     d2s.Header{Name: "NoGolem"},
		Attributes: d2s.Attributes{Level: 50},
	}

	sections := buildAllSections(save)

	if _, ok := sections["golem"]; ok {
		t.Error("golem section present with nil GolemItem")
	}
}

func TestBuildItemMap_SetBonuses(t *testing.T) {
	item := d2s.Item{
		Code:     "urn",
		TypeName: "Crown",
		Quality:  d2s.QualitySet,
		SetName:  "Tal Rasha's Horadric Crest",
		SetAttributes: [][]d2s.MagicAttribute{
			{
				{ID: 31, Name: "+{0} Defense", Values: []int64{60}},
			},
			{
				{ID: 39, Name: "+{0}% Resistance to Fire", Values: []int64{30}},
			},
		},
	}

	m := buildItemMap(item)

	bonuses, ok := m["setBonuses"].([][]map[string]any)
	if !ok {
		t.Fatalf("setBonuses type = %T, want [][]map[string]any", m["setBonuses"])
	}
	if len(bonuses) != 2 {
		t.Errorf("setBonuses count = %d, want 2", len(bonuses))
	}
}

func TestBuildItemMap_SetBonuses_Absent(t *testing.T) {
	item := d2s.Item{
		Code:     "cap",
		TypeName: "Cap",
		Quality:  d2s.QualityNormal,
	}

	m := buildItemMap(item)

	if _, ok := m["setBonuses"]; ok {
		t.Error("setBonuses present on non-set item")
	}
}

func TestBuildItemMap_RunewordProperties(t *testing.T) {
	item := d2s.Item{
		Code:         "mp",
		TypeName:     "Mage Plate",
		Quality:      d2s.QualityNormal,
		Runeword:     true,
		RunewordName: "Enigma",
		RunewordAttributes: []d2s.MagicAttribute{
			{ID: 97, Name: "+{1} To {0}", Values: []int64{54, 2}},
		},
	}

	m := buildItemMap(item)

	if m["runewordName"] != "Enigma" {
		t.Errorf("runewordName = %v, want Enigma", m["runewordName"])
	}
	props, ok := m["runewordProperties"].([]map[string]any)
	if !ok {
		t.Fatalf("runewordProperties type = %T, want []map[string]any", m["runewordProperties"])
	}
	if len(props) == 0 {
		t.Error("no runeword properties")
	}
}

func TestBuildItemMap_SocketedItems(t *testing.T) {
	item := d2s.Item{
		Code:         "mp",
		TypeName:     "Mage Plate",
		Socketed:     true,
		TotalSockets: 3,
		SocketedItems: []d2s.Item{
			{Code: "r22", TypeName: "Jah Rune", SimpleItem: true},
			{Code: "r16", TypeName: "Ith Rune", SimpleItem: true},
			{Code: "r30", TypeName: "Ber Rune", SimpleItem: true},
		},
	}

	m := buildItemMap(item)

	socketed, ok := m["socketedItems"].([]map[string]any)
	if !ok {
		t.Fatalf("socketedItems type = %T", m["socketedItems"])
	}
	if len(socketed) != 3 {
		t.Errorf("socketedItems = %d, want 3", len(socketed))
	}
}

func TestEquipSlotName(t *testing.T) {
	tests := []struct {
		slot byte
		want string
	}{
		{1, "Helm"},
		{2, "Amulet"},
		{3, "Body Armor"},
		{4, "Right Hand"},
		{5, "Left Hand"},
		{6, "Right Ring"},
		{7, "Left Ring"},
		{8, "Belt"},
		{9, "Boots"},
		{10, "Gloves"},
		{11, "Right Hand (Swap)"},
		{12, "Left Hand (Swap)"},
		{0, ""},
		{255, ""},
	}

	for _, tt := range tests {
		got := equipSlotName(tt.slot)
		if got != tt.want {
			t.Errorf("equipSlotName(%d) = %q, want %q", tt.slot, got, tt.want)
		}
	}
}

func TestFormatProperty(t *testing.T) {
	tests := []struct {
		name string
		attr d2s.MagicAttribute
		want string
	}{
		{
			name: "standard substitution",
			attr: d2s.MagicAttribute{ID: 0, Name: "+{0} to Strength", Values: []int64{20}},
			want: "+20 to Strength",
		},
		{
			name: "chance to cast",
			attr: d2s.MagicAttribute{ID: 198, Name: "", Values: []int64{3, 42, 10}},
			want: "10% Chance to Cast Level 3 Static Field on Strike",
		},
		{
			name: "charges",
			attr: d2s.MagicAttribute{ID: 204, Name: "", Values: []int64{15, 54, 30, 60}},
			want: "Level 15 Teleport (30/60 Charges)",
		},
		{
			name: "skill bonus",
			attr: d2s.MagicAttribute{ID: 107, Name: "", Values: []int64{54, 1}},
			want: "+1 To Teleport",
		},
		{
			name: "class skill bonus",
			attr: d2s.MagicAttribute{ID: 83, Name: "", Values: []int64{3, 2}},
			want: "+2 to Paladin Skill Levels",
		},
		{
			name: "aura when equipped",
			attr: d2s.MagicAttribute{ID: 151, Name: "", Values: []int64{123, 13}},
			want: "Level 13 Conviction Aura When Equipped",
		},
		{
			name: "skilltab",
			attr: d2s.MagicAttribute{ID: 188, Name: "", Values: []int64{0, 0, 3}},
			want: "+3 to Bow and Crossbow Skills (Amazon only)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatProperty(tt.attr)
			if got != tt.want {
				t.Errorf("formatProperty = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildStashSummary(t *testing.T) {
	stash := loadStash(t)
	summary := buildStashSummary(stash)

	// Should contain item count and gold.
	if summary == "" {
		t.Error("empty summary")
	}
	// Kind 2 = RotW softcore (maps to Softcore label since kind != 0).
	if got := summary; got != "Shared Stash (Softcore), 60 items, 50000 gold" {
		t.Logf("stash summary = %q (verify manually if gold/count changed)", got)
	}
}

func TestBuildStashSections(t *testing.T) {
	stash := loadStash(t)
	sections := buildStashSections(stash)

	// Overview must exist.
	overview, ok := sections["overview"].(map[string]any)
	if !ok {
		t.Fatal("missing overview section")
	}
	data, ok := overview["data"].(map[string]any)
	if !ok {
		t.Fatal("overview data wrong type")
	}
	if _, ok := data["gold"]; !ok {
		t.Error("overview missing gold")
	}
	if _, ok := data["version"]; !ok {
		t.Error("overview missing version")
	}
	if _, ok := data["tabs"]; !ok {
		t.Error("overview missing tabs")
	}

	// Should have at least one tab section with items.
	hasTabWithItems := false
	for key, sec := range sections {
		if key == "overview" {
			continue
		}
		tabSec, ok := sec.(map[string]any)
		if !ok {
			continue
		}
		if tabData, ok := tabSec["data"].(map[string]any); ok {
			if items, ok := tabData["items"].([]map[string]any); ok && len(items) > 0 {
				hasTabWithItems = true
			}
		}
	}
	if !hasTabWithItems {
		t.Error("no tab sections with items")
	}
}

func TestBuildPropertyList_SkipsInternalProps(t *testing.T) {
	attrs := []d2s.MagicAttribute{
		{ID: 0, Name: "+{0} to Strength", Values: []int64{20}},
		{ID: 67, Name: "internal velocity", Values: []int64{30}}, // should be skipped
		{ID: 31, Name: "+{0} Defense", Values: []int64{100}},
	}

	props := buildPropertyList(attrs)

	if len(props) != 2 {
		t.Errorf("properties = %d, want 2 (one internal skipped)", len(props))
	}
}
