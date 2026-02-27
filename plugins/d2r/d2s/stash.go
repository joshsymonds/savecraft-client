package d2s

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// SharedStash represents a parsed .d2i shared stash file.
type SharedStash struct {
	Version uint32
	Kind    uint32 // 0=hardcore, 1=softcore, 2=RotW softcore
	Gold    uint32 // Shared gold (from first section)
	Tabs    []StashTab
}

// StashTab represents one tab/page in the shared stash.
type StashTab struct {
	Type  byte // 0=normal, 1=advanced stash (RotW), 2=metadata (RotW)
	Items []Item
}

const stashHeaderSize = 64

// stashMagic identifies a .d2i section header.
const stashMagic = 0xAA55AA55

type rawStashHeader struct {
	Magic   uint32
	Kind    uint32
	Version uint32
	Gold    uint32
	Size    uint32
	Type    byte
	_       [43]byte // padding
}

// ParseStash parses a D2R shared stash (.d2i) file into structured data.
func ParseStash(data []byte) (*SharedStash, error) {
	if len(data) < stashHeaderSize {
		return nil, fmt.Errorf("stash file too small: %d bytes", len(data))
	}

	stash := &SharedStash{}
	pos := 0

	for pos < len(data) {
		if pos+stashHeaderSize > len(data) {
			break
		}

		var hdr rawStashHeader
		if err := binary.Read(bytes.NewReader(data[pos:pos+stashHeaderSize]), binary.LittleEndian, &hdr); err != nil {
			return nil, fmt.Errorf("read stash header at 0x%x: %w", pos, err)
		}

		if hdr.Magic != stashMagic {
			return nil, fmt.Errorf("bad stash magic at 0x%x: 0x%08x", pos, hdr.Magic)
		}

		// Capture version/kind/gold from first section.
		if pos == 0 {
			stash.Version = hdr.Version
			stash.Kind = hdr.Kind
			stash.Gold = hdr.Gold
		}

		// Section data starts after the 64-byte header.
		itemStart := pos + stashHeaderSize
		sectionEnd := pos + int(hdr.Size)
		if sectionEnd > len(data) {
			sectionEnd = len(data)
		}

		tab := StashTab{Type: hdr.Type}

		// Type 2 = RotW metadata section (not items).
		if hdr.Type == 2 {
			stash.Tabs = append(stash.Tabs, tab)
			pos = sectionEnd
			continue
		}

		// Parse items from this section.
		itemData := data[itemStart:sectionEnd]
		if len(itemData) >= 4 {
			realm := realmFromKind(hdr.Kind, hdr.Version)
			reader := bytes.NewReader(itemData)
			items, err := parseItemList(reader, hdr.Version, realm)
			if err != nil {
				return stash, fmt.Errorf("stash tab %d items: %w", len(stash.Tabs), err)
			}
			tab.Items = items
		}

		stash.Tabs = append(stash.Tabs, tab)
		pos = sectionEnd
	}

	return stash, nil
}

// realmFromKind infers the realm byte from the stash kind and version.
func realmFromKind(kind, version uint32) byte {
	if version >= 0x69 || kind == 2 {
		return RealmRotW
	}
	if version >= d2rVersion {
		return RealmLoD
	}
	return RealmClassic
}

// IsStash returns true if data looks like a .d2i file rather than a .d2s file.
// .d2i: offset 4 = kind (0-2). .d2s: offset 4 = version (>= 0x60).
func IsStash(data []byte) bool {
	if len(data) < 8 {
		return false
	}
	magic := binary.LittleEndian.Uint32(data[0:4])
	if magic != stashMagic {
		return false
	}
	field4 := binary.LittleEndian.Uint32(data[4:8])
	return field4 <= 2
}
