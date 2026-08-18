package dict

import (
	"errors"
	"os"
	"testing"
)

func TestParseFixture(t *testing.T) {
	b, e := os.ReadFile("../testdata/tutorial-42.20.2/WorldDictionaryReadable.lua")
	if e != nil {
		t.Fatal(e)
	}
	got, e := ParseItems(b)
	if e != nil {
		t.Fatal(e)
	}
	if len(got) != 5092 {
		t.Fatalf("entries=%d", len(got))
	}
	want := map[uint16]string{
		187:  "Base.Pan",
		516:  "Base.Tshirt_DefaultTEXTURE_TINT",
		539:  "Base.EmptyJar",
		1914: "Base.Shorts_ShortDenim",
		2958: "Base.Bandage_LeftHand",
		3210: "Base.Bag_ALICEpack",
		3651: "Base.Shotgun",
		4439: "Base.Socks_Ankle",
		4695: "Base.HuntingKnife",
		4929: "Base.Shoes_BlueTrainers",
	}
	for id, n := range want {
		if got[id] != n {
			t.Errorf("%d=%q", id, got[id])
		}
	}
}

// TestParseItemsErrors pins that the whole ITEMS section is consumed: text the
// record scanner cannot account for is corruption, not something to skip.
func TestParseItemsErrors(t *testing.T) {
	const marker = "--[[ ---- ITEMS ---- --]]\n"
	for name, source := range map[string]string{
		"missing section":     `items = { { registryID = 1, fulltype = "a", } }`,
		"duplicate id":        marker + `items = { { registryID = 1, fulltype = "a", }, { registryID = 1, fulltype = "b", } }`,
		"no fulltype":         marker + `items = { { registryID = 1, } }`,
		"no registry id":      marker + `items = { { fulltype = "a", } }`,
		"registry id too big": marker + `items = { { registryID = 65536, fulltype = "a", } }`,
		"no records":          marker + `items = { }`,
		"no table":            marker + `items = 3`,
		"unexpected header":   marker + `entities = { { registryID = 1, fulltype = "a", } }`,
		"unclosed record":     marker + `items = { { registryID = 1, fulltype = "a", }, { registryID = 2, `,
		"unclosed table":      marker + `items = { { registryID = 1, fulltype = "a", },`,
		"text between":        marker + `items = { { registryID = 1, fulltype = "a", } oops { registryID = 2, fulltype = "b", } }`,
		"trailing garbage":    marker + `items = { { registryID = 1, fulltype = "a", } } oops`,
	} {
		t.Run(name, func(t *testing.T) {
			got, err := ParseItems([]byte(source))
			if !errors.Is(err, ErrMalformed) {
				t.Fatalf("items=%v, err=%v, want ErrMalformed", got, err)
			}
		})
	}
}

// TestParseItemsAcceptsTheSectionShape covers the section shapes that are
// legal: a record list closed by the table brace, with the next section marker
// or end of file after it.
func TestParseItemsAcceptsTheSectionShape(t *testing.T) {
	const records = `items = {
	{
		registryID = 1,
		fulltype = "Base.Pan",
	},
	{
		registryID = 2,
		fulltype = "Base.EmptyJar",
	},
}
`
	want := map[uint16]string{1: "Base.Pan", 2: "Base.EmptyJar"}
	for name, source := range map[string]string{
		"end of file": "--[[ ---- ITEMS ---- --]]\n" + records,
		"before entities": "--[[ ---- ITEMS ---- --]]\n" + records +
			"\n--[[ ---- ENTITIES ---- --]]\nentities = {\n\t{ registryID = 9, fulltype = \"x\", },\n}\n",
	} {
		t.Run(name, func(t *testing.T) {
			got, err := ParseItems([]byte(source))
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(want) {
				t.Fatalf("items=%v", got)
			}
			for id, fulltype := range want {
				if got[id] != fulltype {
					t.Errorf("%d=%q, want %q", id, got[id], fulltype)
				}
			}
		})
	}
}

func TestParseMods(t *testing.T) {
	b, e := os.ReadFile("../testdata/tutorial-42.20.2/mods.txt")
	if e != nil {
		t.Fatal(e)
	}
	m, e := ParseMods(b)
	if e != nil || len(m) != 0 {
		t.Fatalf("%v %v", m, e)
	}
	m, e = ParseMods([]byte("mods\n{\n  \"zebra\",\n alpha\n  \"Mango\",\n}\n"))
	if e != nil || len(m) != 3 || m[0] != "Mango" || m[1] != "alpha" || m[2] != "zebra" {
		t.Fatalf("%v %v", m, e)
	}
}
