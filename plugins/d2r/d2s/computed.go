package d2s

// ComputedStats holds aggregated stats from equipped items and charms.
type ComputedStats struct {
	Resistances  Resistances     `json:"resistances"`
	MagicFind    int             `json:"magicFind"`
	GoldFind     int             `json:"goldFind"`
	FCR          Breakpoint      `json:"fasterCastRate"`
	FHR          Breakpoint      `json:"fasterHitRecovery"`
	IAS          Breakpoint      `json:"increasedAttackSpeed"`
	FRW          int             `json:"fasterRunWalk"`
	LifeLeech    int             `json:"lifeLeech"`
	ManaLeech    int             `json:"manaLeech"`
	CrushingBlow int             `json:"crushingBlow"`
	DeadlyStrike int             `json:"deadlyStrike"`
	OpenWounds   int             `json:"openWounds"`
	AllSkills    int             `json:"allSkills"`
	ClassSkills  int             `json:"classSkills"`
	SkillTrees   map[string]int  `json:"skillTrees"`
	Mercenary    *MercenaryStats `json:"mercenary,omitempty"`
}

// MercenaryStats holds aggregated stats from mercenary equipment.
type MercenaryStats struct {
	Resistances  Resistances `json:"resistances"`
	MagicFind    int         `json:"magicFind"`
	GoldFind     int         `json:"goldFind"`
	LifeLeech    int         `json:"lifeLeech"`
	ManaLeech    int         `json:"manaLeech"`
	CrushingBlow int         `json:"crushingBlow"`
	DeadlyStrike int         `json:"deadlyStrike"`
	OpenWounds   int         `json:"openWounds"`
}

// Resistances holds per-element resistance totals with per-difficulty effective values.
type Resistances struct {
	Fire      ResistanceValues `json:"fire"`
	Cold      ResistanceValues `json:"cold"`
	Lightning ResistanceValues `json:"lightning"`
	Poison    ResistanceValues `json:"poison"`
}

// ResistanceValues holds a single element's resistance total and effective values.
type ResistanceValues struct {
	Total     int `json:"total"`
	Normal    int `json:"normal"`
	Nightmare int `json:"nightmare"`
	Hell      int `json:"hell"`
}

func newResistanceValues(total int) ResistanceValues {
	return ResistanceValues{
		Total:     total,
		Normal:    total,
		Nightmare: total - 40,
		Hell:      total - 100,
	}
}

// Stat IDs for aggregation targets.
const (
	statFireResist      = 39
	statLightningResist = 41
	statColdResist      = 43
	statPoisonResist    = 45
	statLifeLeech       = 60
	statManaLeech       = 62
	statGoldFind        = 79
	statMagicFind       = 80
	statClassSkills     = 83
	statIAS             = 93
	statFRW             = 96
	statFHR             = 99
	statFCR             = 105
	statAllSkills       = 127
	statOpenWounds      = 135
	statCrushingBlow    = 136
	statDeadlyStrike    = 141
	statSkillTab        = 188
	statGoldFindPerLvl  = 239
	statMagicFindPerLvl = 240
)

// perLevelValue computes the effective value for a per-level stat.
// D2 formula: floor(clvl * val / 128).
func perLevelValue(val int64, level int) int {
	return int(int64(level) * val / 128)
}

// isCharm returns true if the item code is a charm.
func isCharm(code string) bool {
	return code == SmallCharm || code == LargeCharm || code == GrandCharm
}

// isSwapSlot returns true for weapon swap equipment slots.
func isSwapSlot(slot byte) bool {
	return slot == 11 || slot == 12
}

// ComputeStats aggregates stats from equipped items and inventory charms.
// Weapon swap slots (11, 12) are excluded. Socketed item (rune/gem) stats
// live on each SocketedItem and are traversed by aggregateItems.
func ComputeStats(save *D2S) ComputedStats {
	class := save.Header.Class
	level := int(save.Attributes.Level)

	// Collect contributing items: equipped (non-swap) + inventory charms.
	var items []Item
	for _, item := range save.Items {
		if item.Location == 0x01 && !isSwapSlot(item.EquipSlot) {
			items = append(items, item)
		} else if item.Location == 0x00 && item.Page == 1 && isCharm(item.Code) {
			items = append(items, item)
		}
	}

	acc := aggregateItems(items, level, class)

	stats := ComputedStats{
		Resistances: Resistances{
			Fire:      newResistanceValues(acc.fireRes),
			Cold:      newResistanceValues(acc.coldRes),
			Lightning: newResistanceValues(acc.lightRes),
			Poison:    newResistanceValues(acc.poisonRes),
		},
		MagicFind:    acc.magicFind,
		GoldFind:     acc.goldFind,
		FCR:          FindBreakpoint(FCRBreakpoints(class), acc.fcr),
		FHR:          FindBreakpoint(FHRBreakpoints(class), acc.fhr),
		IAS:          FindBreakpoint(IASBreakpoints(), acc.ias),
		FRW:          acc.frw,
		LifeLeech:    acc.lifeLeech,
		ManaLeech:    acc.manaLeech,
		CrushingBlow: acc.crushingBlow,
		DeadlyStrike: acc.deadlyStrike,
		OpenWounds:   acc.openWounds,
		AllSkills:    acc.allSkills,
		ClassSkills:  acc.classSkills,
		SkillTrees:   acc.skillTrees,
	}

	// Mercenary stats (separate aggregation, no breakpoints).
	if len(save.MercItems) > 0 {
		mercAcc := aggregateItems(save.MercItems, level, class)
		stats.Mercenary = &MercenaryStats{
			Resistances: Resistances{
				Fire:      newResistanceValues(mercAcc.fireRes),
				Cold:      newResistanceValues(mercAcc.coldRes),
				Lightning: newResistanceValues(mercAcc.lightRes),
				Poison:    newResistanceValues(mercAcc.poisonRes),
			},
			MagicFind:    mercAcc.magicFind,
			GoldFind:     mercAcc.goldFind,
			LifeLeech:    mercAcc.lifeLeech,
			ManaLeech:    mercAcc.manaLeech,
			CrushingBlow: mercAcc.crushingBlow,
			DeadlyStrike: mercAcc.deadlyStrike,
			OpenWounds:   mercAcc.openWounds,
		}
	}

	return stats
}

// accumulator gathers raw stat totals during a single pass over items.
type accumulator struct {
	fireRes, coldRes, lightRes, poisonRes  int
	magicFind, goldFind                    int
	fcr, fhr, ias, frw                     int
	lifeLeech, manaLeech                   int
	crushingBlow, deadlyStrike, openWounds int
	allSkills, classSkills                 int
	skillTrees                             map[string]int
}

func aggregateItems(items []Item, level int, class Class) accumulator {
	acc := accumulator{skillTrees: make(map[string]int)}

	for i := range items {
		accumulateAttrs(&acc, items[i].MagicAttributes, level, class)
		accumulateAttrs(&acc, items[i].RunewordAttributes, level, class)
		for _, setList := range items[i].SetAttributes {
			accumulateAttrs(&acc, setList, level, class)
		}
		for si := range items[i].SocketedItems {
			accumulateAttrs(&acc, items[i].SocketedItems[si].MagicAttributes, level, class)
		}
	}

	return acc
}

func accumulateAttrs(acc *accumulator, attrs []MagicAttribute, level int, class Class) {
	for _, a := range attrs {
		if len(a.Values) == 0 {
			continue
		}
		v := int(a.Values[0])

		switch a.ID {
		case statFireResist:
			acc.fireRes += v
		case statColdResist:
			acc.coldRes += v
		case statLightningResist:
			acc.lightRes += v
		case statPoisonResist:
			acc.poisonRes += v
		case statMagicFind:
			acc.magicFind += v
		case statGoldFind:
			acc.goldFind += v
		case statMagicFindPerLvl:
			acc.magicFind += perLevelValue(a.Values[0], level)
		case statGoldFindPerLvl:
			acc.goldFind += perLevelValue(a.Values[0], level)
		case statFCR:
			acc.fcr += v
		case statFHR:
			acc.fhr += v
		case statIAS:
			acc.ias += v
		case statFRW:
			acc.frw += v
		case statLifeLeech:
			acc.lifeLeech += v
		case statManaLeech:
			acc.manaLeech += v
		case statCrushingBlow:
			acc.crushingBlow += v
		case statDeadlyStrike:
			acc.deadlyStrike += v
		case statOpenWounds:
			acc.openWounds += v
		case statAllSkills:
			acc.allSkills += v
		case statClassSkills:
			// Values = [classID, level]. Only count if it matches the character's class.
			if len(a.Values) >= 2 && int(a.Values[0]) == int(class) {
				acc.classSkills += int(a.Values[1])
			}
		case statSkillTab:
			// Values = [tabIndex, classID, bonus]. Only count for character's class.
			if len(a.Values) >= 3 && int(a.Values[1]) == int(class) {
				tabIdx := int(a.Values[0])
				tabName := SkilltabNameForIdx(tabIdx)
				acc.skillTrees[tabName] += int(a.Values[2])
			}
		}
	}
}

// SkilltabNameForIdx returns a human-readable skill tree name.
// This is the canonical source for skill tab names — also used by parser/main.go.
func SkilltabNameForIdx(idx int) string {
	if idx >= 0 && idx < len(SkilltabNames) {
		return SkilltabNames[idx]
	}
	return "Unknown"
}

// SkilltabNames maps tab index to display name.
var SkilltabNames = []string{
	"Bow and Crossbow", "Passive and Magic", "Javelin and Spear",
	"Fire", "Lightning", "Cold",
	"Curses", "Poison and Bone", "Summoning",
	"Combat (Paladin)", "Offensive Auras", "Defensive Auras",
	"Combat (Barbarian)", "Masteries", "Warcries",
	"Summoning (Druid)", "Shape Shifting", "Elemental",
	"Traps", "Shadow Disciplines", "Martial Arts",
	"Summoning (Warlock)", "Hex", "Sigils",
}
