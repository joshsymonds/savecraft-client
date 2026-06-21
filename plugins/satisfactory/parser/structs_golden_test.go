package main

import (
	"strings"
	"testing"

	"github.com/joshsymonds/savecraft.gg/plugins/satisfactory/sav"
)

// Expected stack counts and items were verified by decoding the fixtures and
// inspecting the output by hand (sink coupons, chainsaw, hazmat suit — all
// consistent with a 6-hour 1.0 save).

func playerInventoryStacks(t *testing.T, fixture string) []any {
	t.Helper()
	objs := extractFromFixture(t, fixture, func(o sav.ObjectHeader) bool {
		return strings.Contains(o.ClassPath, "FGInventoryComponent")
	})
	for _, o := range objs {
		if !strings.Contains(o.InstanceName, "Char_Player") ||
			!strings.HasSuffix(o.InstanceName, ".inventory") {
			continue
		}
		od, err := sav.ParseObjectData(o)
		if err != nil {
			t.Fatalf("ParseObjectData(%s): %v", o.InstanceName, err)
		}
		if len(od.Skipped) != 0 {
			t.Errorf("player inventory has skipped properties: %v", od.Skipped)
		}
		stacks, ok := od.Properties["mInventoryStacks"].([]any)
		if !ok {
			t.Fatalf("mInventoryStacks = %T", od.Properties["mInventoryStacks"])
		}
		return stacks
	}
	t.Fatal("player main inventory component not found")
	return nil
}

func findStack(stacks []any, classFragment string) (int64, bool) {
	for _, s := range stacks {
		m, ok := s.(map[string]any)
		if !ok {
			continue
		}
		item, ok := m["Item"].(sav.InventoryItem)
		if !ok || !strings.Contains(item.ItemClass, classFragment) {
			continue
		}
		count, _ := m["NumItems"].(int64)
		return count, true
	}
	return 0, false
}

func countNonEmpty(stacks []any) int {
	n := 0
	for _, s := range stacks {
		if m, ok := s.(map[string]any); ok {
			if item, ok := m["Item"].(sav.InventoryItem); ok && item.ItemClass != "" {
				n++
			}
		}
	}
	return n
}

func TestGoldenPlayerInventoryEarlyGame(t *testing.T) {
	stacks := playerInventoryStacks(t, "early_game.sav")

	if len(stacks) != 48 {
		t.Errorf("stacks = %d, want 48", len(stacks))
	}
	if got := countNonEmpty(stacks); got != 37 {
		t.Errorf("non-empty stacks = %d, want 37", got)
	}
	if count, ok := findStack(stacks, "Desc_ResourceSinkCoupon"); !ok || count != 54 {
		t.Errorf("sink coupons = %d (found=%v), want 54", count, ok)
	}
}

func TestGoldenPlayerInventoryCurrent12(t *testing.T) {
	stacks := playerInventoryStacks(t, "current_1_2.sav")

	if len(stacks) != 75 {
		t.Errorf("stacks = %d, want 75", len(stacks))
	}
	if got := countNonEmpty(stacks); got != 30 {
		t.Errorf("non-empty stacks = %d, want 30", got)
	}
	if count, ok := findStack(stacks, "Desc_IronPlate"); !ok || count != 200 {
		t.Errorf("iron plates = %d (found=%v), want 200", count, ok)
	}
}

// Storage containers at megafactory scale: every FGInventoryComponent in
// the save must decode without error.
func TestGoldenMegafactoryInventoriesDecode(t *testing.T) {
	objs := extractFromFixture(t, "megafactory.sav", func(o sav.ObjectHeader) bool {
		return strings.Contains(o.ClassPath, "FGInventoryComponent")
	})
	if len(objs) == 0 {
		t.Skip("no inventory components (fixture absent)")
	}

	decoded, withStacks := 0, 0
	for _, o := range objs {
		od, err := sav.ParseObjectData(o)
		if err != nil {
			t.Fatalf("ParseObjectData(%s): %v", o.InstanceName, err)
		}
		decoded++
		if stacks, ok := od.Properties["mInventoryStacks"].([]any); ok &&
			countNonEmpty(stacks) > 0 {
			withStacks++
		}
	}
	if withStacks == 0 {
		t.Errorf("decoded %d inventories, none had non-empty stacks", decoded)
	}
	t.Logf("decoded %d inventory components, %d with items", decoded, withStacks)
}
