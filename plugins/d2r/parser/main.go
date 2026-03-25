// D2R plugin: parses Diablo II Resurrected .d2s and .d2i files into structured GameState.
// Supports both LoD (version <= 0x60) and D2R (version >= 0x61) formats.
// .d2s = character saves, .d2i = shared stash (game-scoped, saveName from stash kind).
//
// Build: GOOS=wasip1 GOARCH=wasm go build -o parser.wasm ./parser
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/joshsymonds/savecraft.gg/plugins/d2r/d2s"
)

func main() {
	enc := json.NewEncoder(os.Stdout)

	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		writeError(enc, "read_error", "failed to read stdin: "+err.Error())
		os.Exit(1)
	}

	if d2s.IsStash(data) {
		handleStash(enc, data)
	} else {
		handleCharacter(enc, data)
	}
}

func handleStash(enc *json.Encoder, data []byte) {
	writeStatusf(enc, "Shared stash file, %d bytes", len(data))

	stash, err := d2s.ParseStash(data)
	if err != nil {
		writeError(enc, "parse_error", err.Error())
		os.Exit(1)
	}

	totalItems := 0
	nonEmptyTabs := 0
	for _, tab := range stash.Tabs {
		totalItems += len(tab.Items)
		if len(tab.Items) > 0 {
			nonEmptyTabs++
		}
	}
	writeStatusf(enc, "%d items across %d tabs", totalItems, nonEmptyTabs)

	sections := buildStashSections(stash)
	summary := buildStashSummary(stash)

	kind := "Softcore"
	if stash.Kind == 0 {
		kind = "Hardcore"
	}

	if err := enc.Encode(map[string]any{
		"type": "result",
		"identity": map[string]any{
			"saveName": fmt.Sprintf("Shared Stash (%s)", kind),
			"gameId":   "d2r",
		},
		"summary":  summary,
		"sections": sections,
	}); err != nil {
		os.Exit(1)
	}
}

func handleCharacter(enc *json.Encoder, data []byte) {
	writeStatusf(enc, "Character save, %d bytes", len(data))

	save, err := d2s.Parse(data)
	if err != nil {
		writeError(enc, "parse_error", err.Error())
		os.Exit(1)
	}

	writeStatusf(
		enc,
		"Character: %s, Level %d %s",
		save.Header.Name,
		save.Attributes.Level,
		save.Header.Class,
	)

	socketed := 0
	for _, item := range save.Items {
		if item.Socketed {
			socketed++
		}
	}
	if socketed > 0 {
		writeStatusf(enc, "%d items, %d socketed", len(save.Items), socketed)
	} else {
		writeStatusf(enc, "%d items", len(save.Items))
	}

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

	// Computed/aggregated stats from equipped items + charms.
	sections["totals"] = map[string]any{
		"description": "Aggregated character stats: resistances (per difficulty), magic find, gold find, faster cast rate, faster hit recovery, attack speed, run/walk speed, crushing blow, deadly strike, open wounds, life/mana leech, skill bonuses — with FCR/FHR/IAS breakpoints. Includes mercenary totals. Use to evaluate gear upgrades or check breakpoints.",
		"data":        d2s.ComputeStats(save),
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

	summary := buildSummary(save)

	if err := enc.Encode(map[string]any{
		"type": "result",
		"identity": map[string]any{
			"saveName": save.Header.Name,
			"gameId":   "d2r",
		},
		"summary":  summary,
		"sections": sections,
	}); err != nil {
		os.Exit(1)
	}
}

func buildSummary(save *d2s.D2S) string {
	h := save.Header
	a := save.Attributes

	diff := "Normal"
	if h.CurrentDifficulty.Active {
		diff = h.CurrentDifficulty.Difficulty.String()
	}

	return fmt.Sprintf("%s, Level %d %s (%s)", h.Name, a.Level, h.Class, diff)
}

func buildCharacterSection(save *d2s.D2S) map[string]any {
	h := save.Header
	result := map[string]any{
		"name":       h.Name,
		"class":      h.Class.String(),
		"level":      save.Attributes.Level,
		"expansion":  h.Status.Expansion,
		"hardcore":   h.Status.Hardcore,
		"ladder":     h.Status.Ladder,
		"lastPlayed": h.LastPlayed,
	}

	if h.CurrentDifficulty.Active {
		result["difficulty"] = h.CurrentDifficulty.Difficulty.String()
		result["act"] = h.CurrentDifficulty.Act + 1
	}

	if h.Mercenary.ID != 0 {
		result["mercenary"] = map[string]any{
			"id":         h.Mercenary.ID,
			"type":       h.Mercenary.Type,
			"experience": h.Mercenary.Experience,
			"dead":       h.Mercenary.Dead,
		}
	}

	return result
}

func buildAttributesSection(save *d2s.D2S) map[string]any {
	a := save.Attributes
	return map[string]any{
		"strength":       a.Strength,
		"dexterity":      a.Dexterity,
		"vitality":       a.Vitality,
		"energy":         a.Energy,
		"unusedStats":    a.UnusedStats,
		"unusedSkills":   a.UnusedSkills,
		"currentHP":      a.CurrentHP / 256,
		"maxHP":          a.MaxHP / 256,
		"currentMana":    a.CurrentMana / 256,
		"maxMana":        a.MaxMana / 256,
		"currentStamina": a.CurrentStamina / 256,
		"maxStamina":     a.MaxStamina / 256,
		"level":          a.Level,
		"experience":     a.Experience,
		"gold":           a.Gold,
		"stashedGold":    a.StashedGold,
	}
}

func buildSkillsSection(save *d2s.D2S) []map[string]any {
	var skills []map[string]any
	for _, s := range save.Skills {
		if s.Level > 0 {
			skills = append(skills, map[string]any{
				"id":    s.ID,
				"name":  s.Name,
				"level": s.Level,
			})
		}
	}
	return skills
}

func buildEquipmentSection(save *d2s.D2S) []map[string]any {
	var equipped []map[string]any
	for _, item := range save.Items {
		if item.Location == 0x01 { // equipped
			m := buildItemMap(item)
			if slot := equipSlotName(item.EquipSlot); slot != "" {
				m["slot"] = slot
			}
			equipped = append(equipped, m)
		}
	}
	return equipped
}

func equipSlotName(slot byte) string {
	switch slot {
	case 1:
		return "Helm"
	case 2:
		return "Amulet"
	case 3:
		return "Body Armor"
	case 4:
		return "Right Hand"
	case 5:
		return "Left Hand"
	case 6:
		return "Right Ring"
	case 7:
		return "Left Ring"
	case 8:
		return "Belt"
	case 9:
		return "Boots"
	case 10:
		return "Gloves"
	case 11:
		return "Right Hand (Swap)"
	case 12:
		return "Left Hand (Swap)"
	default:
		return ""
	}
}

func buildInventorySection(save *d2s.D2S) map[string]any {
	var inventory, stash, cube []map[string]any

	for _, item := range save.Items {
		if item.Location != 0x01 { // not equipped
			m := buildItemMap(item)
			switch item.Page {
			case 1: // inventory
				inventory = append(inventory, m)
			case 4: // cube
				cube = append(cube, m)
			case 5: // stash
				stash = append(stash, m)
			default:
				inventory = append(inventory, m)
			}
		}
	}

	return map[string]any{
		"inventory": inventory,
		"stash":     stash,
		"cube":      cube,
	}
}

func buildItemList(items []d2s.Item) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		result = append(result, buildItemMap(item))
	}
	return result
}

func buildItemMap(item d2s.Item) map[string]any {
	m := map[string]any{
		"code":     item.Code,
		"type":     item.TypeID,
		"typeName": item.TypeName,
	}

	if item.Identified {
		m["identified"] = true
	}
	if item.Ethereal {
		m["ethereal"] = true
	}

	// Quality and names
	if item.Quality > 0 {
		m["quality"] = item.Quality.String()
	}
	if item.UniqueName != "" {
		m["uniqueName"] = item.UniqueName
	}
	if item.SetName != "" {
		m["setName"] = item.SetName
	}
	if item.RareName != "" {
		m["rareName"] = item.RareName + " " + item.RareName2
	}
	if item.RunewordName != "" {
		m["runewordName"] = item.RunewordName
	}
	if item.MagicPrefixName != "" || item.MagicSuffixName != "" {
		m["magicName"] = item.MagicPrefixName + " " + item.MagicSuffixName
	}
	if item.PersonalizedName != "" {
		m["personalizedName"] = item.PersonalizedName
	}

	// Stats
	if item.Defense != 0 {
		m["defense"] = item.Defense
	}
	if item.MaxDurability > 0 {
		m["durability"] = fmt.Sprintf("%d/%d", item.CurDurability, item.MaxDurability)
	}
	if item.Quantity > 0 {
		m["quantity"] = item.Quantity
	}
	if item.BaseDamage != nil {
		d := item.BaseDamage
		if d.Min2H > 0 {
			m["damage"] = fmt.Sprintf("%d-%d (1H) / %d-%d (2H)", d.Min1H, d.Max1H, d.Min2H, d.Max2H)
		} else {
			m["damage"] = fmt.Sprintf("%d-%d", d.Min1H, d.Max1H)
		}
	}
	if item.TotalSockets > 0 {
		m["sockets"] = item.TotalSockets
	}

	// Properties
	if len(item.MagicAttributes) > 0 {
		m["properties"] = buildPropertyList(item.MagicAttributes)
	}
	if len(item.RunewordAttributes) > 0 {
		m["runewordProperties"] = buildPropertyList(item.RunewordAttributes)
	}
	if len(item.SetAttributes) > 0 {
		var setBonuses [][]map[string]any
		for _, setList := range item.SetAttributes {
			if len(setList) > 0 {
				setBonuses = append(setBonuses, buildPropertyList(setList))
			}
		}
		if len(setBonuses) > 0 {
			m["setBonuses"] = setBonuses
		}
	}

	// Socketed items
	if len(item.SocketedItems) > 0 {
		m["socketedItems"] = buildItemList(item.SocketedItems)
	}

	return m
}

// internalOnlyProps are stats stored in the item bitstream but already displayed
// as dedicated fields (defense, durability, sockets, etc.). Skip them in output.
var internalOnlyProps = map[uint64]bool{
	67: true, 68: true, 71: true, 72: true, 73: true,
	82: true, 90: true, 92: true, 94: true, 140: true,
	159: true, 160: true, 181: true, 185: true, 186: true, 194: true,
	324: true, 356: true,
}

func buildPropertyList(attrs []d2s.MagicAttribute) []map[string]any {
	props := make([]map[string]any, 0, len(attrs))
	for _, a := range attrs {
		if internalOnlyProps[a.ID] {
			continue
		}
		props = append(props, map[string]any{
			"name": formatProperty(a),
		})
	}
	return props
}

func formatProperty(a d2s.MagicAttribute) string {
	switch a.ID {
	// Encode=2: chance-to-cast. Values = [level, skillID, chance%].
	case 195, 196, 197, 198, 199, 201:
		if len(a.Values) >= 3 {
			trigger := chanceTocastTrigger[a.ID]
			return fmt.Sprintf("%d%% Chance to Cast Level %d %s %s",
				a.Values[2], a.Values[0], skillOrID(a.Values[1]), trigger)
		}

	// Encode=3: charges. Values = [level, skillID, curCharges, maxCharges].
	case 204:
		if len(a.Values) >= 4 {
			return fmt.Sprintf("Level %d %s (%d/%d Charges)",
				a.Values[0], skillOrID(a.Values[1]), a.Values[2], a.Values[3])
		}

	// Skill bonus: Values = [skillID, level].
	case 97, 107:
		if len(a.Values) >= 2 {
			return fmt.Sprintf("+%d To %s", a.Values[1], skillOrID(a.Values[0]))
		}

	// Class skill bonus: Values = [classID, level].
	case 83:
		if len(a.Values) >= 2 {
			return fmt.Sprintf("+%d to %s Skill Levels", a.Values[1], className(int(a.Values[0])))
		}

	// Aura: Values = [skillID, level].
	case 151:
		if len(a.Values) >= 2 {
			return fmt.Sprintf(
				"Level %d %s Aura When Equipped",
				a.Values[1],
				skillOrID(a.Values[0]),
			)
		}

	// Skilltab: Values = [tabIndex, classID, bonus].
	case 188:
		if len(a.Values) >= 3 {
			return fmt.Sprintf("+%d to %s Skills (%s only)",
				a.Values[2], skilltabName(int(a.Values[0])), className(int(a.Values[1])))
		}

	// Compound damage: Values = [min, max] or [min, max, length].
	// Socketable table entries already have {0}-{1} templates — let those
	// fall through to standard substitution.
	case 48, 50, 52, 54, 57:
		if len(a.Values) >= 2 && !strings.Contains(a.Name, "{0}") {
			return fmt.Sprintf("%s %d-%d", a.Name, a.Values[0], a.Values[1])
		}
	}

	// Standard {N} substitution.
	name := a.Name
	for i, v := range a.Values {
		name = strings.ReplaceAll(name, fmt.Sprintf("{%d}", i), fmt.Sprintf("%d", v))
	}
	return name
}

var chanceTocastTrigger = map[uint64]string{
	195: "on Attack",
	196: "on Kill",
	197: "on Death",
	198: "on Strike",
	199: "on Level Up",
	201: "when Struck",
}

func skillOrID(id int64) string {
	if name := d2s.SkillName(int(id)); name != "" {
		return name
	}
	return fmt.Sprintf("Skill#%d", id)
}

func skilltabName(idx int) string {
	if name := d2s.SkilltabNameForIdx(idx); name != "Unknown" {
		return name
	}
	return fmt.Sprintf("Tab#%d", idx)
}

func className(id int) string {
	switch id {
	case 0:
		return "Amazon"
	case 1:
		return "Sorceress"
	case 2:
		return "Necromancer"
	case 3:
		return "Paladin"
	case 4:
		return "Barbarian"
	case 5:
		return "Druid"
	case 6:
		return "Assassin"
	case 7:
		return "Warlock"
	default:
		return fmt.Sprintf("Class#%d", id)
	}
}

func buildStashSummary(stash *d2s.SharedStash) string {
	totalItems := 0
	for _, tab := range stash.Tabs {
		totalItems += len(tab.Items)
	}

	kind := "Softcore"
	if stash.Kind == 0 {
		kind = "Hardcore"
	}

	return fmt.Sprintf("Shared Stash (%s), %d items, %d gold", kind, totalItems, stash.Gold)
}

func buildStashSections(stash *d2s.SharedStash) map[string]any {
	sections := map[string]any{
		"overview": map[string]any{
			"description": "Shared stash summary: gold, version, tab count — fetch first to see what tabs are available, then fetch individual tab sections",
			"data": map[string]any{
				"gold":    stash.Gold,
				"version": stash.Version,
				"tabs":    len(stash.Tabs),
			},
		},
	}

	tabNum := 0
	for _, tab := range stash.Tabs {
		if tab.Type == 2 { // metadata section, skip
			continue
		}
		tabNum++
		if len(tab.Items) == 0 {
			continue
		}

		tabType := "Normal"
		if tab.Type == 1 {
			tabType = "Advanced"
		}

		key := fmt.Sprintf("tab%d", tabNum)
		sections[key] = map[string]any{
			"description": fmt.Sprintf(
				"Stash tab %d (%s) — full item list with properties, use to find specific items or evaluate stored gear",
				tabNum,
				tabType,
			),
			"data": map[string]any{"items": buildItemList(tab.Items)},
		}
	}

	return sections
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
