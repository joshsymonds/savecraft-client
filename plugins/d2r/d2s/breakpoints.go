package d2s

// Breakpoint holds the result of a breakpoint lookup: the highest breakpoint
// reached and the next one to aim for (nil if already at max).
type Breakpoint struct {
	Total          int  `json:"total"`
	Current        int  `json:"breakpoint"`
	NextBreakpoint *int `json:"nextBreakpoint"`
}

// FindBreakpoint returns the highest breakpoint reached for the given total,
// plus the next breakpoint target. The table must be sorted ascending.
func FindBreakpoint(table []int, total int) Breakpoint {
	bp := Breakpoint{Total: total}
	for i, threshold := range table {
		if total >= threshold {
			bp.Current = threshold
			if i+1 < len(table) {
				next := table[i+1]
				bp.NextBreakpoint = &next
			} else {
				bp.NextBreakpoint = nil
			}
		} else {
			break
		}
	}
	return bp
}

// FCR breakpoint tables per class (sorted ascending).
// These are frame counts where faster cast rate hits the next animation frame.
var fcrBreakpoints = map[Class][]int{
	Amazon:      {0, 7, 14, 22, 32, 48, 68, 99, 152},
	Sorceress:   {0, 9, 20, 37, 63, 105, 200},
	Necromancer: {0, 9, 18, 30, 48, 75, 125},
	Paladin:     {0, 9, 18, 30, 48, 75, 125},
	Barbarian:   {0, 9, 20, 37, 63, 105, 200},
	Druid:       {0, 4, 10, 19, 30, 46, 68, 99, 163},
	Assassin:    {0, 8, 16, 27, 42, 65, 102, 174},
	Warlock:     {0, 9, 18, 30, 48, 75, 125},
}

// FHR breakpoint tables per class (sorted ascending).
var fhrBreakpoints = map[Class][]int{
	Amazon:      {0, 6, 13, 20, 32, 52, 86, 174, 600},
	Sorceress:   {0, 5, 9, 14, 20, 30, 42, 60, 86, 142, 280},
	Necromancer: {0, 5, 10, 16, 26, 39, 56, 86, 152, 377},
	Paladin:     {0, 7, 15, 27, 48, 86, 200},
	Barbarian:   {0, 7, 15, 27, 48, 86, 200},
	Druid:       {0, 5, 10, 16, 26, 39, 56, 86, 152, 377},
	Assassin:    {0, 7, 15, 27, 48, 86, 200},
	Warlock:     {0, 5, 9, 14, 20, 30, 42, 60, 86, 142, 280},
}

// IAS breakpoints are weapon-dependent in practice, but total IAS from gear
// is still a useful aggregate. We use a simplified universal table.
var iasBreakpoints = []int{0, 10, 20, 30, 40, 50, 60, 75, 95, 120}

// FCRBreakpoints returns the FCR breakpoint table for the given class.
func FCRBreakpoints(class Class) []int {
	if table, ok := fcrBreakpoints[class]; ok {
		return table
	}
	// Fallback to Sorceress for unknown classes.
	return fcrBreakpoints[Sorceress]
}

// FHRBreakpoints returns the FHR breakpoint table for the given class.
func FHRBreakpoints(class Class) []int {
	if table, ok := fhrBreakpoints[class]; ok {
		return table
	}
	return fhrBreakpoints[Paladin]
}

// IASBreakpoints returns the simplified IAS breakpoint table.
func IASBreakpoints() []int {
	return iasBreakpoints
}
