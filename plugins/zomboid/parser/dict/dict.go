// Package dict reads the plain-text sidecars of a Project Zomboid save:
// WorldDictionaryReadable.lua, which maps the registry ids stored in a
// character blob to item fulltypes, and mods.txt, which lists the save's
// active mods.
package dict

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// ErrMalformed is returned, wrapped with the position or record that failed,
// whenever the dictionary does not have the shape the game writes.
var ErrMalformed = errors.New("malformed WorldDictionaryReadable.lua")

const (
	// itemsMarker opens the ITEMS section; sectionMarker opens any section, so
	// it also ends ITEMS.
	itemsMarker   = "--[[ ---- ITEMS ---- --]]"
	sectionMarker = "--[[ ----"
	// itemsAssignment is the Lua assignment the ITEMS section's record table is
	// bound to, once surrounding whitespace is trimmed.
	itemsAssignment = "items ="
	// recordSeparators are the bytes that may sit between two records.
	recordSeparators = " \t\r\n,"
)

var idRe = regexp.MustCompile(`registryID\s*=\s*([0-9]+)\s*,?`)
var typeRe = regexp.MustCompile(`fulltype\s*=\s*"([^"]+)"\s*,?`)

// ParseItems maps every registry id in the ITEMS section to its fulltype. The
// section is read as a sequence of records that must account for all of it: an
// unclosed record, an unclosed table, or any other text between the records is
// a malformed dictionary rather than something to skip over, because a record
// the scanner walks past silently drops the item it names.
func ParseItems(src []byte) (map[uint16]string, error) {
	records, base, err := itemsTable(string(src))
	if err != nil {
		return nil, err
	}
	items := map[uint16]string{}
	ordinal := 0
	for pos := 0; ; {
		pos = skipSeparators(records, pos)
		if pos == len(records) {
			return nil, fmt.Errorf("%w: unclosed ITEMS table at byte %d", ErrMalformed, base-1)
		}
		if records[pos] == '}' {
			if strings.TrimSpace(records[pos+1:]) != "" {
				return nil, fmt.Errorf("%w: trailing text after the ITEMS table at byte %d", ErrMalformed, base+pos+1)
			}
			break
		}
		if records[pos] != '{' {
			return nil, fmt.Errorf("%w: unexpected text at byte %d", ErrMalformed, base+pos)
		}
		end := strings.IndexByte(records[pos:], '}')
		if end < 0 {
			return nil, fmt.Errorf("%w: unclosed record at byte %d", ErrMalformed, base+pos)
		}
		end += pos
		ordinal++
		id, fulltype, recordErr := parseItemRecord(records[pos+1:end], ordinal)
		if recordErr != nil {
			return nil, recordErr
		}
		if _, duplicate := items[id]; duplicate {
			return nil, fmt.Errorf("%w: duplicate registryID %d in record %d", ErrMalformed, id, ordinal)
		}
		items[id] = fulltype
		pos = end + 1
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("%w: no records", ErrMalformed)
	}
	return items, nil
}

// itemsTable locates the ITEMS section and strips its `items = {` header,
// returning everything after the opening brace — the records and the brace that
// closes them — along with the byte offset that text starts at in source.
func itemsTable(source string) (string, int, error) {
	start := strings.Index(source, itemsMarker)
	if start < 0 {
		return "", 0, fmt.Errorf("%w: missing ITEMS section", ErrMalformed)
	}
	base := start + len(itemsMarker)
	section := source[base:]
	if end := strings.Index(section, sectionMarker); end >= 0 {
		section = section[:end]
	}
	open := strings.IndexByte(section, '{')
	if open < 0 || strings.TrimSpace(section[:open]) != itemsAssignment {
		return "", 0, fmt.Errorf("%w: ITEMS section does not open with %q at byte %d",
			ErrMalformed, itemsAssignment+" {", base)
	}
	return section[open+1:], base + open + 1, nil
}

// parseItemRecord reads the registryID and fulltype out of one record body; the
// other keys the game writes are not part of the character sheet.
func parseItemRecord(body string, ordinal int) (uint16, string, error) {
	id := idRe.FindStringSubmatch(body)
	fulltype := typeRe.FindStringSubmatch(body)
	if id == nil || fulltype == nil {
		return 0, "", fmt.Errorf("%w: record %d has no registryID or fulltype", ErrMalformed, ordinal)
	}
	value, err := strconv.ParseUint(id[1], 10, 16)
	if err != nil {
		return 0, "", fmt.Errorf("%w: record %d registryID %q", ErrMalformed, ordinal, id[1])
	}
	return uint16(value), fulltype[1], nil
}

// skipSeparators advances past the whitespace and commas between two records.
func skipSeparators(section string, pos int) int {
	for pos < len(section) && strings.IndexByte(recordSeparators, section[pos]) >= 0 {
		pos++
	}
	return pos
}

// ParseMods returns the save's active mod ids, sorted, so that the emitted list
// does not depend on the order the game happened to write them in.
func ParseMods(src []byte) ([]string, error) {
	s := string(src)
	i := strings.Index(s, "mods")
	if i < 0 {
		return nil, fmt.Errorf("%w: missing mods block", ErrMalformed)
	}
	rest := s[i+4:]
	a := strings.Index(rest, "{")
	if a < 0 {
		return nil, fmt.Errorf("%w: missing mods block", ErrMalformed)
	}
	rest = rest[a+1:]
	b := strings.Index(rest, "}")
	if b < 0 {
		return nil, fmt.Errorf("%w: missing mods block", ErrMalformed)
	}
	var out []string
	for _, line := range strings.Split(rest[:b], "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == "{" || line == "}" {
			continue
		}
		line = strings.TrimSuffix(line, ",")
		line = strings.Trim(line, "\" ")
		if line != "" {
			out = append(out, line)
		}
	}
	sort.Strings(out)
	return out, nil
}
