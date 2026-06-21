package main

import "testing"

func TestBeltThroughputTable(t *testing.T) {
	// Generated from the game's belt mSpeed (items/min = mSpeed / 2).
	cases := map[string]float64{
		"Build_ConveyorBeltMk1_C": 60,
		"Build_ConveyorBeltMk2_C": 120,
		"Build_ConveyorBeltMk3_C": 270,
		"Build_ConveyorBeltMk4_C": 480,
		"Build_ConveyorBeltMk5_C": 780,
		"Build_ConveyorBeltMk6_C": 1200,
		"Build_ConveyorLiftMk1_C": 60,
		"Build_ConveyorLiftMk6_C": 1200,
	}
	for class, want := range cases {
		if got := beltThroughput[class]; got != want {
			t.Errorf("beltThroughput[%s] = %v, want %v", class, got, want)
		}
	}
}

func TestBeltActorsCollected(t *testing.T) {
	// early_game.sav has Mk1/Mk2/Mk4/Mk5/Mk6 belts + lifts.
	state := parseFixtureSections(t, "early_game.sav")
	if len(state.belts) == 0 {
		t.Fatalf("no belt actors collected")
	}
	for _, b := range state.belts {
		if b.class == "" {
			t.Errorf("belt record missing class: %+v", b)
		}
		// Throughput must match the generated table for the belt's class.
		if b.throughput != beltThroughput[b.class] {
			t.Errorf(
				"belt %s throughput = %v, want %v",
				b.class,
				b.throughput,
				beltThroughput[b.class],
			)
		}
		// Belts are actors and carry a world position.
		if b.position == [3]float32{} {
			t.Errorf("belt %s has zero position", b.instance)
		}
	}
}

// Admitting belt actors must not leak them into the machine aggregates.
func TestBeltsNoLeakageIntoMachines(t *testing.T) {
	state := parseFixtureSections(t, "early_game.sav")
	for _, group := range [][]machineRecord{state.manufacturers, state.extractors, state.generators} {
		for _, m := range group {
			if isBelt(m.building) {
				t.Errorf("belt leaked into a machine slice: %s", m.building)
			}
		}
	}
}

func TestUnknownBeltClassZeroThroughput(t *testing.T) {
	if got := beltThroughput["Build_ConveyorBeltMkModded_C"]; got != 0 {
		t.Errorf("unknown belt class throughput = %v, want 0", got)
	}
}
