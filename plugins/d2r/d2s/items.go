package d2s

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"strings"
)

// d2rVersion is the minimum file version for D2R format changes.
const d2rVersion = 0x61

type rawItemHeader struct {
	Header [2]byte
	Count  uint16
}

// parseItemList reads a "JM" + count header, then parses that many items.
func parseItemList(r io.ByteReader, version uint32, realm byte) ([]Item, error) {
	buf := make([]byte, 4)

	reader, ok := r.(io.Reader)
	if !ok {
		return nil, fmt.Errorf("byte reader does not implement io.Reader")
	}

	if _, err := io.ReadFull(reader, buf); err != nil {
		return nil, fmt.Errorf("read item header: %w", err)
	}

	var hdr rawItemHeader
	if err := binary.Read(bytes.NewReader(buf), binary.LittleEndian, &hdr); err != nil {
		return nil, fmt.Errorf("decode item header: %w", err)
	}
	if string(hdr.Header[:]) != "JM" {
		return nil, fmt.Errorf("expected item header 'JM', got %q", string(hdr.Header[:]))
	}

	return parseItems(r, int(hdr.Count), version, realm)
}

// parseItems reads itemCount items from the bit stream.
// Items with sockets cause additional items to follow inline.
func parseItems(r io.ByteReader, itemCount int, version uint32, realm byte) ([]Item, error) {
	var itemList []Item
	br := newBitReader(r)

	toRead := itemCount
	for i := 0; i < toRead; i++ {
		br.totalRead = 0 // reset per-item

		item, err := parseSimpleBits(br, version)
		if err != nil {
			return itemList, fmt.Errorf("item %d simple bits: %w", i, err)
		}

		if !item.SimpleItem {
			if err := parseExtendedBits(br, &item, version, realm); err != nil {
				return itemList, fmt.Errorf(
					"item %d (code=%q quality=%s) extended: %w",
					i, item.Code, item.Quality, err,
				)
			}
		}

		// v105: extra bit after all properties (all items, simple and complex).
		if version >= versionD2Rv105 {
			if _, err := br.ReadBits(1); err != nil {
				return itemList, fmt.Errorf("item %d v105 extra bit 2: %w", i, err)
			}
		}

		// RotW advanced stash stackable: 8 extra bits for certain items.
		if version >= versionD2Rv105 && realm == RealmRotW {
			if advancedStashStackableMap[item.Code] {
				qty, err := br.ReadBits(8)
				if err != nil {
					return itemList, fmt.Errorf("item %d advanced stash qty: %w", i, err)
				}
				item.AdvancedStashQuantity = byte(qty)
			}
		}

		if item.Location == locationSocketed {
			if len(itemList) == 0 {
				return itemList, fmt.Errorf("socketed item with no parent")
			}
			last := &itemList[len(itemList)-1]

			// Resolve socketed gem/rune properties.
			resolveSocketedProperties(&item, last)
			last.SocketedItems = append(last.SocketedItems, item)
		} else {
			if item.nrSocketedItems > 0 && !item.SimpleItem {
				toRead += int(item.nrSocketedItems)
			}
			itemList = append(itemList, item)
		}

		br.Align()
	}

	return itemList, nil
}

// parseSimpleBits reads the fixed item flags and location data (all items).
func parseSimpleBits(br *bitReader, version uint32) (Item, error) {
	var item Item
	isD2R := version >= d2rVersion

	// In LoD, each item starts with "JM" header (16 bits).
	// In D2R (>= 0x61), there is no per-item header.
	if !isD2R {
		j, err := br.ReadByte()
		if err != nil {
			return item, err
		}
		m, err := br.ReadByte()
		if err != nil {
			return item, err
		}
		if j != 'J' || m != 'M' {
			return item, fmt.Errorf("expected item header 'JM', got %q%q", string(j), string(m))
		}
	}

	// 4 unknown bits
	if _, err := br.ReadBits(4); err != nil {
		return item, err
	}

	v, err := br.ReadBits(1)
	if err != nil {
		return item, err
	}
	item.Identified = v == 1

	// 6 unknown
	if _, err := br.ReadBits(6); err != nil {
		return item, err
	}

	v, err = br.ReadBits(1)
	if err != nil {
		return item, err
	}
	item.Socketed = v == 1

	// 1 unknown
	if _, err := br.ReadBits(1); err != nil {
		return item, err
	}

	v, err = br.ReadBits(1)
	if err != nil {
		return item, err
	}
	item.New = v == 1

	// 2 unknown
	if _, err := br.ReadBits(2); err != nil {
		return item, err
	}

	v, err = br.ReadBits(1)
	if err != nil {
		return item, err
	}
	item.IsEar = v == 1

	v, err = br.ReadBits(1)
	if err != nil {
		return item, err
	}
	item.StarterItem = v == 1

	// 3 unknown
	if _, err := br.ReadBits(3); err != nil {
		return item, err
	}

	v, err = br.ReadBits(1)
	if err != nil {
		return item, err
	}
	item.SimpleItem = v == 1

	v, err = br.ReadBits(1)
	if err != nil {
		return item, err
	}
	item.Ethereal = v == 1

	// 1 unknown
	if _, err := br.ReadBits(1); err != nil {
		return item, err
	}

	v, err = br.ReadBits(1)
	if err != nil {
		return item, err
	}
	item.Personalized = v == 1

	// 1 unknown
	if _, err := br.ReadBits(1); err != nil {
		return item, err
	}

	v, err = br.ReadBits(1)
	if err != nil {
		return item, err
	}
	item.Runeword = v == 1

	// 5 unknown
	if _, err := br.ReadBits(5); err != nil {
		return item, err
	}

	// Item version — D2R uses 3 bits, LoD uses 10 bits (8 version + 2 padding).
	if isD2R {
		ver, err := br.ReadBits(3)
		if err != nil {
			return item, err
		}
		item.Version = uint(ver)
	} else {
		ver, err := br.ReadBits(8)
		if err != nil {
			return item, err
		}
		item.Version = uint(ver)
		// 2 padding bits (part of the 10-bit version field in LoD).
		if _, err := br.ReadBits(2); err != nil {
			return item, err
		}
	}

	loc, err := br.ReadBits(3)
	if err != nil {
		return item, err
	}
	item.Location = byte(loc)

	eq, err := br.ReadBits(4)
	if err != nil {
		return item, err
	}
	item.EquipSlot = byte(eq)

	px, err := br.ReadBits(4)
	if err != nil {
		return item, err
	}
	item.PositionX = byte(px)

	// position_y: 4 bits (d2rmm reference, not 3+1 like old parser).
	py, err := br.ReadBits(4)
	if err != nil {
		return item, err
	}
	item.PositionY = byte(py)

	// alt_position_id (page): 3 bits.
	pg, err := br.ReadBits(3)
	if err != nil {
		return item, err
	}
	item.Page = byte(pg)

	if !item.IsEar {
		// Item type code — D2R uses Huffman, LoD uses 4x8 bit ASCII.
		if isD2R {
			var code strings.Builder
			for range 4 {
				ch, err := itemCodeTree.decodeChar(br)
				if err != nil {
					return item, fmt.Errorf("huffman decode char: %w", err)
				}
				code.WriteByte(ch)
			}
			item.Code = strings.TrimRight(code.String(), " ")
		} else {
			var code strings.Builder
			for range 4 {
				ch, err := br.ReadBits(8)
				if err != nil {
					return item, err
				}
				code.WriteByte(byte(ch))
			}
			item.Code = strings.TrimRight(code.String(), " ")
		}

		// Resolve item type and name from code.
		typeID := getItemTypeID(item.Code)
		item.TypeID = typeIDString(typeID)
		item.TypeName = getItemTypeName(item.Code, typeID)

		// Weapon base damage.
		if typeID == itemTypeWeapon {
			if dmg, ok := weaponDamageMap[item.Code]; ok {
				d := dmg // copy
				if item.Ethereal {
					d.Min1H = uint(float64(d.Min1H) * 1.5)
					d.Max1H = uint(float64(d.Max1H) * 1.5)
					d.Min2H = uint(float64(d.Min2H) * 1.5)
					d.Max2H = uint(float64(d.Max2H) * 1.5)
				}
				item.BaseDamage = &d
			}
		}

		// Quest items: read quest_difficulty before socket count.
		if questItemMap[item.Code] {
			prop := magicalProperties[356]
			qd, err := br.ReadBits(prop.SaveBits)
			if err != nil {
				return item, fmt.Errorf("quest difficulty: %w", err)
			}
			item.QuestDifficulty = int(int64(qd) - prop.SaveAdd)
		}

		// Number of items socketed in this item.
		// Simple items use 1 bit, complex items use 3 bits.
		// Quest items force 1 bit.
		socketBits := uint(3)
		if item.SimpleItem || questItemMap[item.Code] {
			socketBits = 1
		}
		ns, err := br.ReadBits(socketBits)
		if err != nil {
			return item, err
		}
		item.nrSocketedItems = uint(ns)
	} else {
		// Ear data.
		earClass, err := br.ReadBits(3)
		if err != nil {
			return item, err
		}
		item.EarClass = byte(earClass)

		earLevel, err := br.ReadBits(7)
		if err != nil {
			return item, err
		}
		item.EarLevel = byte(earLevel)

		var name strings.Builder
		for {
			c, err := br.ReadBits(7)
			if err != nil {
				return item, err
			}
			if c == 0 {
				break
			}
			name.WriteByte(byte(c))
		}
		item.EarName = name.String()
		br.Align()
	}

	return item, nil
}

// parseExtendedBits reads quality, defense, durability, sockets, and properties.
func parseExtendedBits(br *bitReader, item *Item, version uint32, realm byte) error {
	var err error
	var v uint64
	isD2R := version >= d2rVersion
	_ = realm // available for future use

	// Item ID (32 bits)
	if v, err = br.ReadBits(32); err != nil {
		return err
	}
	item.ID = uint32(v)

	// Item level (7 bits)
	if v, err = br.ReadBits(7); err != nil {
		return err
	}
	item.ItemLevel = byte(v)

	// Quality (4 bits)
	if v, err = br.ReadBits(4); err != nil {
		return err
	}
	item.Quality = Quality(v)

	// Multiple pictures
	mp, err := br.ReadBits(1)
	if err != nil {
		return err
	}
	if mp == 1 {
		if _, err = br.ReadBits(3); err != nil {
			return err
		}
	}

	// Class specific
	cs, err := br.ReadBits(1)
	if err != nil {
		return err
	}
	if cs == 1 {
		if _, err = br.ReadBits(11); err != nil {
			return err
		}
	}

	// Quality-specific data.
	switch item.Quality {
	case QualityLow:
		if _, err = br.ReadBits(3); err != nil {
			return err
		}
	case QualityNormal:
		// Speculative 12-bit read: if all 0s or all 1s, consume them.
		// Otherwise, put them back (these are actually property bits).
		bits12, err := br.ReadBits(12)
		if err != nil {
			return err
		}
		if bits12 != 0 && bits12 != 0xFFF {
			br.UnreadBits(bits12, 12)
		}
	case QualityHigh:
		if _, err = br.ReadBits(3); err != nil {
			return err
		}
	case QualityMagical:
		if v, err = br.ReadBits(11); err != nil {
			return err
		}
		item.MagicPrefix = v
		if name, ok := magicalPrefixes[v]; ok {
			item.MagicPrefixName = name
		}
		if v, err = br.ReadBits(11); err != nil {
			return err
		}
		item.MagicSuffix = v
		if name, ok := magicalSuffixes[v]; ok {
			item.MagicSuffixName = name
		}
	case QualitySet:
		if v, err = br.ReadBits(12); err != nil {
			return err
		}
		item.SetID = v
		if name, ok := setNames[v]; ok {
			item.SetName = name
		}
	case QualityRare, QualityCrafted:
		if err := parseRareOrCraftedBits(br, item); err != nil {
			return err
		}
	case QualityUnique:
		if v, err = br.ReadBits(12); err != nil {
			return err
		}
		item.UniqueID = v
		if name, ok := uniqueNames[v]; ok {
			item.UniqueName = name
		}
	}

	// Runeword data
	if item.Runeword {
		rwID, err := br.ReadBits(12)
		if err != nil {
			return err
		}
		item.RunewordID = rwID
		if name, ok := runewordNames[rwID]; ok {
			item.RunewordName = name
		}
		// 4 unknown bits (usually 5)
		if _, err = br.ReadBits(4); err != nil {
			return err
		}
	}

	// Personalized name
	if item.Personalized {
		var name strings.Builder
		// D2R uses 8-bit chars, LoD uses 7-bit.
		charBits := uint(7)
		if isD2R {
			charBits = 8
		}
		for {
			c, err := br.ReadBits(charBits)
			if err != nil {
				return err
			}
			if c == 0 {
				break
			}
			name.WriteByte(byte(c))
		}
		item.PersonalizedName = name.String()
	}

	// Tome extra bits
	if tomeMap[item.Code] {
		if _, err = br.ReadBits(5); err != nil {
			return err
		}
	}

	// Timestamp bit (all items)
	if _, err = br.ReadBits(1); err != nil {
		return err
	}

	typeID := getItemTypeID(item.Code)

	// Defense (armor and shields only).
	// Read using property 31's bit width from ISC.
	if typeID == itemTypeArmor || typeID == itemTypeShield {
		prop := magicalProperties[31]
		def, err := br.ReadBits(prop.SaveBits)
		if err != nil {
			return err
		}
		item.Defense = int(int64(def) - prop.SaveAdd)
	}

	// Durability (armor, weapons, shields).
	// Max durability uses property 73, current uses property 72.
	if typeID == itemTypeArmor || typeID == itemTypeWeapon || typeID == itemTypeShield {
		propMax := magicalProperties[73]
		maxDur, err := br.ReadBits(propMax.SaveBits)
		if err != nil {
			return err
		}
		item.MaxDurability = uint(maxDur)

		if maxDur > 0 {
			propCur := magicalProperties[72]
			curDur, err := br.ReadBits(propCur.SaveBits)
			if err != nil {
				return err
			}
			item.CurDurability = uint(curDur)
		}
	}

	// Quantity (stackable items)
	if quantityMap[item.Code] {
		qty, err := br.ReadBits(9)
		if err != nil {
			return err
		}
		item.Quantity = uint(qty)
	}

	// v105: extra bit before socket count.
	if version >= versionD2Rv105 {
		if _, err = br.ReadBits(1); err != nil {
			return err
		}
	}

	// Total sockets
	if item.Socketed {
		ns, err := br.ReadBits(4)
		if err != nil {
			return err
		}
		item.TotalSockets = byte(ns)
	}

	// Set list: 5-bit bitmask, popcount determines bonus property lists.
	var plistFlag uint64
	if item.Quality == QualitySet {
		plistFlag, err = br.ReadBits(5)
		if err != nil {
			return err
		}
	}

	// Base magical properties
	item.MagicAttributes, err = parseMagicalList(br)
	if err != nil {
		return fmt.Errorf("magic attributes: %w", err)
	}

	// Set bonus property lists (popcount approach).
	// Each set bit in the 5-bit flag triggers one bonus list.
	if item.Quality == QualitySet {
		for plistFlag > 0 {
			if plistFlag&1 != 0 {
				setList, err := parseMagicalList(br)
				if err != nil {
					return fmt.Errorf("set attributes: %w", err)
				}
				item.SetAttributes = append(item.SetAttributes, setList)
			}
			plistFlag >>= 1
		}
	}

	// Runeword properties
	if item.Runeword {
		item.RunewordAttributes, err = parseMagicalList(br)
		if err != nil {
			return fmt.Errorf("runeword attributes: %w", err)
		}
	}

	return nil
}

// parseMagicalList reads 9-bit property IDs followed by their values,
// terminated by 0x1ff. Handles compound properties (NumProps),
// SaveParamBits, Encode-based splitting, and DescFunc=14 skilltab.
func parseMagicalList(br *bitReader) ([]MagicAttribute, error) {
	var attrs []MagicAttribute

	for {
		id, err := br.ReadBits(9)
		if err != nil {
			return attrs, err
		}
		if id == 0x1ff {
			break
		}

		prop, ok := magicalProperties[id]
		if !ok {
			return attrs, fmt.Errorf("unknown magical property: %d", id)
		}

		numProps := prop.NumProps
		if numProps == 0 {
			numProps = 1
		}

		var values []int64
		for i := range numProps {
			p, pOk := magicalProperties[id+uint64(i)]
			if !pOk {
				return attrs, fmt.Errorf("unknown compound property: %d (base %d, offset %d)", id+uint64(i), id, i)
			}

			// Read parameter bits if present.
			if p.SaveParamBits > 0 {
				param, err := br.ReadBits(p.SaveParamBits)
				if err != nil {
					return attrs, fmt.Errorf("property %d param: %w", id+uint64(i), err)
				}

				// DescFunc=14: skilltab encoding — low 3 bits are tab index.
				if p.DescFunc == 14 {
					values = append(values, int64(param&0x7))
					param = (param >> 3) & 0x1fff
				}

				// Encode=2 (chance-to-cast) or Encode=3 (charges):
				// low 6 bits are skill level, next 10 bits are skill id.
				if p.Encode == 2 || p.Encode == 3 {
					values = append(values, int64(param&0x3f)) // skill level
					param = (param >> 6) & 0x3ff               // skill id
				}

				values = append(values, int64(param))
			}

			// Read value bits.
			if p.SaveBits == 0 {
				return attrs, fmt.Errorf("property %d has 0 save bits", id+uint64(i))
			}
			val, err := br.ReadBits(p.SaveBits)
			if err != nil {
				return attrs, fmt.Errorf("property %d value: %w", id+uint64(i), err)
			}

			// Apply SaveAdd offset.
			adjusted := int64(val) - p.SaveAdd

			// Encode=3 (charges): split value into current (low 8) + max (high 8).
			if p.Encode == 3 {
				values = append(values, adjusted&0xff, (adjusted>>8)&0xff) // current + max charges
			} else {
				values = append(values, adjusted)
			}
		}

		attrs = append(attrs, MagicAttribute{
			ID:     id,
			Name:   prop.Name,
			Values: values,
		})
	}

	return attrs, nil
}

func parseRareOrCraftedBits(br *bitReader, item *Item) error {
	nameID1, err := br.ReadBits(8)
	if err != nil {
		return err
	}
	if name, ok := rareNames[nameID1]; ok {
		item.RareName = name
	}

	nameID2, err := br.ReadBits(8)
	if err != nil {
		return err
	}
	if name, ok := rareNames[nameID2]; ok {
		item.RareName2 = name
	}

	// 6 possible prefix/suffix pairs
	for range 6 {
		hasID, err := br.ReadBits(1)
		if err != nil {
			return err
		}
		if hasID == 1 {
			if _, err := br.ReadBits(11); err != nil {
				return err
			}
		}
	}

	return nil
}

func resolveSocketedProperties(gem *Item, parent *Item) {
	parentType := getItemTypeID(parent.Code)
	switch parentType {
	case itemTypeWeapon:
		if attrs, ok := socketablesWeapons[gem.Code]; ok {
			gem.MagicAttributes = attrs
		}
	case itemTypeArmor:
		if attrs, ok := socketablesArmor[gem.Code]; ok {
			gem.MagicAttributes = attrs
		}
	case itemTypeShield:
		if attrs, ok := socketablesShields[gem.Code]; ok {
			gem.MagicAttributes = attrs
		}
	}
}

func typeIDString(id uint64) string {
	switch id {
	case itemTypeArmor:
		return "armor"
	case itemTypeShield:
		return "shield"
	case itemTypeWeapon:
		return "weapon"
	default:
		return "misc"
	}
}
