package main

// v3b compression for `game:<match_id>` section data.
//
// MTGA game logs grow linearly with turn count; a single match has exceeded
// the 80 KB / 25k-token tool-result limit in production. v3b shrinks payloads
// ~60% without losing analytical fidelity. See docs/games.md (magic section)
// and the gambit epic for the decision rationale.
//
// Transforms applied:
//  1. Drop the `type` discriminator on actions (kind is implied by which
//     non-nil inner key is present: cast/tap/move/ability/damage/resolve/
//     statMod/target).
//  2. Drop null/empty fields on turn snapshots (null players, empty actions,
//     empty phase).
//  3. Compress phase enum: "Phase_Beginning" → "begin", etc.
//  4. Hoist cardName → top-level `cd` dictionary keyed by cardId; actions
//     carry cardId only.
//  5. Drop triggered-ability actions sourced from basic lands (trivially
//     implied by the tap + cast.manaPaid signal). Covers both named basic
//     lands and tap-only empty-name lands (opponent-side MTGA data-quality
//     case).
//  6. Rename common field keys to single/two-letter forms. Action-kind keys
//     (cast/tap/move/ability/damage/resolve/statMod/target) are NOT renamed;
//     they remain readable discriminators.

// basicLandNames is the set of basic-land card names. Triggered abilities
// sourced from these cards are always the mana ability and are dropped as
// engine noise.
var basicLandNames = map[string]bool{
	"Island":   true,
	"Plains":   true,
	"Forest":   true,
	"Mountain": true,
	"Swamp":    true,
	"Wastes":   true,
}

// phaseRename compresses verbose phase enum strings.
var phaseRename = map[string]string{
	"Phase_Beginning": "begin",
	"Phase_Main1":     "main1",
	"Phase_Combat":    "combat",
	"Phase_Main2":     "main2",
	"Phase_Ending":    "end",
}

// buildV3bGameSectionData returns the v3b-compressed data for a single game,
// suitable for inclusion as the `data` field of a `game:<matchId>` section.
func buildV3bGameSectionData(game GameLog) map[string]any {
	cards := collectCardLookup(game)
	landIds := collectLandIds(game)

	turns := make([]map[string]any, 0, len(game.Turns))
	for _, turn := range game.Turns {
		turns = append(turns, buildV3bTurn(turn, landIds))
	}

	return map[string]any{
		"matchId": game.MatchID,
		"cd":      cards,
		"tn":      turns,
	}
}

// collectCardLookup walks all actions in a game and returns a cardId→cardName
// map. When the same cardId appears with multiple names (empty vs populated),
// the non-empty name wins.
func collectCardLookup(game GameLog) map[int]string {
	lookup := map[int]string{}
	consider := func(id int, name string) {
		if id == 0 {
			return
		}
		existing, exists := lookup[id]
		if !exists || (existing == "" && name != "") {
			lookup[id] = name
		}
	}
	for _, turn := range game.Turns {
		for _, a := range turn.Actions {
			switch {
			case a.Cast != nil:
				consider(a.Cast.CardID, a.Cast.CardName)
			case a.Resolve != nil:
				consider(a.Resolve.CardID, a.Resolve.CardName)
			case a.Move != nil:
				consider(a.Move.CardID, a.Move.CardName)
			case a.Tap != nil:
				consider(a.Tap.CardID, a.Tap.CardName)
			case a.Ability != nil:
				consider(a.Ability.CardID, a.Ability.CardName)
			case a.Target != nil:
				consider(a.Target.CardID, a.Target.CardName)
			case a.StatMod != nil:
				consider(a.StatMod.CardID, a.StatMod.CardName)
			case a.Damage != nil:
				consider(a.Damage.SourceID, a.Damage.Source)
			}
		}
		for _, p := range turn.Players {
			for _, perm := range p.Battlefield {
				consider(perm.CardID, perm.CardName)
			}
		}
	}
	return lookup
}

// collectLandIds returns the set of cardIds that appear as a tap source but
// never as a cast source. This captures lands, including opponent-side lands
// whose cardName the MTGA logger didn't resolve (empty-name case).
func collectLandIds(game GameLog) map[int]bool {
	tapIds := map[int]bool{}
	castIds := map[int]bool{}
	for _, turn := range game.Turns {
		for _, a := range turn.Actions {
			if a.Tap != nil {
				tapIds[a.Tap.CardID] = true
			}
			if a.Cast != nil {
				castIds[a.Cast.CardID] = true
			}
		}
	}
	lands := map[int]bool{}
	for id := range tapIds {
		if !castIds[id] {
			lands[id] = true
		}
	}
	return lands
}

// buildV3bTurn converts a TurnLog into the v3b compressed map. Null/empty
// fields are dropped; long keys are renamed; nested structures are
// recursively compressed.
func buildV3bTurn(turn TurnLog, landIds map[int]bool) map[string]any {
	out := map[string]any{
		"t":  turn.TurnNumber,
		"ap": turn.ActivePlayer,
	}
	if phase, ok := phaseRename[turn.Phase]; ok {
		out["ph"] = phase
	} else if turn.Phase != "" {
		// Unknown phase: keep as-is rather than silently drop (forward-compat
		// for any new MTGA phase enums).
		out["ph"] = turn.Phase
	}
	if len(turn.Players) > 0 {
		pl := make([]map[string]any, len(turn.Players))
		for i, p := range turn.Players {
			pl[i] = buildV3bPlayerState(p)
		}
		out["pl"] = pl
	}
	if len(turn.Actions) > 0 {
		actions := make([]map[string]any, 0, len(turn.Actions))
		for _, a := range turn.Actions {
			if converted, keep := buildV3bAction(a, landIds); keep {
				actions = append(actions, converted)
			}
		}
		if len(actions) > 0 {
			out["a"] = actions
		}
	}
	return out
}

// buildV3bPlayerState compresses a PlayerState snapshot. Empty collections
// are dropped; long keys are renamed.
func buildV3bPlayerState(p PlayerState) map[string]any {
	out := map[string]any{
		"s": p.Seat,
		"l": p.LifeTotal,
	}
	if len(p.ManaPool) > 0 {
		pool := make([]map[string]any, len(p.ManaPool))
		for i, m := range p.ManaPool {
			pool[i] = map[string]any{"k": m.Color, "n": m.Count}
		}
		out["manaPool"] = pool
	}
	if len(p.HandCards) > 0 {
		out["handCards"] = p.HandCards
	}
	if len(p.Battlefield) > 0 {
		bf := make([]map[string]any, len(p.Battlefield))
		for i, perm := range p.Battlefield {
			bf[i] = buildV3bPermanent(perm)
		}
		out["battlefield"] = bf
	}
	return out
}

// buildV3bPermanent compresses a Permanent, dropping cardName (the top-level
// `cd` dict is authoritative).
func buildV3bPermanent(perm Permanent) map[string]any {
	out := map[string]any{"c": perm.CardID}
	if len(perm.CardTypes) > 0 {
		out["ct"] = perm.CardTypes
	}
	if len(perm.SubTypes) > 0 {
		out["st"] = perm.SubTypes
	}
	if perm.Power != 0 {
		out["pw"] = perm.Power
	}
	if perm.Toughness != 0 {
		out["tf"] = perm.Toughness
	}
	if perm.IsTapped {
		out["tdb"] = perm.IsTapped
	}
	if perm.Damage != 0 {
		out["damage"] = perm.Damage
	}
	return out
}

// buildV3bAction converts one GameAction. Returns (action, true) when the
// action should be emitted; returns (nil, false) when it should be dropped
// (e.g. basic-land triggered abilities).
func buildV3bAction(a GameAction, landIds map[int]bool) (map[string]any, bool) {
	switch {
	case a.Cast != nil:
		return map[string]any{"p": a.Player, "cast": buildV3bCast(a.Cast)}, true
	case a.Resolve != nil:
		return map[string]any{"p": a.Player, "resolve": buildV3bResolve(a.Resolve)}, true
	case a.Move != nil:
		return map[string]any{"p": a.Player, "move": buildV3bMove(a.Move)}, true
	case a.Damage != nil:
		return map[string]any{"p": a.Player, "damage": buildV3bDamage(a.Damage)}, true
	case a.Tap != nil:
		return map[string]any{"p": a.Player, "tap": buildV3bTap(a.Tap)}, true
	case a.Ability != nil:
		if isBasicLandTrigger(a.Ability, landIds) {
			return nil, false
		}
		return map[string]any{"p": a.Player, "ability": buildV3bAbility(a.Ability)}, true
	case a.Target != nil:
		return map[string]any{"p": a.Player, "target": buildV3bTarget(a.Target)}, true
	case a.StatMod != nil:
		return map[string]any{"p": a.Player, "statMod": buildV3bStatMod(a.StatMod)}, true
	}
	return nil, false
}

// isBasicLandTrigger returns true when the ability should be dropped as
// engine noise: triggered abilities from cards that are either named basic
// lands or tap-only empty-name cards (opponent-side data-quality case).
func isBasicLandTrigger(a *AbilityAction, landIds map[int]bool) bool {
	if a.AbilityType != "triggered" {
		return false
	}
	if basicLandNames[a.CardName] {
		return true
	}
	if a.CardName == "" && landIds[a.CardID] {
		return true
	}
	return false
}

func buildV3bCast(c *CastAction) map[string]any {
	out := map[string]any{"c": c.CardID}
	if len(c.ManaPaid) > 0 {
		m := make([]map[string]any, len(c.ManaPaid))
		for i, e := range c.ManaPaid {
			m[i] = map[string]any{"k": e.Color, "n": e.Count}
		}
		out["m"] = m
	}
	return out
}

func buildV3bResolve(r *ResolveAction) map[string]any {
	return map[string]any{"c": r.CardID}
}

func buildV3bMove(m *MoveAction) map[string]any {
	out := map[string]any{"c": m.CardID, "mt": m.MoveType}
	if m.ZoneFrom != "" {
		out["zoneFrom"] = m.ZoneFrom
	}
	if m.ZoneTo != "" {
		out["zoneTo"] = m.ZoneTo
	}
	return out
}

func buildV3bDamage(d *DamageAction) map[string]any {
	// NB: damage.target is the damaged entity (a creature name or "player").
	// Kept as "target" for discoverability — agents searching for target=="player"
	// to find direct-player damage need a recognizable field name.
	return map[string]any{
		"src":    d.Source,
		"sid":    d.SourceID,
		"target": d.Target,
		"am":     d.Amount,
		"ic":     d.IsCombat,
	}
}

func buildV3bTap(t *TapAction) map[string]any {
	return map[string]any{"c": t.CardID, "td": t.Tapped}
}

func buildV3bAbility(a *AbilityAction) map[string]any {
	return map[string]any{"c": a.CardID, "at": a.AbilityType}
}

func buildV3bTarget(t *TargetAction) map[string]any {
	out := map[string]any{"tgs": t.Targets}
	if t.CardID != 0 {
		out["c"] = t.CardID
	}
	return out
}

func buildV3bStatMod(s *StatModAction) map[string]any {
	return map[string]any{"c": s.CardID, "pw": s.Power, "tf": s.Toughness}
}
