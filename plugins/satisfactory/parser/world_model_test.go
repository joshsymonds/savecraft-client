package main

import (
	"testing"

	"github.com/joshsymonds/savecraft.gg/plugins/satisfactory/sav"
)

// collectMachineAt mirrors collectMachine but carries an instance path and
// world position, so position capture and inventory joins can be exercised.
func collectMachineAt(
	s *saveState,
	classPath, instance string,
	pos [3]float32,
	props map[string]any,
) {
	o := sav.Object{ObjectHeader: sav.ObjectHeader{
		ClassPath:    classPath,
		InstanceName: instance,
		Translation:  pos,
	}}
	s.collectFactory(factoryKind(classPath), o, &sav.ObjectData{Properties: props})
}

func stackOf(itemClass string, count int64) map[string]any {
	return map[string]any{
		"Item":     sav.InventoryItem{ItemClass: itemClass},
		"NumItems": count,
	}
}

func TestCollectFactoryCapturesPositionAndInstance(t *testing.T) {
	s := newSaveState(testHeader())
	constructor := "/Game/FactoryGame/Buildable/Factory/ConstructorMk1/Build_ConstructorMk1.Build_ConstructorMk1_C"
	inst := "Persistent_Level:PersistentLevel.Build_ConstructorMk1_C_7"
	collectMachineAt(s, constructor, inst, [3]float32{-249734, -55302, -271}, map[string]any{
		"mCurrentRecipe": sav.ObjectRef{Path: "/Game/X/Recipe_Screw.Recipe_Screw_C"},
		"mIsProducing":   true,
	})
	if len(s.manufacturers) != 1 {
		t.Fatalf("manufacturers = %d, want 1", len(s.manufacturers))
	}
	rec := s.manufacturers[0]
	if rec.instance != inst {
		t.Errorf("instance = %q, want %q", rec.instance, inst)
	}
	if rec.position != [3]float32{-249734, -55302, -271} {
		t.Errorf("position = %v, want [-249734 -55302 -271]", rec.position)
	}
}

func TestResolveJoinsInventoryContents(t *testing.T) {
	s := newSaveState(testHeader())
	constructor := "/Game/FactoryGame/Buildable/Factory/ConstructorMk1/Build_ConstructorMk1.Build_ConstructorMk1_C"
	miner := "/Game/FactoryGame/Buildable/Factory/MinerMk1/Build_MinerMk1.Build_MinerMk1_C"

	machineA := "Persistent_Level:PersistentLevel.Build_ConstructorMk1_C_1"
	machineB := "Persistent_Level:PersistentLevel.Build_MinerMk1_C_2"
	machineC := "Persistent_Level:PersistentLevel.Build_ConstructorMk1_C_3" // no inventory components

	collectMachineAt(s, constructor, machineA, [3]float32{1, 0, 0}, map[string]any{
		"mCurrentRecipe": sav.ObjectRef{Path: "/Game/X/Recipe_Screw.Recipe_Screw_C"},
	})
	collectMachineAt(s, miner, machineB, [3]float32{2, 0, 0}, nil)
	collectMachineAt(s, constructor, machineC, [3]float32{3, 0, 0}, map[string]any{
		"mCurrentRecipe": sav.ObjectRef{Path: "/Game/X/Recipe_IronRod.Recipe_IronRod_C"},
	})

	// Hand-built inventory components keyed by their instance path, as the
	// extract pass would have stored them.
	s.machineInventories[machineA+".InputInventory"] = &sav.ObjectData{Properties: map[string]any{
		"mInventoryStacks": []any{stackOf("/Game/X/Desc_IronRod.Desc_IronRod_C", 200)},
	}}
	s.machineInventories[machineA+".OutputInventory"] = &sav.ObjectData{Properties: map[string]any{
		"mInventoryStacks": []any{stackOf("/Game/X/Desc_IronScrew.Desc_IronScrew_C", 144)},
	}}
	s.machineInventories[machineB+".OutputInventory"] = &sav.ObjectData{Properties: map[string]any{
		"mInventoryStacks": []any{stackOf("/Game/X/Desc_OreIron.Desc_OreIron_C", 50)},
	}}

	s.resolve()

	a := s.manufacturers[0]
	if len(a.inputContents) != 1 ||
		a.inputContents[0].itemClass != "/Game/X/Desc_IronRod.Desc_IronRod_C" ||
		a.inputContents[0].count != 200 {
		t.Errorf("machineA input = %+v, want IronRod=200", a.inputContents)
	}
	if len(a.outputContents) != 1 ||
		a.outputContents[0].itemClass != "/Game/X/Desc_IronScrew.Desc_IronScrew_C" ||
		a.outputContents[0].count != 144 {
		t.Errorf("machineA output = %+v, want IronScrew=144", a.outputContents)
	}

	b := s.extractors[0]
	if len(b.inputContents) != 0 {
		t.Errorf("machineB (extractor) input = %+v, want empty", b.inputContents)
	}
	if len(b.outputContents) != 1 || b.outputContents[0].count != 50 {
		t.Errorf("machineB output = %+v, want OreIron=50", b.outputContents)
	}

	// Machine C has no inventory components: contents resolve empty, no panic.
	c := s.manufacturers[1]
	if len(c.inputContents) != 0 || len(c.outputContents) != 0 {
		t.Errorf(
			"machineC contents = in:%+v out:%+v, want both empty",
			c.inputContents,
			c.outputContents,
		)
	}
}

func TestResolveEmptyInventoryComponent(t *testing.T) {
	s := newSaveState(testHeader())
	constructor := "/Game/FactoryGame/Buildable/Factory/ConstructorMk1/Build_ConstructorMk1.Build_ConstructorMk1_C"
	inst := "Persistent_Level:PersistentLevel.Build_ConstructorMk1_C_9"
	collectMachineAt(s, constructor, inst, [3]float32{0, 0, 0}, map[string]any{
		"mCurrentRecipe": sav.ObjectRef{Path: "/Game/X/Recipe_Screw.Recipe_Screw_C"},
	})
	// Component present but no stacks property.
	s.machineInventories[inst+".OutputInventory"] = &sav.ObjectData{Properties: map[string]any{}}
	s.resolve()
	if len(s.manufacturers[0].outputContents) != 0 {
		t.Errorf("output = %+v, want empty", s.manufacturers[0].outputContents)
	}
}
