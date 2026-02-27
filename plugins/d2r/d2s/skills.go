package d2s

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
)

type rawSkillData struct {
	Header [2]byte
	List   [30]byte
}

// skillOffsetMap maps character class to the starting skill ID offset.
var skillOffsetMap = map[Class]int{
	Amazon:      6,
	Sorceress:   36,
	Necromancer: 66,
	Paladin:     96,
	Barbarian:   126,
	Druid:       221,
	Assassin:    251,
	Warlock:     373, // RotW expansion
}

// skillMap maps skill IDs to human-readable names.
var skillMap = map[int]string{
	0: "Attack", 1: "Kick", 2: "Throw Item", 3: "Unsummon",
	4: "Left Hand Throw", 5: "Left Hand Swing",
	// Amazon
	6: "Magic Arrow", 7: "Fire Arrow", 8: "Inner Sight", 9: "Critical Strike",
	10: "Jab", 11: "Cold Arrow", 12: "Multiple Shot", 13: "Dodge",
	14: "Power Strike", 15: "Poison Javelin", 16: "Exploding Arrow",
	17: "Slow Missiles", 18: "Avoid", 19: "Impale", 20: "Lightning Bolt",
	21: "Ice Arrow", 22: "Guided Arrow", 23: "Penetrate", 24: "Charged Strike",
	25: "Plague Javelin", 26: "Strafe", 27: "Immolation Arrow", 28: "Dopplezon",
	29: "Evade", 30: "Fend", 31: "Freezing Arrow", 32: "Valkyrie",
	33: "Pierce", 34: "Lightning Strike", 35: "Lightning Fury",
	// Sorceress
	36: "Fire Bolt", 37: "Warmth", 38: "Charged Bolt", 39: "Ice Bolt",
	40: "Frozen Armor", 41: "Inferno", 42: "Static Field", 43: "Telekinesis",
	44: "Frost Nova", 45: "Ice Blast", 46: "Blaze", 47: "Fire Ball",
	48: "Nova", 49: "Lightning", 50: "Shiver Armor", 51: "Fire Wall",
	52: "Enchant", 53: "Chain Lightning", 54: "Teleport", 55: "Glacial Spike",
	56: "Meteor", 57: "Thunder Storm", 58: "Energy Shield", 59: "Blizzard",
	60: "Chilling Armor", 61: "Fire Mastery", 62: "Hydra",
	63: "Lightning Mastery", 64: "Frozen Orb", 65: "Cold Mastery",
	// Necromancer
	66: "Amplify Damage", 67: "Teeth", 68: "Bone Armor", 69: "Skeleton Mastery",
	70: "Raise Skeleton", 71: "Dim Vision", 72: "Weaken", 73: "Poison Dagger",
	74: "Corpse Explosion", 75: "Clay Golem", 76: "Iron Maiden", 77: "Terror",
	78: "Bone Wall", 79: "Golem Mastery", 80: "Raise Skeletal Mage",
	81: "Confuse", 82: "Life Tap", 83: "Poison Explosion", 84: "Bone Spear",
	85: "Blood Golem", 86: "Attract", 87: "Decrepify", 88: "Bone Prison",
	89: "Summon Resist", 90: "Iron Golem", 91: "Lower Resist",
	92: "Poison Nova", 93: "Bone Spirit", 94: "Fire Golem", 95: "Revive",
	// Paladin
	96: "Sacrifice", 97: "Smite", 98: "Might", 99: "Prayer",
	100: "Resist Fire", 101: "Holy Bolt", 102: "Holy Fire", 103: "Thorns",
	104: "Defiance", 105: "Resist Cold", 106: "Zeal", 107: "Charge",
	108: "Blessed Aim", 109: "Cleansing", 110: "Resist Lightning",
	111: "Vengeance", 112: "Blessed Hammer", 113: "Concentration",
	114: "Holy Freeze", 115: "Vigor", 116: "Conversion", 117: "Holy Shield",
	118: "Holy Shock", 119: "Sanctuary", 120: "Meditation",
	121: "Fist Of The Heavens", 122: "Fanaticism", 123: "Conviction",
	124: "Redemption", 125: "Salvation",
	// Barbarian
	126: "Bash", 127: "Sword Mastery", 128: "Axe Mastery", 129: "Mace Mastery",
	130: "Howl", 131: "Find Potion", 132: "Leap", 133: "Double Swing",
	134: "Pole Arm Mastery", 135: "Throwing Mastery", 136: "Spear Mastery",
	137: "Taunt", 138: "Shout", 139: "Stun", 140: "Double Throw",
	141: "Increased Stamina", 142: "Find Item", 143: "Leap Attack",
	144: "Concentrate", 145: "Iron Skin", 146: "Battle Cry", 147: "Frenzy",
	148: "Increased Speed", 149: "Battle Orders", 150: "Grim Ward",
	151: "Whirlwind", 152: "Berserk", 153: "Natural Resistance",
	154: "War Cry", 155: "Battle Command",
	// Druid
	221: "Raven", 222: "Poison Creeper", 223: "Werewolf",
	224: "Shape Shifting", 225: "Firestorm", 226: "Oak Sage",
	227: "Summon Spirit Wolf", 228: "Wearbear", 229: "Molten Boulder",
	230: "Arctic Blast", 231: "Cycle Of Life", 232: "Feral Rage",
	233: "Maul", 234: "Eruption", 235: "Cyclone Armor",
	236: "Heart Of Wolverine", 237: "Summon Fenris", 238: "Rabies",
	239: "Fire Claws", 240: "Twister", 241: "Vines", 242: "Hunger",
	243: "Shock Wave", 244: "Volcano", 245: "Tornado",
	246: "Spirit Of Barbs", 247: "Summon Grizzly", 248: "Fury",
	249: "Armageddon", 250: "Hurricane",
	// Assassin
	251: "Fire Blast", 252: "Claw Mastery", 253: "Psychic Hammer",
	254: "Tiger Strike", 255: "Dragon Talon", 256: "Shock Field",
	257: "Blade Sentinel", 258: "Quickness", 259: "Fists Of Fire",
	260: "Dragon Claw", 261: "Charged Bolt Sentry", 262: "Wake Of Fire Sentry",
	263: "Weapon Block", 264: "Cloak Of Shadows", 265: "Cobra Strike",
	266: "Blade Fury", 267: "Fade", 268: "Shadow Warrior",
	269: "Claws Of Thunder", 270: "Dragon Tail", 271: "Lightning Sentry",
	272: "Inferno Sentry", 273: "Mind Blast", 274: "Blades Of Ice",
	275: "Dragon Flight", 276: "Death Sentry", 277: "Blade Shield",
	278: "Venom", 279: "Shadow Master", 280: "Royal Strike",
	// Warlock
	373: "Summon Goatman", 374: "Demonic Mastery", 375: "Death Mark",
	376: "Summon Tainted", 377: "Summon Defiler", 378: "Blood Oath",
	379: "Engorge", 380: "Blood Boil", 381: "Consume", 382: "Bind Demon",
	383: "Levitate", 384: "Eldritch Blast", 385: "Hex Bane",
	386: "Hex Siphon", 387: "Psychic Ward", 388: "Echoing Strike",
	389: "Hex Purge", 390: "Blade Warp", 391: "Cleave",
	392: "Mirrored Blades", 393: "Sigil Lethargy", 394: "Ring of Fire",
	395: "Miasma Bolt", 396: "Sigil Rancor", 397: "Enhanced Entropy",
	398: "Flame Wave", 399: "Miasma Chains", 400: "Sigil Death",
	401: "Apocalypse", 402: "Abyss",
}

// SkillName returns the human-readable name for a skill ID, or "" if unknown.
func SkillName(id int) string {
	return skillMap[id]
}

func parseSkills(r io.Reader, class Class) ([]Skill, error) {
	buf := make([]byte, 32)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, fmt.Errorf("read skills: %w", err)
	}

	var raw rawSkillData
	if err := binary.Read(bytes.NewReader(buf), binary.LittleEndian, &raw); err != nil {
		return nil, fmt.Errorf("decode skills: %w", err)
	}

	if string(raw.Header[:]) != "if" {
		return nil, fmt.Errorf("missing skill header 'if', got %q", string(raw.Header[:]))
	}

	offset, ok := skillOffsetMap[class]
	if !ok {
		return nil, fmt.Errorf("unknown skill offset for class %d", class)
	}

	skills := make([]Skill, 0, 30)
	for i, pts := range raw.List {
		id := i + offset
		name := skillMap[id]
		if name == "" {
			name = fmt.Sprintf("Unknown Skill %d", id)
		}
		skills = append(skills, Skill{
			ID:    id,
			Name:  name,
			Level: pts,
		})
	}

	return skills, nil
}
