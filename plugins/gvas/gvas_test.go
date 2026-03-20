package gvas

import (
	"bytes"
	"math"
	"os"
	"testing"
)

func loadTestSave(t *testing.T) *Save {
	t.Helper()
	data, err := os.ReadFile("testdata/EXPEDITION_0.sav")
	if err != nil {
		t.Fatalf("read test file: %v", err)
	}
	save, err := ParseBytes(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return save
}

func TestParseSucceeds(t *testing.T) {
	loadTestSave(t)
}

func TestHeader(t *testing.T) {
	save := loadTestSave(t)
	h := save.Header

	tests := []struct {
		name string
		got  any
		want any
	}{
		{"Magic", h.Magic, uint32(0x53415647)},
		{"SaveGameVersion", h.SaveGameVersion, uint32(3)},
		{"PackageVersionUE4", h.PackageVersionUE4, uint32(522)},
		{"PackageVersionUE5", h.PackageVersionUE5, uint32(1012)},
		{"EngineVersionMajor", h.EngineVersionMajor, uint16(5)},
		{"EngineVersionMinor", h.EngineVersionMinor, uint16(4)},
		{"EngineVersionPatch", h.EngineVersionPatch, uint16(4)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("got %v, want %v", tt.got, tt.want)
			}
		})
	}
}

func TestSaveGameType(t *testing.T) {
	save := loadTestSave(t)
	want := "BP_SaveGameObject_V5"
	if !bytes.Contains([]byte(save.SaveGameType), []byte(want)) {
		t.Errorf("SaveGameType = %q, want to contain %q", save.SaveGameType, want)
	}
}

func TestRootPropertyCount(t *testing.T) {
	save := loadTestSave(t)
	if got := len(save.Properties); got != 40 {
		t.Errorf("root property count = %d, want 40", got)
	}
}

func TestGold(t *testing.T) {
	save := loadTestSave(t)
	gold := save.Properties.GetInt("Gold")
	if gold != 1225269 {
		t.Errorf("Gold = %d, want 1225269", gold)
	}
}

func TestTimePlayed(t *testing.T) {
	save := loadTestSave(t)
	tp := save.Properties.GetFloat64("TimePlayed")
	if math.Abs(tp-205446.09) > 1.0 {
		t.Errorf("TimePlayed = %f, want ~205446.09", tp)
	}
}

func TestFinishedGameCount(t *testing.T) {
	save := loadTestSave(t)
	fgc := save.Properties.GetInt("FinishedGameCount")
	if fgc != 1 {
		t.Errorf("FinishedGameCount = %d, want 1", fgc)
	}
}

func TestCharactersCollectionKeys(t *testing.T) {
	save := loadTestSave(t)
	entries := save.Properties.GetMap("CharactersCollection")
	if entries == nil {
		t.Fatal("CharactersCollection not found")
	}
	if len(entries) != 5 {
		t.Fatalf("CharactersCollection has %d entries, want 5", len(entries))
	}

	wantKeys := []string{"Lune", "Maelle", "Sciel", "Monoco", "Verso"}
	gotKeys := make(map[string]bool)
	for _, e := range entries {
		k := mapKeyString(e.Key)
		gotKeys[k] = true
	}
	for _, wk := range wantKeys {
		if !gotKeys[wk] {
			t.Errorf("missing key %q in CharactersCollection", wk)
		}
	}
}

func TestLuneCurrentLevel(t *testing.T) {
	save := loadTestSave(t)
	entries := save.Properties.GetMap("CharactersCollection")
	if entries == nil {
		t.Fatal("CharactersCollection not found")
	}

	lune := FindMapEntry(entries, "Lune")
	if lune == nil {
		t.Fatal("Lune not found in CharactersCollection")
	}

	sv, ok := lune.Value.(StructValue)
	if !ok {
		t.Fatalf("Lune value is %T, want StructValue", lune.Value)
	}

	lvl := sv.Properties.GetIntPrefix("CurrentLevel")
	if lvl != 80 {
		t.Errorf("Lune CurrentLevel = %d, want 80", lvl)
	}
}

func TestLuneEquippedSkills(t *testing.T) {
	save := loadTestSave(t)
	entries := save.Properties.GetMap("CharactersCollection")
	if entries == nil {
		t.Fatal("CharactersCollection not found")
	}

	lune := FindMapEntry(entries, "Lune")
	if lune == nil {
		t.Fatal("Lune not found in CharactersCollection")
	}

	sv, ok := lune.Value.(StructValue)
	if !ok {
		t.Fatalf("Lune value is %T, want StructValue", lune.Value)
	}

	skillsProp := sv.Properties.GetPrefix("EquippedSkills")
	if skillsProp == nil {
		t.Fatal("EquippedSkills not found on Lune")
	}

	arr, ok := skillsProp.Value.(ArrayValue)
	if !ok {
		t.Fatalf("EquippedSkills is %T, want ArrayValue", skillsProp.Value)
	}

	if len(arr.Elements) != 6 {
		t.Errorf("Lune EquippedSkills length = %d, want 6", len(arr.Elements))
	}
}

func TestCurrentParty(t *testing.T) {
	save := loadTestSave(t)
	arr := save.Properties.GetArray("CurrentParty")
	if arr == nil {
		t.Fatal("CurrentParty not found")
	}
	if len(arr) != 3 {
		t.Errorf("CurrentParty length = %d, want 3", len(arr))
	}
}

func TestWeaponProgressions(t *testing.T) {
	save := loadTestSave(t)
	arr := save.Properties.GetArray("WeaponProgressions")
	if arr == nil {
		t.Fatal("WeaponProgressions not found")
	}
	if len(arr) <= 100 {
		t.Errorf("WeaponProgressions length = %d, want > 100", len(arr))
	}
}

func TestParseEmptyInput(t *testing.T) {
	_, err := ParseBytes(nil)
	if err == nil {
		t.Error("expected error for empty input, got nil")
	}
}

func TestParseBadMagic(t *testing.T) {
	// First verify the real file has the right magic.
	data, err := os.ReadFile("testdata/EXPEDITION_0.sav")
	if err != nil {
		t.Fatalf("read test file: %v", err)
	}
	if len(data) < 4 {
		t.Fatal("test file too small")
	}
	// Confirm correct magic.
	if data[0] != 'G' || data[1] != 'V' || data[2] != 'A' || data[3] != 'S' {
		t.Fatalf("test file has wrong magic: %02x %02x %02x %02x", data[0], data[1], data[2], data[3])
	}

	// Corrupt magic and attempt parse.
	bad := make([]byte, len(data))
	copy(bad, data)
	bad[0] = 0xFF
	_, err = ParseBytes(bad)
	if err == nil {
		t.Error("expected error for bad magic, got nil")
	}
}
