package dict

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var ErrMalformed = errors.New("malformed WorldDictionaryReadable.lua")
var itemRe = regexp.MustCompile(`(?s)\{(.*?)\}`)
var idRe = regexp.MustCompile(`registryID\s*=\s*([0-9]+)\s*,?`)
var typeRe = regexp.MustCompile(`fulltype\s*=\s*"([^"]+)"\s*,?`)

func ParseItems(src []byte) (map[uint16]string, error) {
	s := string(src)
	start := strings.Index(s, "--[[ ---- ITEMS ---- --]]")
	if start < 0 {
		return nil, fmt.Errorf("%w: missing ITEMS section", ErrMalformed)
	}
	rest := s[start+len("--[[ ---- ITEMS ---- --]]"):]
	if end := strings.Index(rest, "--[[ ----"); end >= 0 {
		rest = rest[:end]
	}
	out := map[uint16]string{}
	ordinal := 0
	for _, m := range itemRe.FindAllStringSubmatchIndex(rest, -1) {
		ordinal++
		body := rest[m[2]:m[3]]
		idm := idRe.FindStringSubmatch(body)
		tm := typeRe.FindStringSubmatch(body)
		if idm == nil || tm == nil {
			return nil, fmt.Errorf("%w: record %d", ErrMalformed, ordinal)
		}
		n, e := strconv.ParseUint(idm[1], 10, 16)
		if e != nil {
			return nil, fmt.Errorf("%w: record %d registryID", ErrMalformed, ordinal)
		}
		id := uint16(n)
		if _, ok := out[id]; ok {
			return nil, fmt.Errorf("%w: duplicate registryID %d in record %d", ErrMalformed, id, ordinal)
		}
		out[id] = tm[1]
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: no records", ErrMalformed)
	}
	return out, nil
}

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
	return out, nil
}
