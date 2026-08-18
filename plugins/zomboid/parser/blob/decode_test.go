package blob

import (
	"encoding/binary"
	"errors"
	"math"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// The golden values below come from the upstream Kaitai parse of this exact
// fixture: a world-version 249 localPlayers.data blob for "Jane Doe".
const worldVersion249 = 249

func fixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("../testdata/jane-doe-249.blob")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func decodeFixture(t *testing.T) *Player {
	t.Helper()
	player, err := Decode(fixture(t), worldVersion249)
	if err != nil {
		t.Fatal(err)
	}
	return player
}

// closeEnough reports whether got is within tolerance of want.
func closeEnough(got, want float32, tolerance float64) bool {
	return math.Abs(float64(got)-float64(want)) <= tolerance
}

func TestDecodeDescriptor(t *testing.T) {
	desc := decodeFixture(t).Descriptor
	if desc == nil {
		t.Fatal("no descriptor")
	}
	if desc.Forename != "Jane" || desc.Surname != "Doe" || desc.Torso != "Kate" {
		t.Errorf("name: %q %q %q", desc.Forename, desc.Surname, desc.Torso)
	}
	if !desc.Female {
		t.Error("female: false")
	}
	if desc.Profession != "base:unemployed" {
		t.Errorf("profession: %q", desc.Profession)
	}
	if desc.VoicePrefix != "VoiceFemale" {
		t.Errorf("voice prefix: %q", desc.VoicePrefix)
	}
	want := []XPBoost{{Perk: "Strength", Level: 3}, {Perk: "Fitness", Level: 3}}
	if !reflect.DeepEqual(desc.XPBoosts, want) {
		t.Errorf("xp boosts: %v, want %v", desc.XPBoosts, want)
	}
}

func TestDecodeXP(t *testing.T) {
	player := decodeFixture(t)
	xp := player.XP
	if len(xp.Traits) != 0 {
		t.Errorf("traits: %v", xp.Traits)
	}
	if len(xp.Multipliers) != 0 {
		t.Errorf("multipliers: %v", xp.Multipliers)
	}
	if len(player.KnownRecipes) != 0 {
		t.Errorf("known recipes: %v", player.KnownRecipes)
	}
	if player.KnowAllRecipes {
		t.Error("know all recipes: true")
	}

	wantLevels := map[string]int32{"Strength": 5, "Fitness": 5, "Aiming": 8}
	gotLevels := make(map[string]int32, len(xp.PerkLevels))
	for _, level := range xp.PerkLevels {
		gotLevels[level.Perk] = level.Level
	}
	if !reflect.DeepEqual(gotLevels, wantLevels) {
		t.Errorf("perk levels: %v, want %v", gotLevels, wantLevels)
	}

	wantXP := map[string]float32{
		"SmallBlade":  0.24347272515296936,
		"Maintenance": 0.3333333432674408,
		"Doctor":      1.25,
		"SmallBlunt":  0.7950000166893005,
		"Strength":    37507.0,
		"Fitness":     37503.0,
		"Nimble":      0.25,
	}
	gotXP := make(map[string]float32, len(xp.Entries))
	for _, entry := range xp.Entries {
		gotXP[entry.Perk] = entry.XP
	}
	if !reflect.DeepEqual(gotXP, wantXP) {
		t.Errorf("perk xp: %v, want %v", gotXP, wantXP)
	}
}

func TestDecodeStats(t *testing.T) {
	stats := decodeFixture(t).Stats
	const tolerance = 1e-5
	for index, want := range map[int]float32{
		3:  1.0,
		10: 1.0,
		12: 5.252767,
		13: 84.980507,
		15: 0.99966443,
		18: 36.796982,
	} {
		if !closeEnough(stats[index], want, tolerance) {
			t.Errorf("stats[%d] = %v, want %v", index, stats[index], want)
		}
	}
}

func TestDecodeBodyDamage(t *testing.T) {
	parts := decodeFixture(t).BodyDamage.Parts
	if len(parts) != 17 {
		t.Fatalf("body parts: %d, want 17", len(parts))
	}
	full, damaged := 0, 0
	for index, part := range parts {
		switch part.Health {
		case 100.0:
			full++
		case 99.29702758789062:
			damaged++
		default:
			t.Errorf("part %d health: %v", index, part.Health)
		}
		if part.IsInfected {
			t.Errorf("part %d infected", index)
		}
		// Wetness sits deep inside body_part; garbage here means the fields
		// ahead of it were mis-sized.
		if math.IsNaN(float64(part.Wetness)) || part.Wetness < 0 || part.Wetness > 100 {
			t.Errorf("part %d wetness: %v", index, part.Wetness)
		}
	}
	if full != 16 || damaged != 1 {
		t.Errorf("health: %d parts at 100.0, %d at 99.29702758789062", full, damaged)
	}
}

func TestDecodeInventory(t *testing.T) {
	player := decodeFixture(t)
	// The groups are the fixture's ten in file order: WornItem.ItemIndex counts
	// positions in this list, so order is part of the meaning. Nine groups hold
	// a single item; registry id 2958 is stored compressed as a group of two,
	// followed by one duplicate id, so the ten groups flatten to 11 items.
	want := []InventoryItem{
		{RegistryID: 516, Count: 1},
		{RegistryID: 1914, Count: 1},
		{RegistryID: 4439, Count: 1},
		{RegistryID: 4929, Count: 1},
		{RegistryID: 539, Count: 1},
		{RegistryID: 187, Count: 1},
		{RegistryID: 4695, Count: 1},
		{RegistryID: 3210, Count: 1},
		{RegistryID: 3651, Count: 1},
		{RegistryID: 2958, Count: 2},
	}
	if !reflect.DeepEqual(player.Inventory, want) {
		t.Errorf("item groups: %v, want %v", player.Inventory, want)
	}

	flattened := 0
	for _, item := range player.Inventory {
		flattened += item.Count
	}
	if flattened != 11 {
		t.Fatalf("flattened items: %d, want 11", flattened)
	}
	for _, worn := range player.WornItems {
		if worn.ItemIndex < 0 || int(worn.ItemIndex) >= flattened {
			t.Errorf("worn %s: item index %d outside 0..%d",
				worn.BodyLocation, worn.ItemIndex, flattened-1)
		}
	}
}

// itemVisualBytes encodes a visual::item_visual: flags1, the three empty names,
// the optional fields flags1 selects, then the six trailing u1 arrays — blood,
// dirt, holes, basic_patches, denim_patches, leather_patches — sized as given.
// The fixture carries no body visuals, so this is the only input the item
// visual decoder ever sees.
func itemVisualBytes(flags1 uint8, arrays [6]int) []byte {
	data := []byte{flags1}
	for range 3 { // full_type, alternate_model_name, clothing_item_name
		data = binary.BigEndian.AppendUint16(data, 0)
	}
	if flags1&0x01 != 0 { // tint
		data = append(data, 0, 0, 0) // color_rgb
	}
	if flags1&0x02 != 0 { // base_texture
		data = append(data, 0)
	}
	if flags1&0x04 != 0 { // texture_choice
		data = append(data, 0)
	}
	if flags1&0x08 != 0 { // hue
		data = binary.BigEndian.AppendUint32(data, 0)
	}
	if flags1&0x10 != 0 { // decal
		data = binary.BigEndian.AppendUint16(data, 0)
	}
	for _, size := range arrays {
		data = append(data, uint8(size))
		data = append(data, make([]byte, size)...)
	}
	return data
}

// TestDecodeItemVisualConsumesEveryArray covers the six length-prefixed arrays
// that close item_visual. Stopping early leaves the cursor inside the visual,
// which desynchronizes every field after a character's first clothing item.
func TestDecodeItemVisualConsumesEveryArray(t *testing.T) {
	for name, visual := range map[string]struct {
		flags1 uint8
		arrays [6]int
	}{
		"no fields, no entries": {},
		"leather patches":       {arrays: [6]int{0, 0, 0, 0, 0, 3}},
		"every array":           {arrays: [6]int{1, 2, 3, 4, 5, 6}},
		"every optional field":  {flags1: 0x1f, arrays: [6]int{2, 0, 1, 0, 4, 2}},
	} {
		t.Run(name, func(t *testing.T) {
			data := itemVisualBytes(visual.flags1, visual.arrays)
			r := &reader{data: data}
			decodeItemVisual(r)
			if r.err != nil {
				t.Fatalf("decode: %v", r.err)
			}
			if r.pos != len(data) {
				t.Errorf("consumed %d of %d bytes", r.pos, len(data))
			}
		})
	}
}

// groupBytes builds an inventory::group holding identical copies of one item
// whose blob is just the registry id, plus the trailing duplicate ids.
func groupBytes(identical int32, lenData uint32, registryID uint16) []byte {
	data := binary.BigEndian.AppendUint32(nil, uint32(identical))
	data = binary.BigEndian.AppendUint32(data, lenData)
	data = binary.BigEndian.AppendUint16(data, registryID)
	for range max(identical-1, 0) {
		data = binary.BigEndian.AppendUint32(data, 0) // duplicate_ids
	}
	return data
}

func TestDecodeItemGroup(t *testing.T) {
	data := groupBytes(2, sizeU16, 2958)
	r := &reader{data: data}
	item := decodeItemGroup(r)
	if r.err != nil {
		t.Fatalf("decode: %v", r.err)
	}
	if want := (InventoryItem{RegistryID: 2958, Count: 2}); item != want {
		t.Errorf("item: %+v, want %+v", item, want)
	}
	if r.pos != len(data) {
		t.Errorf("consumed %d of %d bytes", r.pos, len(data))
	}
}

// TestDecodeItemGroupRejectsHugeLength pins sized_blob.len_data as the u4 the
// spec declares: a length with the high bit set is rejected on its true
// magnitude rather than read as a negative size.
func TestDecodeItemGroupRejectsHugeLength(t *testing.T) {
	const hugeLen = uint32(1) << 31
	r := &reader{data: groupBytes(1, hugeLen, 2958)}
	decodeItemGroup(r)
	if !errors.Is(r.err, ErrShortRead) {
		t.Fatalf("len_data %d: %v, want ErrShortRead", hugeLen, r.err)
	}
	if strings.Contains(r.err.Error(), "-2147483648") {
		t.Errorf("len_data %d read as a negative size: %v", hugeLen, r.err)
	}
}

func TestDecodePlayerTail(t *testing.T) {
	player := decodeFixture(t)
	if player.HoursSurvived != 0.07402128669514241 {
		t.Errorf("hours survived: %v", player.HoursSurvived)
	}
	if player.ZombieKills != 2 || player.SurvivorKills != 0 {
		t.Errorf("kills: %d zombie, %d survivor", player.ZombieKills, player.SurvivorKills)
	}
	wantWorn := []WornItem{
		{BodyLocation: "base:bandage", ItemIndex: 9},
		{BodyLocation: "base:bandage", ItemIndex: 10},
		{BodyLocation: "base:tshirt", ItemIndex: 0},
		{BodyLocation: "base:socks", ItemIndex: 2},
		{BodyLocation: "base:shoes", ItemIndex: 3},
		{BodyLocation: "base:shortsshort", ItemIndex: 1},
		{BodyLocation: "base:back", ItemIndex: 7},
	}
	if !reflect.DeepEqual(player.WornItems, wantWorn) {
		t.Errorf("worn items: %v, want %v", player.WornItems, wantWorn)
	}
	if player.RightHandIndex != 8 {
		t.Errorf("right hand index: %d", player.RightHandIndex)
	}
	// nutrition_data's field order is calories, proteins, lipids,
	// carbohydrates, weight (character_shared.ksy).
	wantNutrition := Nutrition{
		Calories:      1116.2327880859375,
		Proteins:      39.759239196777344,
		Lipids:        15.695856094360352,
		Carbohydrates: -0.9328311085700989,
		Weight:        80.0007553100586,
	}
	if player.Nutrition != wantNutrition {
		t.Errorf("nutrition: %+v, want %+v", player.Nutrition, wantNutrition)
	}
	if len(player.AlreadyReadBooks) != 0 {
		t.Errorf("already read books: %v", player.AlreadyReadBooks)
	}
}

// TestDecodeConsumesWholeBlob pins exact consumption: the fixture decodes and
// the same bytes plus one more do not, so the decoder stops at byte 4312.
func TestDecodeConsumesWholeBlob(t *testing.T) {
	data := fixture(t)
	if len(data) != 4312 {
		t.Fatalf("fixture size: %d, want 4312", len(data))
	}
	if _, err := Decode(data, worldVersion249); err != nil {
		t.Fatal(err)
	}
	longer := append(slices.Clone(data), 0)
	if _, err := Decode(longer, worldVersion249); !errors.Is(err, ErrTrailingBytes) {
		t.Fatalf("trailing byte: %v, want ErrTrailingBytes", err)
	}
}

func TestDecodeRejectsTruncation(t *testing.T) {
	data := fixture(t)
	for size := len(data) - 1; size > 0; size -= 64 {
		if _, err := Decode(data[:size], worldVersion249); err == nil {
			t.Errorf("truncated to %d bytes: no error", size)
		}
	}
}

func TestDecodeRejectsUnsupportedVersion(t *testing.T) {
	if _, err := Decode(fixture(t), 245); !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("world version 245: %v, want ErrUnsupportedVersion", err)
	}
}

func TestDecodeRejectsBadHeader(t *testing.T) {
	data := slices.Clone(fixture(t))
	data[0] = 2 // serialize
	if _, err := Decode(data, worldVersion249); err == nil {
		t.Error("serialize byte 2: no error")
	}
	data[0] = 1
	data[1] = 3 // class id: IsoZombie
	_, err := Decode(data, worldVersion249)
	if err == nil {
		t.Fatal("class id 3: no error")
	}
	if !strings.Contains(err.Error(), "IsoPlayer") || !strings.Contains(err.Error(), "3") {
		t.Errorf("class id 3: %v, want an error naming the class", err)
	}
}

func TestDecodeIsDeterministic(t *testing.T) {
	data := fixture(t)
	first, err := Decode(data, worldVersion249)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Decode(data, worldVersion249)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Error("two decodes of the same blob differ")
	}
}
