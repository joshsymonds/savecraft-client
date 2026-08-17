package dict

import (
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
	want := map[uint16]string{187: "Base.Pan", 516: "Base.Tshirt_DefaultTEXTURE_TINT", 539: "Base.EmptyJar", 1914: "Base.Shorts_ShortDenim", 2958: "Base.Bandage_LeftHand", 3210: "Base.Bag_ALICEpack", 3651: "Base.Shotgun", 4439: "Base.Socks_Ankle", 4695: "Base.HuntingKnife", 4929: "Base.Shoes_BlueTrainers"}
	for id, n := range want {
		if got[id] != n {
			t.Errorf("%d=%q", id, got[id])
		}
	}
}
func TestParseItemsErrors(t *testing.T) {
	for _, s := range []string{`--[[ ---- ITEMS ---- --]] { registryID = 1, fulltype = "a", } { registryID = 1, fulltype = "b", }`, `--[[ ---- ITEMS ---- --]] { registryID = 1, }`, `--[[ ---- ITEMS ---- --]] { fulltype = "a", }`} {
		if _, e := ParseItems([]byte(s)); e == nil || !IsMalformed(e) {
			t.Errorf("error=%v", e)
		}
	}
}
func IsMalformed(e error) bool {
	return e != nil && (e == ErrMalformed || len(e.Error()) > len(ErrMalformed.Error()) && e.Error()[:len(ErrMalformed.Error())] == ErrMalformed.Error())
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
	m, e = ParseMods([]byte("mods\n{\n  \"one\",\n two\n}\n"))
	if e != nil || len(m) != 2 || m[0] != "one" || m[1] != "two" {
		t.Fatalf("%v %v", m, e)
	}
}
