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
			name: "producing at full productivity",
			rec: machineRecord{
				recipe: screwRecipe, producing: true, productivity: 1.0,
				inputContents: []invStack{{"Desc_IronRod_C", 200}},
			},
			want: statusProducing,
		},
		{
			name: "producing without a measurement",
			rec: machineRecord{
				recipe: screwRecipe, producing: true, productivity: -1,
			},
			want: statusProducing,
		},
		{
			name: "throttled below threshold",
			rec: machineRecord{
				recipe: screwRecipe, producing: true, productivity: 0.5,
			},
			want: statusThrottled,
		},
		{
			name: "blocked: output at stack max",
			rec: machineRecord{
				recipe: screwRecipe, producing: false,
				inputContents:  []invStack{{"Desc_IronRod_C", 200}},
				outputContents: []invStack{{"Desc_IronScrew_C", 500}},
			},
			want: statusBlocked,
		},
		{
			name: "starved: ingredient absent",
			rec: machineRecord{
				recipe: screwRecipe, producing: false,
				inputContents:  nil,
				outputContents: []invStack{{"Desc_IronScrew_C", 100}},
			},
			want: statusStarved,
		},
		{
			name: "unconfigured: no recipe",
			rec:  machineRecord{recipe: "", producing: false},
			want: statusUnconfigured,
		},
		{
			name: "unpowered: input present, output not full, idle",
			rec: machineRecord{
				recipe: screwRecipe, producing: false,
				inputContents:  []invStack{{"Desc_IronRod_C", 50}},
				outputContents: []invStack{{"Desc_IronScrew_C", 100}},
			},
			want: statusUnpowered,
		},
		{
			name: "fluid ingredient absent → starved",
			rec: machineRecord{
				recipe: leachedRecipe, producing: false,
				inputContents: []invStack{{"Desc_OreIron_C", 50}}, // no SulfuricAcid
			},
			want: statusStarved,
		},
		{
			name: "fluid ingredient present → not starved (unpowered)",
			rec: machineRecord{
				recipe: leachedRecipe, producing: false,
				inputContents: []invStack{{"Desc_OreIron_C", 50}, {"Desc_SulfuricAcid_C", 1000}},
			},
			want: statusUnpowered,
		},
		{
			name: "product lacks a stack size → not blocked",
			rec: machineRecord{
				recipe: storageRecipe, producing: false,
				inputContents:  []invStack{{"Desc_IronPlate_C", 10}, {"Desc_IronRod_C", 10}},
				outputContents: []invStack{{"Desc_StorageContainerMk1_C", 50}},
			},
			want: statusUnpowered,
		},
		{
			name: "unknown recipe, idle → unpowered (no panic)",
			rec: machineRecord{
				recipe: "/Game/X/Recipe_Modded.Recipe_Modded_C", producing: false,
				inputContents: []invStack{{"Desc_Whatever_C", 5}},
			},
			want: statusUnpowered,
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
			rec:  machineRecord{producing: true, productivity: 1.0},
			want: statusProducing,
		},
		{
			name: "blocked: output ore at stack max",
			rec: machineRecord{
				producing:      false,
				outputContents: []invStack{{"Desc_OreIron_C", 100}}, // SS_MEDIUM
			},
			want: statusBlocked,
		},
		{
			name: "unpowered: output below max, idle",
			rec: machineRecord{
				producing:      false,
				outputContents: []invStack{{"Desc_OreIron_C", 50}},
			},
			want: statusUnpowered,
		},
		{
			name: "no recipe never makes an extractor unconfigured",
			rec:  machineRecord{recipe: "", producing: false, outputContents: nil},
			want: statusUnpowered,
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
