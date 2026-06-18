package main

import "testing"

const (
	screwRecipe   = "/Game/FactoryGame/Recipes/Constructor/Recipe_Screw.Recipe_Screw_C"
	leachedRecipe = "/Game/FactoryGame/Recipes/Foundry/Recipe_Alternate_IronIngot_Leached.Recipe_Alternate_IronIngot_Leached_C"
	storageRecipe = "/Game/FactoryGame/Recipes/Buildings/Recipe_StorageContainerMk1.Recipe_StorageContainerMk1_C"
)

func TestClassifyManufacturer(t *testing.T) {
	cases := []struct {
		name string
		rec  machineRecord
		want machineStatus
	}{
		{
			name: "producing: productivity at full",
			rec: machineRecord{
				recipe: screwRecipe, productivity: 1.0,
				inputContents: []invStack{{"Desc_IronRod_C", 200}},
			},
			want: statusProducing,
		},
		{
			name: "producing: at the throttle threshold",
			rec:  machineRecord{recipe: screwRecipe, productivity: throttledProductivityThreshold},
			want: statusProducing,
		},
		{
			name: "producing: no measurement, mIsProducing fallback true",
			rec:  machineRecord{recipe: screwRecipe, producing: true, productivity: -1},
			want: statusProducing,
		},
		{
			name: "throttled: productivity below threshold",
			rec:  machineRecord{recipe: screwRecipe, productivity: 0.5},
			want: statusThrottled,
		},
		{
			name: "blocked: productivity 0, output at stack max",
			rec: machineRecord{
				recipe: screwRecipe, productivity: 0,
				inputContents:  []invStack{{"Desc_IronRod_C", 200}},
				outputContents: []invStack{{"Desc_IronScrew_C", 500}},
			},
			want: statusBlocked,
		},
		{
			name: "starved: productivity 0, ingredient absent",
			rec: machineRecord{
				recipe: screwRecipe, productivity: 0,
				inputContents:  nil,
				outputContents: []invStack{{"Desc_IronScrew_C", 100}},
			},
			want: statusStarved,
		},
		{
			name: "unconfigured: no recipe",
			rec:  machineRecord{recipe: "", productivity: 0},
			want: statusUnconfigured,
		},
		{
			name: "idle: productivity 0, input present, output not full",
			rec: machineRecord{
				recipe: screwRecipe, productivity: 0,
				inputContents:  []invStack{{"Desc_IronRod_C", 50}},
				outputContents: []invStack{{"Desc_IronScrew_C", 100}},
			},
			want: statusIdle,
		},
		{
			name: "idle: no measurement, not producing, inputs ok",
			rec: machineRecord{
				recipe: screwRecipe, producing: false, productivity: -1,
				inputContents: []invStack{{"Desc_IronRod_C", 50}},
			},
			want: statusIdle,
		},
		{
			name: "starved: fluid ingredient absent",
			rec: machineRecord{
				recipe: leachedRecipe, productivity: 0,
				inputContents: []invStack{{"Desc_OreIron_C", 50}}, // no SulfuricAcid
			},
			want: statusStarved,
		},
		{
			name: "idle: fluid ingredient present, not starved",
			rec: machineRecord{
				recipe: leachedRecipe, productivity: 0,
				inputContents: []invStack{{"Desc_OreIron_C", 50}, {"Desc_SulfuricAcid_C", 1000}},
			},
			want: statusIdle,
		},
		{
			name: "idle: product lacks a stack size → not blocked",
			rec: machineRecord{
				recipe: storageRecipe, productivity: 0,
				inputContents:  []invStack{{"Desc_IronPlate_C", 10}, {"Desc_IronRod_C", 10}},
				outputContents: []invStack{{"Desc_StorageContainerMk1_C", 50}},
			},
			want: statusIdle,
		},
		{
			name: "idle: unknown recipe (no panic)",
			rec: machineRecord{
				recipe: "/Game/X/Recipe_Modded.Recipe_Modded_C", productivity: 0,
				inputContents: []invStack{{"Desc_Whatever_C", 5}},
			},
			want: statusIdle,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyMachine(tc.rec, "manufacturer"); got != tc.want {
				t.Errorf("classifyMachine = %q, want %q", got, tc.want)
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
			name: "producing",
			rec:  machineRecord{productivity: 1.0},
			want: statusProducing,
		},
		{
			name: "throttled",
			rec:  machineRecord{productivity: 0.4},
			want: statusThrottled,
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
