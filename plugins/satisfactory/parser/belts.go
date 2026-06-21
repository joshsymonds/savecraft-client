package main

import "strings"

// beltRecord is one collected conveyor belt or lift actor: its instance, tier
// class, world position, and item throughput (items/min from beltThroughput, 0
// for an unknown class). Belts are retained as their own records — not folded
// into the machine aggregates — so the logistics graph and a future tap-order
// reconstruction can use their layout.
type beltRecord struct {
	instance   string
	class      string // e.g. Build_ConveyorBeltMk1_C
	position   [3]float32
	throughput float64
}

// isBelt reports whether a class path (or bare class name) is a conveyor belt
// or lift — the passive transport actors the throughput model needs but the
// factory/logistics classifiers deliberately skip.
func isBelt(classPath string) bool {
	return strings.Contains(classPath, "Build_ConveyorBelt") ||
		strings.Contains(classPath, "Build_ConveyorLift")
}
