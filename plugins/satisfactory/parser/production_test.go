package main

import "testing"

func TestItemStackSizes(t *testing.T) {
	cases := map[string]int{
		"Desc_IronRod_C":   200, // SS_BIG
		"Desc_IronScrew_C": 500, // SS_HUGE
		"Desc_IronPlate_C": 200, // SS_BIG
		"Desc_OreIron_C":   100, // SS_MEDIUM
	}
	for class, want := range cases {
		if got, ok := itemStackSize[class]; !ok {
			t.Errorf("itemStackSize[%q] missing", class)
		} else if got != want {
			t.Errorf("itemStackSize[%q] = %d, want %d", class, got, want)
		}
	}
}

func TestRecipeIO(t *testing.T) {
	spec, ok := recipeIO["Recipe_Screw_C"]
	if !ok {
		t.Fatal("recipeIO[Recipe_Screw_C] missing")
	}
	if len(spec.Ingredients) != 1 ||
		spec.Ingredients[0].Class != "Desc_IronRod_C" || spec.Ingredients[0].Amount != 1 {
		t.Errorf("Recipe_Screw ingredients = %+v, want [{Desc_IronRod_C 1}]", spec.Ingredients)
	}
	if len(spec.Products) != 1 ||
		spec.Products[0].Class != "Desc_IronScrew_C" || spec.Products[0].Amount != 4 {
		t.Errorf("Recipe_Screw products = %+v, want [{Desc_IronScrew_C 4}]", spec.Products)
	}
}
