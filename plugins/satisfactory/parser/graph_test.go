package main

import (
	"reflect"
	"slices"
	"sort"
	"testing"
)

// inst builds an actor instance path for a class + numeric id.
func inst(class string, id int) string {
	return "Persistent_Level:PersistentLevel." + class + "_" + itoa(id)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func belt(from, to string) connEdge { return connEdge{from: from, to: to, transport: "belt"} }
func pipe(from, to string) connEdge { return connEdge{from: from, to: to, transport: "pipe"} }

func machineSet(insts ...string) map[string]bool {
	m := map[string]bool{}
	for _, i := range insts {
		m[i] = true
	}
	return m
}

// lineWith returns the production line containing the given machine instance.
func lineWith(lines []productionLine, machine string) *productionLine {
	for i := range lines {
		if slices.Contains(lines[i].machines, machine) {
			return &lines[i]
		}
	}
	return nil
}

func sortedEqual(a, b []string) bool {
	a = append([]string(nil), a...)
	b = append([]string(nil), b...)
	sort.Strings(a)
	sort.Strings(b)
	return reflect.DeepEqual(a, b)
}

func TestBuildLinesBeltChain(t *testing.T) {
	miner := inst("Build_MinerMk1_C", 1)
	b1 := inst("Build_ConveyorBeltMk1_C", 2)
	b2 := inst("Build_ConveyorBeltMk1_C", 3)
	cons := inst("Build_ConstructorMk1_C", 4)
	edges := []connEdge{belt(miner, b1), belt(b1, b2), belt(b2, cons)}

	lines := buildProductionLines(edges, machineSet(miner, cons))
	if len(lines) != 1 {
		t.Fatalf("lines = %d, want 1: %+v", len(lines), lines)
	}
	if !sortedEqual(lines[0].machines, []string{miner, cons}) {
		t.Errorf("machines = %v, want {miner, constructor}", lines[0].machines)
	}
}

func TestBuildLinesSplitterFanOut(t *testing.T) {
	cons := inst("Build_ConstructorMk1_C", 1)
	sp := inst("Build_ConveyorAttachmentSplitter_C", 2)
	asmA := inst("Build_AssemblerMk1_C", 3)
	asmB := inst("Build_AssemblerMk1_C", 4)
	edges := []connEdge{belt(cons, sp), belt(sp, asmA), belt(sp, asmB)}

	lines := buildProductionLines(edges, machineSet(cons, asmA, asmB))
	if len(lines) != 1 {
		t.Fatalf("lines = %d, want 1", len(lines))
	}
	if !sortedEqual(lines[0].machines, []string{cons, asmA, asmB}) {
		t.Errorf("machines = %v", lines[0].machines)
	}
}

func TestBuildLinesMergerFanIn(t *testing.T) {
	sA := inst("Build_SmelterMk1_C", 1)
	sB := inst("Build_SmelterMk1_C", 2)
	mg := inst("Build_ConveyorAttachmentMerger_C", 3)
	cons := inst("Build_ConstructorMk1_C", 4)
	edges := []connEdge{belt(sA, mg), belt(sB, mg), belt(mg, cons)}

	lines := buildProductionLines(edges, machineSet(sA, sB, cons))
	if len(lines) != 1 || !sortedEqual(lines[0].machines, []string{sA, sB, cons}) {
		t.Fatalf("lines = %+v", lines)
	}
}

func TestBuildLinesPipeChainTransport(t *testing.T) {
	refinery := inst("Build_OilRefinery_C", 1)
	p1 := inst("Build_Pipeline_C", 2)
	p2 := inst("Build_Pipeline_C", 3)
	blender := inst("Build_Blender_C", 4)
	edges := []connEdge{pipe(refinery, p1), pipe(p1, p2), pipe(p2, blender)}

	lines := buildProductionLines(edges, machineSet(refinery, blender))
	if len(lines) != 1 {
		t.Fatalf("lines = %d, want 1", len(lines))
	}
	if !sortedEqual(lines[0].transports, []string{"pipe"}) {
		t.Errorf("transports = %v, want [pipe]", lines[0].transports)
	}
}

func TestBuildLinesStorageIsBoundary(t *testing.T) {
	consA := inst("Build_ConstructorMk1_C", 1)
	consB := inst("Build_ConstructorMk1_C", 2)
	st1 := inst("Build_StorageContainerMk1_C", 3)
	st2 := inst("Build_StorageContainerMk1_C", 4)
	edges := []connEdge{
		belt(consA, st1),
		belt(consB, st2),
		belt(st1, st2), // storage-to-storage must NOT merge the two lines
	}

	lines := buildProductionLines(edges, machineSet(consA, consB))
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2 (storage is a boundary): %+v", len(lines), lines)
	}
	la := lineWith(lines, consA)
	if la == nil || !sortedEqual(la.terminals, []string{st1}) {
		t.Errorf("consA line terminals = %v, want {st1}", la.terminals)
	}
}

func TestBuildLinesTrainDockingStationIsBoundary(t *testing.T) {
	cons := inst("Build_ConstructorMk1_C", 1)
	dock := inst("Build_TrainDockingStation_C", 2)
	consFar := inst("Build_ConstructorMk1_C", 3)
	dock2 := inst("Build_TrainDockingStation_C", 4)
	edges := []connEdge{belt(cons, dock), belt(consFar, dock2)}

	lines := buildProductionLines(edges, machineSet(cons, consFar))
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(lines))
	}
	if l := lineWith(lines, cons); l == nil || !sortedEqual(l.terminals, []string{dock}) {
		t.Errorf("line terminals = %v, want {dock}", l.terminals)
	}
}

func TestBuildLinesTwoDisjointSameRecipe(t *testing.T) {
	// Two physically separate Motor (constructor) lines, no connection between.
	c1 := inst("Build_ConstructorMk1_C", 1)
	b1 := inst("Build_ConveyorBeltMk1_C", 2)
	a1 := inst("Build_AssemblerMk1_C", 3)
	c2 := inst("Build_ConstructorMk1_C", 4)
	b2 := inst("Build_ConveyorBeltMk1_C", 5)
	a2 := inst("Build_AssemblerMk1_C", 6)
	edges := []connEdge{belt(c1, b1), belt(b1, a1), belt(c2, b2), belt(b2, a2)}

	lines := buildProductionLines(edges, machineSet(c1, a1, c2, a2))
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(lines))
	}
	if lineWith(lines, c1) == lineWith(lines, c2) {
		t.Error("the two disjoint lines must be distinct")
	}
}

func TestBuildLinesDeterministicOrder(t *testing.T) {
	// Line A has 3 machines, line B has 2 — A must sort first.
	a1, a2, a3 := inst(
		"Build_ConstructorMk1_C",
		1,
	), inst(
		"Build_AssemblerMk1_C",
		2,
	), inst(
		"Build_SmelterMk1_C",
		3,
	)
	mgA := inst("Build_ConveyorAttachmentMerger_C", 10)
	b1, b2 := inst("Build_ConstructorMk1_C", 4), inst("Build_AssemblerMk1_C", 5)
	beltB := inst("Build_ConveyorBeltMk1_C", 11)
	edges := []connEdge{
		belt(a1, mgA),
		belt(a2, mgA),
		belt(mgA, a3),
		belt(b1, beltB),
		belt(beltB, b2),
	}

	lines := buildProductionLines(edges, machineSet(a1, a2, a3, b1, b2))
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(lines))
	}
	if len(lines[0].machines) != 3 || len(lines[1].machines) != 2 {
		t.Errorf(
			"order by machine count desc broken: %d then %d",
			len(lines[0].machines),
			len(lines[1].machines),
		)
	}
	// Stable across a re-run.
	again := buildProductionLines(edges, machineSet(a1, a2, a3, b1, b2))
	if !reflect.DeepEqual(lines, again) {
		t.Error("buildProductionLines not deterministic")
	}
}

func TestBuildLinesSelfLoopIgnored(t *testing.T) {
	c := inst("Build_ConstructorMk1_C", 1)
	lines := buildProductionLines([]connEdge{belt(c, c)}, machineSet(c))
	// A self-loop gives a component with one machine and no real link.
	if len(lines) != 1 || len(lines[0].machines) != 1 {
		t.Fatalf("lines = %+v", lines)
	}
}
