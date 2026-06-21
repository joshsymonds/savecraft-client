package main

import "testing"

const (
	screwRecipe   = "/Game/FactoryGame/Recipes/Constructor/Recipe_Screw.Recipe_Screw_C"
	leachedRecipe = "/Game/FactoryGame/Recipes/Foundry/Recipe_Alternate_IronIngot_Leached.Recipe_Alternate_IronIngot_Leached_C"
	storageRecipe = "/Game/FactoryGame/Recipes/Buildings/Recipe_StorageContainerMk1.Recipe_StorageContainerMk1_C"
)

func TestClassifyManufacturer(t *testing.T) {
	cases := []struct {
		name         string
		rec          machineRecord
		wantStatus   machineStatus
		wantLimiting string // "" = none expected
	}{
		{
			name: "balanced: productivity at full",
			rec: machineRecord{
				recipe: screwRecipe, productivity: 1.0,
				inputContents: []invStack{{"Desc_IronRod_C", 200}},
			},
			wantStatus: statusBalanced,
		},
		{
			name: "balanced: at the throttle threshold",
			rec: machineRecord{
				recipe:       screwRecipe,
				productivity: throttledProductivityThreshold,
			},
			wantStatus: statusBalanced,
		},
		{
			name:       "balanced: no measurement, mIsProducing fallback true",
			rec:        machineRecord{recipe: screwRecipe, producing: true, productivity: -1},
			wantStatus: statusBalanced,
		},
		{
			name: "input_limited: throttled, one input below a craft",
			// leached needs OreIron 5 + SulfuricAcid 1000. OreIron 2 (ratio 0.4)
			// is thin; acid 5000 (ratio 5) is plentiful → OreIron is the limiter.
			rec: machineRecord{
				recipe:       leachedRecipe,
				productivity: 0.5,
				inputContents: []invStack{
					{"/Game/X/Desc_OreIron.Desc_OreIron_C", 2},
					{"/Game/X/Desc_SulfuricAcid.Desc_SulfuricAcid_C", 5000},
				},
			},
			wantStatus:   statusInputLimited,
			wantLimiting: displayName("Desc_OreIron_C"),
		},
		{
			name: "input_limited: thinnest of two thin inputs wins",
			// OreIron 3 (ratio 0.6) vs SulfuricAcid 100 (ratio 0.1) → acid limits.
			rec: machineRecord{
				recipe:       leachedRecipe,
				productivity: 0.3,
				inputContents: []invStack{
					{"/Game/X/Desc_OreIron.Desc_OreIron_C", 3},
					{"/Game/X/Desc_SulfuricAcid.Desc_SulfuricAcid_C", 100},
				},
			},
			wantStatus:   statusInputLimited,
			wantLimiting: displayName("Desc_SulfuricAcid_C"),
		},
		{
			name: "output_limited: throttled, inputs plentiful, output near full",
			rec: machineRecord{
				recipe:        screwRecipe,
				productivity:  0.5,
				inputContents: []invStack{{"Desc_IronRod_C", 200}},
				outputContents: []invStack{
					{"/Game/X/Desc_IronScrew.Desc_IronScrew_C", 450},
				}, // 450/500 = 0.9
			},
			wantStatus: statusOutputLimited,
		},
		{
			name: "output_limited: throttled, inputs plentiful, output empty (no observable input constraint)",
			rec: machineRecord{
				recipe: screwRecipe, productivity: 0.5,
				inputContents: []invStack{{"Desc_IronRod_C", 200}},
			},
			wantStatus: statusOutputLimited,
		},
		{
			name: "blocked: productivity 0, output at stack max",
			rec: machineRecord{
				recipe: screwRecipe, productivity: 0,
				inputContents:  []invStack{{"Desc_IronRod_C", 200}},
				outputContents: []invStack{{"Desc_IronScrew_C", 500}},
			},
			wantStatus: statusBlocked,
		},
		{
			name: "starved: productivity 0, ingredient absent, names the ingredient",
			rec: machineRecord{
				recipe: screwRecipe, productivity: 0,
				inputContents:  nil,
				outputContents: []invStack{{"Desc_IronScrew_C", 100}},
			},
			wantStatus:   statusStarved,
			wantLimiting: displayName("Desc_IronRod_C"),
		},
		{
			name:       "unconfigured: no recipe",
			rec:        machineRecord{recipe: "", productivity: 0},
			wantStatus: statusUnconfigured,
		},
		{
			name: "idle: productivity 0, input present, output not full",
			rec: machineRecord{
				recipe: screwRecipe, productivity: 0,
				inputContents:  []invStack{{"Desc_IronRod_C", 50}},
				outputContents: []invStack{{"Desc_IronScrew_C", 100}},
			},
			wantStatus: statusIdle,
		},
		{
			name: "idle: no measurement, not producing, inputs ok",
			rec: machineRecord{
				recipe: screwRecipe, producing: false, productivity: -1,
				inputContents: []invStack{{"Desc_IronRod_C", 50}},
			},
			wantStatus: statusIdle,
		},
		{
			name: "starved: fluid ingredient absent, names the fluid",
			rec: machineRecord{
				recipe: leachedRecipe, productivity: 0,
				inputContents: []invStack{{"Desc_OreIron_C", 50}}, // no SulfuricAcid
			},
			wantStatus:   statusStarved,
			wantLimiting: displayName("Desc_SulfuricAcid_C"),
		},
		{
			name: "idle: fluid ingredient present, not starved",
			rec: machineRecord{
				recipe: leachedRecipe, productivity: 0,
				inputContents: []invStack{{"Desc_OreIron_C", 50}, {"Desc_SulfuricAcid_C", 1000}},
			},
			wantStatus: statusIdle,
		},
		{
			name: "idle: product lacks a stack size → not blocked",
			rec: machineRecord{
				recipe: storageRecipe, productivity: 0,
				inputContents:  []invStack{{"Desc_IronPlate_C", 10}, {"Desc_IronRod_C", 10}},
				outputContents: []invStack{{"Desc_StorageContainerMk1_C", 50}},
			},
			wantStatus: statusIdle,
		},
		{
			name: "idle: unknown recipe (no panic)",
			rec: machineRecord{
				recipe: "/Game/X/Recipe_Modded.Recipe_Modded_C", productivity: 0,
				inputContents: []invStack{{"Desc_Whatever_C", 5}},
			},
			wantStatus: statusIdle,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := diagnose(tc.rec, "manufacturer")
			if got.status != tc.wantStatus {
				t.Errorf("status = %q, want %q", got.status, tc.wantStatus)
			}
			if got.limitingInput != tc.wantLimiting {
				t.Errorf("limitingInput = %q, want %q", got.limitingInput, tc.wantLimiting)
			}
		})
	}
}

func TestClassifyExtractor(t *testing.T) {
	cases := []struct {
		name string
		rec  machineRecord
		want machineStatus
	}{
		{
			name: "balanced",
			rec:  machineRecord{productivity: 1.0},
			want: statusBalanced,
		},
		{
			name: "output_limited: throttled with no recipe inputs",
			rec:  machineRecord{productivity: 0.4},
			want: statusOutputLimited,
		},
		{
			name: "blocked: output ore at stack max",
			rec: machineRecord{
				productivity:   0,
				outputContents: []invStack{{"Desc_OreIron_C", 100}}, // SS_MEDIUM
			},
			want: statusBlocked,
		},
		{
			name: "idle: output below max",
			rec: machineRecord{
				productivity:   0,
				outputContents: []invStack{{"Desc_OreIron_C", 50}},
			},
			want: statusIdle,
		},
		{
			name: "idle: no recipe never makes an extractor unconfigured",
			rec:  machineRecord{recipe: "", productivity: 0, outputContents: nil},
			want: statusIdle,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyMachine(tc.rec, "extractor"); got != tc.want {
				t.Errorf("classifyMachine = %q, want %q", got, tc.want)
			}
		})
	}
}
