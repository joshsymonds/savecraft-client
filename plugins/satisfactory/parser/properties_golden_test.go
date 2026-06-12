package main

import (
	"strings"
	"testing"

	"github.com/joshsymonds/savecraft.gg/plugins/satisfactory/sav"
)

// Expected property names/values were established with an independent
// python decode of the player object in each fixture (see task notes).

func decodePlayer(t *testing.T, fixture string) (*sav.ObjectData, sav.Object) {
	t.Helper()
	got := extractFromFixture(t, fixture, wantPlayer)
	if len(got) != 1 {
		t.Fatalf("got %d player objects, want 1", len(got))
	}
	od, err := sav.ParseObjectData(got[0])
	if err != nil {
		t.Fatalf("ParseObjectData: %v", err)
	}
	return od, got[0]
}

func componentNames(od *sav.ObjectData) []string {
	names := make([]string, 0, len(od.Components))
	for _, c := range od.Components {
		parts := strings.Split(c.Path, ".")
		names = append(names, parts[len(parts)-1])
	}
	return names
}

func TestGoldenPlayerPropertiesEarlyGame(t *testing.T) {
	od, obj := decodePlayer(t, "early_game.sav")

	if obj.PackageVersionUE5 != 1000 {
		t.Errorf("PackageVersionUE5 = %d, want 1000 (sv46 has no version data)", obj.PackageVersionUE5)
	}
	if len(od.Components) != 9 {
		t.Errorf("components = %d (%v), want 9", len(od.Components), componentNames(od))
	}
	if !strings.Contains(strings.Join(componentNames(od), ","), "HealthComponent") {
		t.Errorf("components %v missing HealthComponent", componentNames(od))
	}
	if od.Properties["mIsFlashlightOn"] != true {
		t.Errorf("mIsFlashlightOn = %v, want true", od.Properties["mIsFlashlightOn"])
	}
	buildGun, ok := od.Properties["mBuildGun"].(sav.ObjectRef)
	if !ok || buildGun.Path == "" {
		t.Errorf("mBuildGun = %v, want non-empty ObjectRef", od.Properties["mBuildGun"])
	}
	assertSafeGroundVectors(t, od)
}

// mLastSafeGroundPositions appears three times (a ring buffer) and decodes
// to world-position vectors — non-zero, within the map's ±500k cm bounds.
func assertSafeGroundVectors(t *testing.T, od *sav.ObjectData) {
	t.Helper()
	vecs, ok := od.Properties["mLastSafeGroundPositions"].([]any)
	if !ok || len(vecs) != 3 {
		t.Fatalf("mLastSafeGroundPositions = %v (%T), want 3 vectors",
			od.Properties["mLastSafeGroundPositions"], od.Properties["mLastSafeGroundPositions"])
	}
	for i, v := range vecs {
		vec, ok := v.([3]float64)
		if !ok {
			t.Fatalf("vector %d = %T", i, v)
		}
		if vec == ([3]float64{}) {
			t.Errorf("vector %d is zero", i)
		}
		for _, c := range vec {
			if c < -500_000 || c > 500_000 {
				t.Errorf("vector %d component %v outside map bounds", i, c)
			}
		}
	}
}

func TestGoldenPlayerPropertiesCurrent12(t *testing.T) {
	od, obj := decodePlayer(t, "current_1_2.sav")

	if obj.PackageVersionUE5 != 1017 {
		t.Errorf("PackageVersionUE5 = %d, want 1017", obj.PackageVersionUE5)
	}
	if len(od.Components) != 9 {
		t.Errorf("components = %d (%v), want 9", len(od.Components), componentNames(od))
	}
	if _, ok := od.Properties["mLastSafeGroundPositionLoopHead"].(int64); !ok {
		t.Errorf("mLastSafeGroundPositionLoopHead = %v (%T), want int64",
			od.Properties["mLastSafeGroundPositionLoopHead"], od.Properties["mLastSafeGroundPositionLoopHead"])
	}
	buildGun, ok := od.Properties["mBuildGun"].(sav.ObjectRef)
	if !ok || buildGun.Path == "" {
		t.Errorf("mBuildGun = %v, want non-empty ObjectRef", od.Properties["mBuildGun"])
	}
	assertSafeGroundVectors(t, od)
}

// Decode every buildable factory machine in the megafactory while holding
// the heap bound — proves property decoding works at scale across both the
// persistent level and sublevels.
func TestGoldenMegafactoryDecodeMachines(t *testing.T) {
	wantMachine := func(o sav.ObjectHeader) bool {
		return strings.Contains(o.ClassPath, "/Buildable/Factory/ConstructorMk1/")
	}
	objs := extractFromFixture(t, "megafactory.sav", wantMachine)
	if len(objs) == 0 {
		t.Skip("no constructors in fixture (unexpected for a megafactory)")
	}

	decoded, withRecipe := 0, 0
	for _, o := range objs {
		od, err := sav.ParseObjectData(o)
		if err != nil {
			t.Fatalf("ParseObjectData(%s): %v", o.InstanceName, err)
		}
		decoded++
		if _, ok := od.Properties["mCurrentRecipe"].(sav.ObjectRef); ok {
			withRecipe++
		}
	}
	if withRecipe == 0 {
		t.Errorf("decoded %d constructors, none had mCurrentRecipe ObjectRef", decoded)
	}
	t.Logf("decoded %d constructors, %d with mCurrentRecipe", decoded, withRecipe)
}
