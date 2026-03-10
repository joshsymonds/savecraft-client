package d2s

import (
	"os"
	"testing"
)

func TestFindBreakpoint(t *testing.T) {
	table := []int{0, 9, 18, 30, 48, 75, 125}

	tests := []struct {
		name     string
		total    int
		wantCur  int
		wantNext *int
	}{
		{"zero", 0, 0, intPtr(9)},
		{"exact hit", 18, 18, intPtr(30)},
		{"between breakpoints", 25, 18, intPtr(30)},
		{"at max", 125, 125, nil},
		{"above max", 200, 125, nil},
		{"just below", 8, 0, intPtr(9)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bp := FindBreakpoint(table, tt.total)
			if bp.Total != tt.total {
				t.Errorf("Total = %d, want %d", bp.Total, tt.total)
			}
			if bp.Current != tt.wantCur {
				t.Errorf("Current = %d, want %d", bp.Current, tt.wantCur)
			}
			if tt.wantNext == nil {
				if bp.NextBreakpoint != nil {
					t.Errorf("NextBreakpoint = %d, want nil", *bp.NextBreakpoint)
				}
			} else {
				if bp.NextBreakpoint == nil {
					t.Errorf("NextBreakpoint = nil, want %d", *tt.wantNext)
				} else if *bp.NextBreakpoint != *tt.wantNext {
					t.Errorf("NextBreakpoint = %d, want %d", *bp.NextBreakpoint, *tt.wantNext)
				}
			}
		})
	}
}

func TestPerLevelValue(t *testing.T) {
	// floor(74 * 8 / 128) = floor(4.625) = 4
	if got := perLevelValue(8, 74); got != 4 {
		t.Errorf("perLevelValue(8, 74) = %d, want 4", got)
	}
	// floor(99 * 8 / 128) = floor(6.1875) = 6
	if got := perLevelValue(8, 99); got != 6 {
		t.Errorf("perLevelValue(8, 99) = %d, want 6", got)
	}
	// floor(1 * 1 / 128) = 0
	if got := perLevelValue(1, 1); got != 0 {
		t.Errorf("perLevelValue(1, 1) = %d, want 0", got)
	}
}

func TestComputeStats_Handcrafted(t *testing.T) {
	save := &D2S{
		Header: Header{
			Class: Paladin,
		},
		Attributes: Attributes{Level: 80},
		Items: []Item{
			// Equipped helm with fire res and MF
			{
				Location: 0x01, EquipSlot: 1, Code: "cap",
				MagicAttributes: []MagicAttribute{
					{ID: statFireResist, Values: []int64{30}},
					{ID: statMagicFind, Values: []int64{25}},
				},
			},
			// Equipped body armor with all res and FCR
			{
				Location: 0x01, EquipSlot: 3, Code: "plt",
				MagicAttributes: []MagicAttribute{
					{ID: statFireResist, Values: []int64{20}},
					{ID: statColdResist, Values: []int64{20}},
					{ID: statLightningResist, Values: []int64{20}},
					{ID: statPoisonResist, Values: []int64{20}},
					{ID: statFCR, Values: []int64{20}},
				},
			},
			// Equipped weapon (right hand) with life leech
			{
				Location: 0x01, EquipSlot: 4, Code: "hax",
				MagicAttributes: []MagicAttribute{
					{ID: statLifeLeech, Values: []int64{6}},
					{ID: statCrushingBlow, Values: []int64{33}},
				},
			},
			// Swap weapon — should be EXCLUDED
			{
				Location: 0x01, EquipSlot: 11, Code: "sst",
				MagicAttributes: []MagicAttribute{
					{ID: statFireResist, Values: []int64{50}},
					{ID: statMagicFind, Values: []int64{100}},
				},
			},
			// Inventory charm
			{
				Location: 0x00, Page: 1, Code: SmallCharm,
				MagicAttributes: []MagicAttribute{
					{ID: statColdResist, Values: []int64{5}},
					{ID: statMagicFind, Values: []int64{7}},
				},
			},
			// Inventory non-charm — should be EXCLUDED
			{
				Location: 0x00, Page: 1, Code: "hp1",
				MagicAttributes: []MagicAttribute{
					{ID: statFireResist, Values: []int64{99}},
				},
			},
			// Stash item — should be EXCLUDED
			{
				Location: 0x00, Page: 5, Code: SmallCharm,
				MagicAttributes: []MagicAttribute{
					{ID: statMagicFind, Values: []int64{50}},
				},
			},
		},
	}

	stats := ComputeStats(save)

	// Fire res: 30 + 20 = 50 (swap excluded, non-charm excluded)
	assertInt(t, "fire total", stats.Resistances.Fire.Total, 50)
	assertInt(t, "fire normal", stats.Resistances.Fire.Normal, 50)
	assertInt(t, "fire nightmare", stats.Resistances.Fire.Nightmare, 10)
	assertInt(t, "fire hell", stats.Resistances.Fire.Hell, -50)

	// Cold res: 20 + 5 = 25
	assertInt(t, "cold total", stats.Resistances.Cold.Total, 25)

	// Lightning res: 20
	assertInt(t, "lightning total", stats.Resistances.Lightning.Total, 20)

	// Poison res: 20
	assertInt(t, "poison total", stats.Resistances.Poison.Total, 20)

	// MF: 25 + 7 = 32 (swap excluded, stash charm excluded)
	assertInt(t, "magicFind", stats.MagicFind, 32)

	// FCR: 20, Paladin breakpoints: 0, 9, 18, 30, 48, 75, 125
	assertInt(t, "fcr total", stats.FCR.Total, 20)
	assertInt(t, "fcr breakpoint", stats.FCR.Current, 18)
	assertIntPtr(t, "fcr next", stats.FCR.NextBreakpoint, 30)

	// Life leech: 6
	assertInt(t, "lifeLeech", stats.LifeLeech, 6)

	// Crushing blow: 33
	assertInt(t, "crushingBlow", stats.CrushingBlow, 33)

	// No merc items
	if stats.Mercenary != nil {
		t.Error("expected nil mercenary stats")
	}
}

func TestComputeStats_RunewordAndSetAttributes(t *testing.T) {
	save := &D2S{
		Header:     Header{Class: Sorceress},
		Attributes: Attributes{Level: 85},
		Items: []Item{
			{
				Location: 0x01, EquipSlot: 3, Code: "plt",
				Runeword: true,
				MagicAttributes: []MagicAttribute{
					{ID: statFireResist, Values: []int64{10}},
				},
				RunewordAttributes: []MagicAttribute{
					{ID: statFireResist, Values: []int64{35}},
					{ID: statFCR, Values: []int64{20}},
				},
			},
			{
				Location: 0x01, EquipSlot: 1, Code: "cap",
				Quality: QualitySet,
				MagicAttributes: []MagicAttribute{
					{ID: statColdResist, Values: []int64{15}},
				},
				SetAttributes: [][]MagicAttribute{
					{{ID: statColdResist, Values: []int64{10}}},
					{{ID: statAllSkills, Values: []int64{1}}},
				},
			},
		},
	}

	stats := ComputeStats(save)

	// Fire: 10 (base) + 35 (runeword) = 45
	assertInt(t, "fire total", stats.Resistances.Fire.Total, 45)

	// Cold: 15 (base) + 10 (set bonus) = 25
	assertInt(t, "cold total", stats.Resistances.Cold.Total, 25)

	// FCR: 20 (runeword), Sorc breakpoints: 0, 9, 20, 37, 63, 105, 200
	assertInt(t, "fcr breakpoint", stats.FCR.Current, 20)
	assertIntPtr(t, "fcr next", stats.FCR.NextBreakpoint, 37)

	// All skills: 1 (from set bonus)
	assertInt(t, "allSkills", stats.AllSkills, 1)
}

func TestComputeStats_PerLevelStats(t *testing.T) {
	save := &D2S{
		Header:     Header{Class: Barbarian},
		Attributes: Attributes{Level: 80},
		Items: []Item{
			{
				Location: 0x01, EquipSlot: 1, Code: "cap",
				MagicAttributes: []MagicAttribute{
					{ID: statMagicFind, Values: []int64{25}},
					{ID: statMagicFindPerLvl, Values: []int64{8}}, // floor(80*8/128) = 5
					{ID: statGoldFindPerLvl, Values: []int64{16}}, // floor(80*16/128) = 10
				},
			},
		},
	}

	stats := ComputeStats(save)
	assertInt(t, "magicFind", stats.MagicFind, 30) // 25 + 5
	assertInt(t, "goldFind", stats.GoldFind, 10)
}

func TestComputeStats_ClassSkills(t *testing.T) {
	save := &D2S{
		Header:     Header{Class: Necromancer},
		Attributes: Attributes{Level: 50},
		Items: []Item{
			{
				Location: 0x01, EquipSlot: 2, Code: "amu",
				MagicAttributes: []MagicAttribute{
					{ID: statAllSkills, Values: []int64{2}},
					// +3 Necro skills (classID=2)
					{ID: statClassSkills, Values: []int64{2, 3}},
					// +1 Sorc skills (classID=1) — should be excluded
					{ID: statClassSkills, Values: []int64{1, 1}},
					// +2 Poison and Bone (tab=7, classID=2)
					{ID: statSkillTab, Values: []int64{7, 2, 2}},
					// +1 Fire (tab=3, classID=1) — wrong class, excluded
					{ID: statSkillTab, Values: []int64{3, 1, 1}},
				},
			},
		},
	}

	stats := ComputeStats(save)
	assertInt(t, "allSkills", stats.AllSkills, 2)
	assertInt(t, "classSkills", stats.ClassSkills, 3)
	if got := stats.SkillTrees["Poison and Bone"]; got != 2 {
		t.Errorf("Poison and Bone = %d, want 2", got)
	}
	if got := stats.SkillTrees["Fire"]; got != 0 {
		t.Errorf("Fire tree = %d, want 0 (wrong class)", got)
	}
}

func TestComputeStats_Mercenary(t *testing.T) {
	save := &D2S{
		Header:     Header{Class: Paladin},
		Attributes: Attributes{Level: 75},
		MercItems: []Item{
			{
				Code: "plt",
				MagicAttributes: []MagicAttribute{
					{ID: statFireResist, Values: []int64{30}},
					{ID: statLifeLeech, Values: []int64{8}},
					{ID: statMagicFind, Values: []int64{40}},
				},
				SocketedItems: []Item{
					{
						Code:            OrtRune,
						MagicAttributes: []MagicAttribute{{ID: statLightningResist, Values: []int64{35}}},
					},
				},
			},
		},
	}

	stats := ComputeStats(save)
	if stats.Mercenary == nil {
		t.Fatal("expected mercenary stats")
	}
	assertInt(t, "merc fire", stats.Mercenary.Resistances.Fire.Total, 30)
	assertInt(t, "merc lightning", stats.Mercenary.Resistances.Lightning.Total, 35)
	assertInt(t, "merc lifeLeech", stats.Mercenary.LifeLeech, 8)
	assertInt(t, "merc mf", stats.Mercenary.MagicFind, 40)
}

func TestComputeStats_Atmus(t *testing.T) {
	data, err := os.ReadFile("../testdata/Atmus.d2s")
	if err != nil {
		t.Skipf("skip: test save not available: %v", err)
	}

	save, err := Parse(data)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	stats := ComputeStats(save)

	// Log all stats for visibility.
	t.Logf("Character: %s, Level %d %s", save.Header.Name, save.Attributes.Level, save.Header.Class)
	t.Logf("Resistances: fire=%+v cold=%+v light=%+v poison=%+v",
		stats.Resistances.Fire, stats.Resistances.Cold,
		stats.Resistances.Lightning, stats.Resistances.Poison)
	t.Logf("MF=%d GF=%d", stats.MagicFind, stats.GoldFind)
	t.Logf("FCR=%+v FHR=%+v IAS=%+v FRW=%d",
		stats.FCR, stats.FHR, stats.IAS, stats.FRW)
	t.Logf("LifeLeech=%d ManaLeech=%d CB=%d DS=%d OW=%d",
		stats.LifeLeech, stats.ManaLeech, stats.CrushingBlow,
		stats.DeadlyStrike, stats.OpenWounds)
	t.Logf("AllSkills=%d ClassSkills=%d Trees=%v",
		stats.AllSkills, stats.ClassSkills, stats.SkillTrees)

	if stats.Mercenary != nil {
		t.Logf("Merc: fire=%d cold=%d light=%d poison=%d MF=%d LifeLeech=%d",
			stats.Mercenary.Resistances.Fire.Total,
			stats.Mercenary.Resistances.Cold.Total,
			stats.Mercenary.Resistances.Lightning.Total,
			stats.Mercenary.Resistances.Poison.Total,
			stats.Mercenary.MagicFind,
			stats.Mercenary.LifeLeech)
	}

	// Atmus is a level 74 Warlock — pin known values from real save.
	assertInt(t, "class", int(save.Header.Class), int(Warlock))
	assertInt(t, "fire resist", stats.Resistances.Fire.Total, 73)
	assertInt(t, "cold resist", stats.Resistances.Cold.Total, 62)
	assertInt(t, "lightning resist", stats.Resistances.Lightning.Total, 110)
	assertInt(t, "poison resist", stats.Resistances.Poison.Total, 55)
	assertInt(t, "magicFind", stats.MagicFind, 81)
	assertInt(t, "goldFind", stats.GoldFind, 191)
	assertInt(t, "fcr total", stats.FCR.Total, 35)
	assertInt(t, "fcr breakpoint", stats.FCR.Current, 30)
	assertInt(t, "fhr total", stats.FHR.Total, 40)
	assertInt(t, "fhr breakpoint", stats.FHR.Current, 30)
	assertInt(t, "frw", stats.FRW, 27)
	assertInt(t, "lifeLeech", stats.LifeLeech, 9)
	assertInt(t, "allSkills", stats.AllSkills, 1)
	assertInt(t, "classSkills", stats.ClassSkills, 2)
}

func TestFCRBreakpoints_AllClasses(t *testing.T) {
	classes := []Class{Amazon, Sorceress, Necromancer, Paladin, Barbarian, Druid, Assassin, Warlock}
	for _, c := range classes {
		table := FCRBreakpoints(c)
		if len(table) == 0 {
			t.Errorf("FCR table for %s is empty", c)
		}
		if table[0] != 0 {
			t.Errorf("FCR table for %s doesn't start at 0", c)
		}
	}
}

func TestFHRBreakpoints_AllClasses(t *testing.T) {
	classes := []Class{Amazon, Sorceress, Necromancer, Paladin, Barbarian, Druid, Assassin, Warlock}
	for _, c := range classes {
		table := FHRBreakpoints(c)
		if len(table) == 0 {
			t.Errorf("FHR table for %s is empty", c)
		}
		if table[0] != 0 {
			t.Errorf("FHR table for %s doesn't start at 0", c)
		}
	}
}

func assertInt(t *testing.T, name string, got, want int) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %d, want %d", name, got, want)
	}
}

func assertIntPtr(t *testing.T, name string, got *int, want int) {
	t.Helper()
	if got == nil {
		t.Errorf("%s = nil, want %d", name, want)
	} else if *got != want {
		t.Errorf("%s = %d, want %d", name, *got, want)
	}
}

func intPtr(v int) *int { return &v }
