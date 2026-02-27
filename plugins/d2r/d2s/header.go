package d2s

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
)

// Version constants for format branching.
const (
	versionD2Rv105 = 0x69 // D2R RotW expansion
)

// Realm values (v105+).
const (
	RealmClassic byte = 1
	RealmLoD     byte = 2
	RealmRotW    byte = 3
)

// headerReader wraps an io.Reader with error-accumulating helpers
// for sequential binary reads. After the first error, all subsequent
// reads return zero values and the error is preserved.
type headerReader struct {
	r   io.Reader
	err error
}

func (hr *headerReader) u32() uint32 {
	if hr.err != nil {
		return 0
	}
	var buf [4]byte
	_, hr.err = io.ReadFull(hr.r, buf[:])
	return binary.LittleEndian.Uint32(buf[:])
}

func (hr *headerReader) u16() uint16 {
	if hr.err != nil {
		return 0
	}
	var buf [2]byte
	_, hr.err = io.ReadFull(hr.r, buf[:])
	return binary.LittleEndian.Uint16(buf[:])
}

func (hr *headerReader) u8() byte {
	if hr.err != nil {
		return 0
	}
	var buf [1]byte
	_, hr.err = io.ReadFull(hr.r, buf[:])
	return buf[0]
}

func (hr *headerReader) bytes(n int) []byte {
	if hr.err != nil {
		return nil
	}
	buf := make([]byte, n)
	_, hr.err = io.ReadFull(hr.r, buf)
	return buf
}

func (hr *headerReader) skip(n int) {
	if hr.err != nil {
		return
	}
	_, hr.err = io.ReadFull(hr.r, make([]byte, n))
}

func nullTermString(b []byte) string {
	i := bytes.IndexByte(b, 0)
	if i < 0 {
		i = len(b)
	}
	return string(b[:i])
}

// parseHeader reads the D2S header sequentially with version branching.
// Returns the file version, parsed header, and any error.
//
// Supports:
//   - LoD (version <= 0x60): 767 bytes through "gf"
//   - D2R v105 (version 0x69): 835 bytes through "gf"
func parseHeader(r io.Reader) (uint32, Header, error) {
	hr := &headerReader{r: r}

	// Preamble (same for all versions): signature, version, file size, checksum, active weapon.
	sig := hr.u32()
	version := hr.u32()
	_ = hr.u32() // file size
	_ = hr.u32() // checksum
	_ = hr.u32() // active weapon

	if hr.err != nil {
		return 0, Header{}, fmt.Errorf("read header preamble: %w", hr.err)
	}
	if sig != 0xAA55AA55 {
		return 0, Header{}, fmt.Errorf("invalid signature: %08X", sig)
	}

	// Name position depends on version:
	//   LoD (<= 0x60): 16 bytes at offset 0x14 (before Status)
	//   D2R v105 (>= 0x69): name is later, after realm data
	var name string
	if version < versionD2Rv105 {
		nameBytes := hr.bytes(16)
		if hr.err != nil {
			return 0, Header{}, fmt.Errorf("read name: %w", hr.err)
		}
		name = nullTermString(nameBytes)
	}

	// Status through assigned skills — same field order for all versions,
	// but at different offsets due to name position above.
	status := hr.u8()
	_ = hr.u8() // progression
	hr.skip(2)  // unknown (active_arms)
	class := hr.u8()
	hr.skip(2) // unknown
	level := hr.u8()
	hr.skip(4) // created timestamp
	lastPlayed := hr.u32()
	hr.skip(4) // unknown (0xFF * 4)

	if hr.err != nil {
		return 0, Header{}, fmt.Errorf("read character fields: %w", hr.err)
	}

	var assignedSkills [16]uint32
	for i := range assignedSkills {
		assignedSkills[i] = hr.u32()
	}
	leftSkill := hr.u32()
	rightSkill := hr.u32()
	leftSwapSkill := hr.u32()
	rightSwapSkill := hr.u32()

	if hr.err != nil {
		return 0, Header{}, fmt.Errorf("read skills: %w", hr.err)
	}

	// Menu appearance (32 bytes: 16 graphics + 16 tints).
	hr.skip(32)

	// Difficulty (3 bytes: Normal, Nightmare, Hell).
	diffBytes := hr.bytes(3)

	// Map ID.
	mapID := hr.u32()
	hr.skip(2) // unknown

	if hr.err != nil {
		return 0, Header{}, fmt.Errorf("read difficulty/map: %w", hr.err)
	}

	// Mercenary data.
	deadMerc := hr.u16()
	mercID := hr.u32()
	mercNameID := hr.u16()
	mercType := hr.u16()
	mercExp := hr.u32()

	if hr.err != nil {
		return 0, Header{}, fmt.Errorf("read mercenary: %w", hr.err)
	}

	// Version-specific middle section between merc data and quest data.
	var realm byte
	if version >= versionD2Rv105 {
		// v105: 73 unknown bytes, realm byte, 50 unknown bytes,
		// name (16), 4 unknown bytes, 84 bytes extended header.
		hr.skip(73)
		realm = hr.u8()
		hr.skip(50)

		nameBytes := hr.bytes(16)
		if hr.err != nil {
			return 0, Header{}, fmt.Errorf("read v105 name: %w", hr.err)
		}
		name = nullTermString(nameBytes)

		hr.skip(4)  // unknown after name
		hr.skip(84) // extended header data
	} else {
		// LoD: 144 unknown bytes fill the gap.
		hr.skip(144)

		// Infer realm from status flags.
		if status&0x20 != 0 {
			realm = RealmLoD
		} else {
			realm = RealmClassic
		}
	}

	if hr.err != nil {
		return 0, Header{}, fmt.Errorf("read version-specific section: %w", hr.err)
	}

	// --- Quest section ---
	// "Woo!" (4) + version (4) + length (2) + 3 * 96 quest bytes.
	questMagic := hr.bytes(4)
	if hr.err != nil {
		return 0, Header{}, fmt.Errorf("read quest header: %w", hr.err)
	}
	if string(questMagic) != "Woo!" {
		return 0, Header{}, fmt.Errorf("expected quest header 'Woo!', got %q", string(questMagic))
	}
	hr.skip(6) // quest version (4) + length (2)
	questNormal := hr.bytes(96)
	questNM := hr.bytes(96)
	questHell := hr.bytes(96)

	if hr.err != nil {
		return 0, Header{}, fmt.Errorf("read quest data: %w", hr.err)
	}

	// --- Waypoint section ---
	// "WS" (2) + version (4) + length (2) + 3 * 24 waypoint bytes.
	wpMagic := hr.bytes(2)
	if hr.err != nil {
		return 0, Header{}, fmt.Errorf("read waypoint header: %w", hr.err)
	}
	if string(wpMagic) != "WS" {
		return 0, Header{}, fmt.Errorf("expected waypoint header 'WS', got %q", string(wpMagic))
	}
	hr.skip(6) // waypoint version (4) + length (2)
	wpNormal := hr.bytes(24)
	wpNM := hr.bytes(24)
	wpHell := hr.bytes(24)

	if hr.err != nil {
		return 0, Header{}, fmt.Errorf("read waypoint data: %w", hr.err)
	}

	// --- NPC section ---
	// magic (2) + length (2) + data (48).
	npcMagic := hr.bytes(2)
	if hr.err != nil {
		return 0, Header{}, fmt.Errorf("read npc header: %w", hr.err)
	}
	if npcMagic[0] != 0x01 || npcMagic[1] != 0x77 {
		return 0, Header{}, fmt.Errorf("expected NPC header [0x01 0x77], got [%02X %02X]", npcMagic[0], npcMagic[1])
	}
	hr.skip(2) // NPC length
	npcData := hr.bytes(48)

	if hr.err != nil {
		return 0, Header{}, fmt.Errorf("read npc data: %w", hr.err)
	}

	// --- Stat header ---
	statMagic := hr.bytes(2)
	if hr.err != nil {
		return 0, Header{}, fmt.Errorf("read stat header: %w", hr.err)
	}
	if string(statMagic) != "gf" {
		return 0, Header{}, fmt.Errorf("expected stat header 'gf', got %q", string(statMagic))
	}

	// Build parsed header.
	h := Header{
		Name:  name,
		Class: Class(class),
		Level: level,
		Realm: realm,
		Status: Status{
			Hardcore:  status&0x04 != 0,
			Died:      status&0x08 != 0,
			Expansion: status&0x20 != 0,
			Ladder:    status&0x40 != 0,
		},
		LastPlayed:     lastPlayed,
		AssignedSkills: assignedSkills,
		LeftSkill:      leftSkill,
		RightSkill:     rightSkill,
		LeftSwapSkill:  leftSwapSkill,
		RightSwapSkill: rightSwapSkill,
		MapID:          mapID,
		Mercenary: Mercenary{
			Dead:       deadMerc != 0,
			ID:         mercID,
			NameID:     mercNameID,
			Type:       mercType,
			Experience: mercExp,
		},
	}

	// Parse current difficulty.
	for i, b := range diffBytes {
		if b&0x80 != 0 {
			h.CurrentDifficulty = Difficulty{
				Active:     true,
				Difficulty: DiffLevel(i),
				Act:        b & 0x07,
			}
		}
	}

	// Combine quest/waypoint/NPC data.
	h.QuestData = append([]byte{}, questNormal...)
	h.QuestData = append(h.QuestData, questNM...)
	h.QuestData = append(h.QuestData, questHell...)

	h.WaypointData = append([]byte{}, wpNormal...)
	h.WaypointData = append(h.WaypointData, wpNM...)
	h.WaypointData = append(h.WaypointData, wpHell...)

	h.NPCData = append([]byte{}, npcData...)

	return version, h, nil
}
