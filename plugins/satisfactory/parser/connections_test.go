package main

import (
	"testing"

	"github.com/joshsymonds/savecraft.gg/plugins/satisfactory/sav"
)

func TestActorOf(t *testing.T) {
	cases := map[string]string{
		"Persistent_Level:PersistentLevel.Build_MinerMk1_C_7.Output0":              "Persistent_Level:PersistentLevel.Build_MinerMk1_C_7",
		"Persistent_Level:PersistentLevel.Build_ConveyorBeltMk1_C_42.ConveyorAny1": "Persistent_Level:PersistentLevel.Build_ConveyorBeltMk1_C_42",
	}
	for in, want := range cases {
		if got := actorOf(in); got != want {
			t.Errorf("actorOf(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestConnEdgeFrom(t *testing.T) {
	miner := "Persistent_Level:PersistentLevel.Build_MinerMk1_C_7.Output0"
	belt := "Persistent_Level:PersistentLevel.Build_ConveyorBeltMk1_C_42.ConveyorAny0"

	connected := &sav.ObjectData{Properties: map[string]any{
		"mConnectedComponent": sav.ObjectRef{Path: belt},
	}}
	edge, ok := connEdgeFrom(miner, connected, "belt")
	if !ok {
		t.Fatal("connected component should produce an edge")
	}
	if edge.from != "Persistent_Level:PersistentLevel.Build_MinerMk1_C_7" ||
		edge.to != "Persistent_Level:PersistentLevel.Build_ConveyorBeltMk1_C_42" ||
		edge.transport != "belt" {
		t.Errorf("edge = %+v", edge)
	}

	pipe := &sav.ObjectData{Properties: map[string]any{
		"mConnectedComponent": sav.ObjectRef{
			Path: "Persistent_Level:PersistentLevel.Build_GeneratorCoal_C_3.FGPipeConnectionFactory",
		},
	}}
	pe, ok := connEdgeFrom(
		"Persistent_Level:PersistentLevel.Build_Pipeline_C_9.PipelineConnection0",
		pipe,
		"pipe",
	)
	if !ok || pe.transport != "pipe" ||
		pe.to != "Persistent_Level:PersistentLevel.Build_GeneratorCoal_C_3" {
		t.Errorf("pipe edge = %+v ok=%v", pe, ok)
	}

	// Snap-only / unconnected: no mConnectedComponent → no edge.
	if _, ok := connEdgeFrom(
		"x.SnapOnly0",
		&sav.ObjectData{Properties: map[string]any{}},
		"belt",
	); ok {
		t.Error("unconnected component should produce no edge")
	}
}

func TestConnEdgeDirection(t *testing.T) {
	const (
		machine = "L:P.Build_AssemblerMk1_C_5"
		belt    = "L:P.Build_ConveyorBeltMk1_C_9"
	)
	connTo := func(target string) *sav.ObjectData {
		return &sav.ObjectData{Properties: map[string]any{
			"mConnectedComponent": sav.ObjectRef{Path: target + ".ConveyorAny0"},
		}}
	}

	// Output connector: flow leaves the machine → owner→connected, directed.
	out, _ := connEdgeFrom(machine+".Output0", connTo(belt), "belt")
	if out.from != machine || out.to != belt || !out.directed {
		t.Errorf("output edge = %+v, want %s→%s directed", out, machine, belt)
	}

	// Input connector: flow enters the machine → connected→owner (reversed), directed.
	in, _ := connEdgeFrom(machine+".Input1", connTo(belt), "belt")
	if in.from != belt || in.to != machine || !in.directed {
		t.Errorf("input edge = %+v, want %s→%s directed", in, belt, machine)
	}

	// Belt-side ConveyorAny connector: direction unknown → undirected, but still
	// from=owner→to=connected for connectivity.
	beltSide, _ := connEdgeFrom(belt+".ConveyorAny1", connTo(machine), "belt")
	if beltSide.from != belt || beltSide.to != machine || beltSide.directed {
		t.Errorf("conveyor-any edge = %+v, want %s→%s undirected", beltSide, belt, machine)
	}
}
