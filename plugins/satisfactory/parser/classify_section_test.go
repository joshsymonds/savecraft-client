package main

import (
	"maps"
	"testing"

	"github.com/joshsymonds/savecraft.gg/plugins/satisfactory/sav"
)

// Three screw constructors in one group — producing, blocked, starved — must
// report a status breakdown that sums to the group count.
func TestMachinesSectionStatusBreakdown(t *testing.T) {
	s := newSaveState(testHeader())
	constructor := "/Game/FactoryGame/Buildable/Factory/ConstructorMk1/Build_ConstructorMk1.Build_ConstructorMk1_C"
	recipe := map[string]any{"mCurrentRecipe": sav.ObjectRef{Path: screwRecipe}}

	producing := "Persistent_Level:PersistentLevel.Build_ConstructorMk1_C_1"
	blocked := "Persistent_Level:PersistentLevel.Build_ConstructorMk1_C_2"
	starved := "Persistent_Level:PersistentLevel.Build_ConstructorMk1_C_3"

	collectMachineAt(s, constructor, producing, [3]float32{1, 0, 0}, mergeProps(recipe, map[string]any{"mIsProducing": true,
		"mLastProductivityMeasurementDuration": 300.0, "mLastProductivityMeasurementProduceDuration": 300.0}))
	collectMachineAt(s, constructor, blocked, [3]float32{2, 0, 0}, mergeProps(recipe, map[string]any{"mIsProducing": false}))
	collectMachineAt(s, constructor, starved, [3]float32{3, 0, 0}, mergeProps(recipe, map[string]any{"mIsProducing": false}))

	// blocked: output at stack max; starved: empty input.
	s.machineInventories[blocked+".OutputInventory"] = &sav.ObjectData{Properties: map[string]any{
		"mInventoryStacks": []any{stackOf("/Game/X/Desc_IronScrew.Desc_IronScrew_C", 500)}}}
	s.machineInventories[blocked+".InputInventory"] = &sav.ObjectData{Properties: map[string]any{
		"mInventoryStacks": []any{stackOf("/Game/X/Desc_IronRod.Desc_IronRod_C", 100)}}}
	s.machineInventories[starved+".InputInventory"] = &sav.ObjectData{Properties: map[string]any{}}

	s.resolve()

	data := s.buildMachinesSection()
	groups, _ := data["manufacturers"].([]map[string]any)
	if len(groups) != 1 {
		t.Fatalf("manufacturer groups = %d, want 1", len(groups))
	}
	status, ok := groups[0]["status"].(map[machineStatus]int)
	if !ok {
		t.Fatalf("group has no status map: %v", groups[0])
	}
	want := map[machineStatus]int{statusProducing: 1, statusBlocked: 1, statusStarved: 1}
	for st, n := range want {
		if status[st] != n {
			t.Errorf("status[%s] = %d, want %d (full: %v)", st, status[st], n, status)
		}
	}
	sum := 0
	for _, n := range status {
		sum += n
	}
	if sum != groups[0]["count"] {
		t.Errorf("status counts sum %d != group count %v", sum, groups[0]["count"])
	}
}

// A fully-producing group emits no status breakdown (keeps output lean).
func TestMachinesSectionNoStatusWhenAllProducing(t *testing.T) {
	s := newSaveState(testHeader())
	constructor := "/Game/FactoryGame/Buildable/Factory/ConstructorMk1/Build_ConstructorMk1.Build_ConstructorMk1_C"
	collectMachineAt(s, constructor, "Persistent_Level:PersistentLevel.Build_ConstructorMk1_C_9", [3]float32{0, 0, 0},
		map[string]any{"mCurrentRecipe": sav.ObjectRef{Path: screwRecipe}, "mIsProducing": true,
			"mLastProductivityMeasurementDuration": 300.0, "mLastProductivityMeasurementProduceDuration": 300.0})
	s.resolve()
	groups, _ := s.buildMachinesSection()["manufacturers"].([]map[string]any)
	if _, has := groups[0]["status"]; has {
		t.Errorf("all-producing group should have no status key, got %v", groups[0]["status"])
	}
}

func mergeProps(a, b map[string]any) map[string]any {
	out := map[string]any{}
	maps.Copy(out, a)
	maps.Copy(out, b)
	return out
}
