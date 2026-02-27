package d2s

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
)

// Parse reads a D2S save file from the given byte slice and returns the parsed data.
// Supports both LoD (version <= 0x60) and D2R (version >= 0x61) formats.
func Parse(data []byte) (*D2S, error) {
	r := bufio.NewReader(bytes.NewReader(data))
	return parse(r)
}

func parse(r *bufio.Reader) (*D2S, error) {
	d := &D2S{}

	version, header, err := parseHeader(r)
	if err != nil {
		return nil, fmt.Errorf("header: %w", err)
	}
	d.Version = version
	d.Header = header

	// Attributes (bit-packed, follows the "gf" stat header).
	br := newBitReader(r)
	d.Attributes, err = parseAttributes(br)
	if err != nil {
		return nil, fmt.Errorf("attributes [%s]: %w", header.Name, err)
	}

	// Skills (32 bytes: "if" + 30 skill bytes).
	d.Skills, err = parseSkills(r, header.Class)
	if err != nil {
		return nil, fmt.Errorf("skills [%s]: %w", header.Name, err)
	}

	// Character items.
	d.Items, err = parseItemList(r, version, header.Realm)
	if err != nil {
		// Return partial results — remaining sections can't be located
		// when item parsing fails mid-bitstream.
		return d, fmt.Errorf("items [%s]: %w", header.Name, err)
	}

	// Corpse section.
	if err := parseCorpse(r, d); err != nil {
		return d, fmt.Errorf("corpse [%s]: %w", header.Name, err)
	}

	// Mercenary items. The LoD status bit is not set in v105+ saves;
	// use realm instead as the authoritative expansion indicator.
	isExpansion := header.Status.Expansion || header.Realm >= RealmLoD
	if isExpansion {
		if err := parseMercItems(r, d); err != nil {
			return d, fmt.Errorf("merc items [%s]: %w", header.Name, err)
		}
	}

	// Iron Golem (Necromancer expansion only).
	if header.Class == Necromancer && isExpansion {
		if err := parseIronGolem(r, d); err != nil {
			return d, fmt.Errorf("iron golem [%s]: %w", header.Name, err)
		}
	}

	return d, nil
}

func parseCorpse(r *bufio.Reader, d *D2S) error {
	buf := make([]byte, 4)
	if _, err := io.ReadFull(r, buf); err != nil {
		return fmt.Errorf("read corpse header: %w", err)
	}

	if string(buf[0:2]) != "JM" {
		return fmt.Errorf("expected corpse header 'JM', got %q", string(buf[0:2]))
	}

	count := uint16(buf[2]) | uint16(buf[3])<<8
	if count > 0 {
		// 12 unknown bytes of corpse data.
		skip := make([]byte, 12)
		if _, err := io.ReadFull(r, skip); err != nil {
			return fmt.Errorf("read corpse data: %w", err)
		}

		var err error
		d.CorpseItems, err = parseItemList(r, d.Version, d.Header.Realm)
		if err != nil {
			return fmt.Errorf("corpse items: %w", err)
		}
	}

	return nil
}

func parseMercItems(r *bufio.Reader, d *D2S) error {
	// 2-byte "jf" header.
	buf := make([]byte, 2)
	if _, err := io.ReadFull(r, buf); err != nil {
		return fmt.Errorf("read merc header: %w", err)
	}

	if string(buf) != "jf" {
		return fmt.Errorf("expected merc header 'jf', got %q", string(buf))
	}

	// Only parse merc items if a mercenary is hired.
	if d.Header.Mercenary.ID != 0 {
		var err error
		d.MercItems, err = parseItemList(r, d.Version, d.Header.Realm)
		if err != nil {
			return fmt.Errorf("merc items: %w", err)
		}
	}

	return nil
}

func parseIronGolem(r *bufio.Reader, d *D2S) error {
	buf := make([]byte, 3)
	if _, err := io.ReadFull(r, buf); err != nil {
		return fmt.Errorf("read golem header: %w", err)
	}

	if string(buf[0:2]) != "kf" {
		return fmt.Errorf("expected golem header 'kf', got %q", string(buf[0:2]))
	}

	hasGolem := buf[2]
	if hasGolem == 1 {
		items, err := parseItems(r, 1, d.Version, d.Header.Realm)
		if err != nil {
			return fmt.Errorf("golem item: %w", err)
		}
		if len(items) > 0 {
			d.GolemItem = &items[0]
		}
	}

	return nil
}
