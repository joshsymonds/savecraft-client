package d2s

import "fmt"

// Attribute IDs in the bit-packed section.
const (
	attrStrength       = 0
	attrEnergy         = 1
	attrDexterity      = 2
	attrVitality       = 3
	attrUnusedStats    = 4
	attrUnusedSkills   = 5
	attrCurrentHP      = 6
	attrMaxHP          = 7
	attrCurrentMana    = 8
	attrMaxMana        = 9
	attrCurrentStamina = 10
	attrMaxStamina     = 11
	attrLevel          = 12
	attrExperience     = 13
	attrGold           = 14
	attrStashedGold    = 15
)

// attributeBitMap maps each attribute ID to its bit length.
var attributeBitMap = map[uint64]uint{
	attrStrength:       10,
	attrEnergy:         10,
	attrDexterity:      10,
	attrVitality:       10,
	attrUnusedStats:    10,
	attrUnusedSkills:   8,
	attrCurrentHP:      21,
	attrMaxHP:          21,
	attrCurrentMana:    21,
	attrMaxMana:        21,
	attrCurrentStamina: 21,
	attrMaxStamina:     21,
	attrLevel:          7,
	attrExperience:     32,
	attrGold:           25,
	attrStashedGold:    25,
}

func parseAttributes(br *bitReader) (Attributes, error) {
	var a Attributes

	for {
		id, err := br.ReadBits(9)
		if err != nil {
			return a, fmt.Errorf("read attribute id: %w", err)
		}

		// 0x1ff terminates the attribute list.
		if id == 0x1ff {
			break
		}

		length, ok := attributeBitMap[id]
		if !ok {
			return a, fmt.Errorf("unknown attribute id: %d", id)
		}

		val, err := br.ReadBits(length)
		if err != nil {
			return a, fmt.Errorf("read attribute %d: %w", id, err)
		}

		switch id {
		case attrStrength:
			a.Strength = uint32(val)
		case attrEnergy:
			a.Energy = uint32(val)
		case attrDexterity:
			a.Dexterity = uint32(val)
		case attrVitality:
			a.Vitality = uint32(val)
		case attrUnusedStats:
			a.UnusedStats = uint32(val)
		case attrUnusedSkills:
			a.UnusedSkills = uint32(val)
		case attrCurrentHP:
			a.CurrentHP = uint32(val)
		case attrMaxHP:
			a.MaxHP = uint32(val)
		case attrCurrentMana:
			a.CurrentMana = uint32(val)
		case attrMaxMana:
			a.MaxMana = uint32(val)
		case attrCurrentStamina:
			a.CurrentStamina = uint32(val)
		case attrMaxStamina:
			a.MaxStamina = uint32(val)
		case attrLevel:
			a.Level = uint32(val)
		case attrExperience:
			a.Experience = uint32(val)
		case attrGold:
			a.Gold = uint32(val)
		case attrStashedGold:
			a.StashedGold = uint32(val)
		}
	}

	return a, nil
}
