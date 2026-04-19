package main

import (
	"encoding/json"
	"os"
	"testing"
)

// Helper: extract the v3b-compressed map for a game section from buildOutputSections.
func gameSectionV3b(t *testing.T, gs *GameState, matchID string) map[string]any {
	t.Helper()
	sections := buildOutputSections(gs)
	key := "game:" + matchID
	section, ok := sections[key]
	if !ok {
		t.Fatalf("expected section %q", key)
	}
	secMap, ok := section.(map[string]any)
	if !ok {
		t.Fatalf("section %q is not a map", key)
	}
	data, ok := secMap["data"].(map[string]any)
	if !ok {
		t.Fatalf("section %q data is not a map (got %T)", key, secMap["data"])
	}
	return data
}

// Helper: first action from first turn in the v3b data.
func firstAction(t *testing.T, data map[string]any) map[string]any {
	t.Helper()
	turns, ok := data["tn"].([]map[string]any)
	if !ok || len(turns) == 0 {
		t.Fatalf("expected non-empty tn array, got %T", data["tn"])
	}
	actions, ok := turns[0]["a"].([]map[string]any)
	if !ok || len(actions) == 0 {
		t.Fatalf("expected non-empty actions on first turn, got %T", turns[0]["a"])
	}
	return actions[0]
}

func TestGameSectionV3b_DropsTypeField(t *testing.T) {
	gs := &GameState{
		GameLogs: &GameLogSection{
			Games: []GameLog{{
				MatchID: "m1",
				Turns: []TurnLog{{
					TurnNumber: 1,
					Phase:      "Phase_Main1",
					Actions: []GameAction{{
						Player: 1,
						Type:   "cast",
						Cast:   &CastAction{CardID: 100605, CardName: "Genghis Frog", ManaPaid: []ManaEntry{{Color: "U", Count: 1}}},
					}},
				}},
			}},
		},
	}
	data := gameSectionV3b(t, gs, "m1")
	action := firstAction(t, data)
	if _, ok := action["type"]; ok {
		t.Error("action should not have a 'type' field")
	}
	if _, ok := action["cast"]; !ok {
		t.Error("action should have a 'cast' inner key")
	}
}

func TestGameSectionV3b_CompressesPhaseNames(t *testing.T) {
	cases := map[string]string{
		"Phase_Beginning": "begin",
		"Phase_Main1":     "main1",
		"Phase_Combat":    "combat",
		"Phase_Main2":     "main2",
		"Phase_Ending":    "end",
	}
	for in, want := range cases {
		gs := &GameState{
			GameLogs: &GameLogSection{
				Games: []GameLog{{
					MatchID: "m1",
					Turns: []TurnLog{{
						TurnNumber: 1,
						Phase:      in,
						Actions: []GameAction{{
							Player: 1,
							Type:   "move",
							Move:   &MoveAction{CardID: 1, CardName: "Island", MoveType: "play_land"},
						}},
					}},
				}},
			},
		}
		data := gameSectionV3b(t, gs, "m1")
		turns := data["tn"].([]map[string]any)
		if got := turns[0]["ph"]; got != want {
			t.Errorf("phase %q: want %q, got %v", in, want, got)
		}
	}
}

func TestGameSectionV3b_DropsNullPlayersAndEmptyActions(t *testing.T) {
	gs := &GameState{
		GameLogs: &GameLogSection{
			Games: []GameLog{{
				MatchID: "m1",
				Turns: []TurnLog{
					{TurnNumber: 1, Phase: "Phase_Main1", Players: nil, Actions: []GameAction{}},
					{TurnNumber: 2, Phase: "Phase_Main1", Players: []PlayerState{{Seat: 1, LifeTotal: 20}},
						Actions: []GameAction{{
							Player: 1,
							Type:   "move",
							Move:   &MoveAction{CardID: 1, CardName: "Island", MoveType: "play_land"},
						}},
					},
				},
			}},
		},
	}
	data := gameSectionV3b(t, gs, "m1")
	turns := data["tn"].([]map[string]any)
	if _, ok := turns[0]["pl"]; ok {
		t.Error("turn 0 should not have 'pl' key when players is nil")
	}
	if _, ok := turns[0]["a"]; ok {
		t.Error("turn 0 should not have 'a' key when actions is empty")
	}
	if _, ok := turns[1]["pl"]; !ok {
		t.Error("turn 1 should have 'pl' key when players is populated")
	}
	if _, ok := turns[1]["a"]; !ok {
		t.Error("turn 1 should have 'a' key when actions is populated")
	}
}

func TestGameSectionV3b_DropsEmptyPhase(t *testing.T) {
	gs := &GameState{
		GameLogs: &GameLogSection{
			Games: []GameLog{{
				MatchID: "m1",
				Turns: []TurnLog{
					{TurnNumber: 0, Phase: "", Players: []PlayerState{{Seat: 1, LifeTotal: 20}}},
				},
			}},
		},
	}
	data := gameSectionV3b(t, gs, "m1")
	turns := data["tn"].([]map[string]any)
	if _, ok := turns[0]["ph"]; ok {
		t.Error("should not emit 'ph' key when phase is empty")
	}
}

func TestGameSectionV3b_CardDictionary(t *testing.T) {
	gs := &GameState{
		GameLogs: &GameLogSection{
			Games: []GameLog{{
				MatchID: "m1",
				Turns: []TurnLog{{
					TurnNumber: 1,
					Phase:      "Phase_Main1",
					Actions: []GameAction{
						{Player: 1, Type: "cast", Cast: &CastAction{CardID: 100605, CardName: "Genghis Frog"}},
						{Player: 1, Type: "cast", Cast: &CastAction{CardID: 100605, CardName: "Genghis Frog"}},
						{Player: 1, Type: "move", Move: &MoveAction{CardID: 100649, CardName: "Island", MoveType: "play_land"}},
					},
				}},
			}},
		},
	}
	data := gameSectionV3b(t, gs, "m1")
	cd, ok := data["cd"].(map[int]string)
	if !ok {
		t.Fatalf("expected cd to be map[int]string, got %T", data["cd"])
	}
	if cd[100605] != "Genghis Frog" {
		t.Errorf("cd[100605]: want 'Genghis Frog', got %q", cd[100605])
	}
	if cd[100649] != "Island" {
		t.Errorf("cd[100649]: want 'Island', got %q", cd[100649])
	}
	// Actions must carry cardId only, not cardName
	turns := data["tn"].([]map[string]any)
	actions := turns[0]["a"].([]map[string]any)
	for i, a := range actions {
		cast, isCast := a["cast"].(map[string]any)
		move, isMove := a["move"].(map[string]any)
		var inner map[string]any
		switch {
		case isCast:
			inner = cast
		case isMove:
			inner = move
		default:
			t.Fatalf("action %d has unexpected shape", i)
		}
		if _, ok := inner["cardName"]; ok {
			t.Errorf("action %d inner object should not have cardName", i)
		}
		if _, ok := inner["c"]; !ok {
			t.Errorf("action %d inner object should have 'c' (cardId)", i)
		}
	}
}

func TestGameSectionV3b_CardDictPrefersNonEmptyName(t *testing.T) {
	// Real-world case: same cardId appears once with empty name (opponent's land
	// seen via shuffle) and once with a populated name. Prefer non-empty.
	gs := &GameState{
		GameLogs: &GameLogSection{
			Games: []GameLog{{
				MatchID: "m1",
				Turns: []TurnLog{{
					TurnNumber: 1,
					Phase:      "Phase_Main1",
					Actions: []GameAction{
						{Player: 2, Type: "tap", Tap: &TapAction{CardID: 100661, CardName: "", Tapped: true}},
						{Player: 2, Type: "move", Move: &MoveAction{CardID: 100661, CardName: "Forest", MoveType: "play_land"}},
					},
				}},
			}},
		},
	}
	data := gameSectionV3b(t, gs, "m1")
	cd := data["cd"].(map[int]string)
	if cd[100661] != "Forest" {
		t.Errorf("cd[100661]: want 'Forest' (non-empty preferred), got %q", cd[100661])
	}
}

func TestGameSectionV3b_DropsBasicLandTriggersByName(t *testing.T) {
	gs := &GameState{
		GameLogs: &GameLogSection{
			Games: []GameLog{{
				MatchID: "m1",
				Turns: []TurnLog{{
					TurnNumber: 1,
					Phase:      "Phase_Main1",
					Actions: []GameAction{
						{Player: 1, Type: "tap", Tap: &TapAction{CardID: 100649, CardName: "Island", Tapped: true}},
						{Player: 1, Type: "ability", Ability: &AbilityAction{CardID: 100649, CardName: "Island", AbilityType: "triggered"}},
						{Player: 1, Type: "cast", Cast: &CastAction{CardID: 100605, CardName: "Genghis Frog", ManaPaid: []ManaEntry{{Color: "U", Count: 1}}}},
					},
				}},
			}},
		},
	}
	data := gameSectionV3b(t, gs, "m1")
	turns := data["tn"].([]map[string]any)
	actions := turns[0]["a"].([]map[string]any)
	// Expect: tap, cast. NOT the triggered ability.
	if len(actions) != 2 {
		t.Fatalf("expected 2 actions (tap, cast), got %d", len(actions))
	}
	if _, ok := actions[0]["tap"]; !ok {
		t.Error("first action should be tap")
	}
	if _, ok := actions[1]["cast"]; !ok {
		t.Error("second action should be cast (ability dropped)")
	}
}

func TestGameSectionV3b_DropsBasicLandTriggersByTapHeuristic(t *testing.T) {
	// cardId 100661 has empty cardName everywhere, but is only ever tapped
	// (never cast) — this is MTGA's opponent-side data-quality case.
	// Its triggered abilities should still be dropped.
	gs := &GameState{
		GameLogs: &GameLogSection{
			Games: []GameLog{{
				MatchID: "m1",
				Turns: []TurnLog{{
					TurnNumber: 1,
					Phase:      "Phase_Main1",
					Actions: []GameAction{
						{Player: 2, Type: "tap", Tap: &TapAction{CardID: 100661, CardName: "", Tapped: true}},
						{Player: 2, Type: "ability", Ability: &AbilityAction{CardID: 100661, CardName: "", AbilityType: "triggered"}},
					},
				}},
			}},
		},
	}
	data := gameSectionV3b(t, gs, "m1")
	turns := data["tn"].([]map[string]any)
	actions := turns[0]["a"].([]map[string]any)
	if len(actions) != 1 {
		t.Fatalf("expected 1 action (tap only), got %d", len(actions))
	}
	if _, ok := actions[0]["ability"]; ok {
		t.Error("triggered ability on tap-only empty-name land should have been dropped")
	}
}

func TestGameSectionV3b_KeepsNonLandTriggers(t *testing.T) {
	// Utrom Scientists has a triggered ability AND is a creature that gets cast.
	// Its triggered abilities must be preserved.
	gs := &GameState{
		GameLogs: &GameLogSection{
			Games: []GameLog{{
				MatchID: "m1",
				Turns: []TurnLog{{
					TurnNumber: 1,
					Phase:      "Phase_Main1",
					Actions: []GameAction{
						{Player: 2, Type: "cast", Cast: &CastAction{CardID: 100513, CardName: "Utrom Scientists"}},
						{Player: 2, Type: "ability", Ability: &AbilityAction{CardID: 100513, CardName: "Utrom Scientists", AbilityType: "triggered"}},
					},
				}},
			}},
		},
	}
	data := gameSectionV3b(t, gs, "m1")
	turns := data["tn"].([]map[string]any)
	actions := turns[0]["a"].([]map[string]any)
	if len(actions) != 2 {
		t.Fatalf("expected 2 actions, got %d", len(actions))
	}
	if _, ok := actions[1]["ability"]; !ok {
		t.Error("triggered ability on non-land (cast as spell) should be preserved")
	}
}

func TestGameSectionV3b_KeepsActivatedAbilitiesOnLands(t *testing.T) {
	// Activated abilities on lands (like utility lands) are rare but meaningful
	// player choices and must be preserved.
	gs := &GameState{
		GameLogs: &GameLogSection{
			Games: []GameLog{{
				MatchID: "m1",
				Turns: []TurnLog{{
					TurnNumber: 1,
					Phase:      "Phase_Main1",
					Actions: []GameAction{
						{Player: 1, Type: "tap", Tap: &TapAction{CardID: 100649, CardName: "Island", Tapped: true}},
						{Player: 1, Type: "ability", Ability: &AbilityAction{CardID: 100649, CardName: "Island", AbilityType: "activated"}},
					},
				}},
			}},
		},
	}
	data := gameSectionV3b(t, gs, "m1")
	turns := data["tn"].([]map[string]any)
	actions := turns[0]["a"].([]map[string]any)
	if len(actions) != 2 {
		t.Fatalf("expected 2 actions (activated ability preserved), got %d", len(actions))
	}
}

func TestGameSectionV3b_RenameMap(t *testing.T) {
	// Verify the full rename map is applied to representative action kinds.
	gs := &GameState{
		GameLogs: &GameLogSection{
			Games: []GameLog{{
				MatchID: "m1",
				Turns: []TurnLog{{
					TurnNumber:   5,
					ActivePlayer: 2,
					Phase:        "Phase_Main1",
					Players:      []PlayerState{{Seat: 1, LifeTotal: 20}, {Seat: 2, LifeTotal: 18}},
					Actions: []GameAction{
						{Player: 1, Type: "cast", Cast: &CastAction{CardID: 100605, CardName: "Genghis Frog", ManaPaid: []ManaEntry{{Color: "U", Count: 1}, {Color: "G", Count: 1}}}},
						{Player: 1, Type: "damage", Damage: &DamageAction{Source: "Genghis Frog", SourceID: 100605, Target: "player", Amount: 1, IsCombat: true}},
						{Player: 2, Type: "stat_mod", StatMod: &StatModAction{CardID: 100503, CardName: "Mondo Gecko", Power: 1, Toughness: 1}},
						{Player: 1, Type: "move", Move: &MoveAction{CardID: 100649, CardName: "Island", MoveType: "play_land"}},
						{Player: 1, Type: "tap", Tap: &TapAction{CardID: 100649, CardName: "Island", Tapped: true}},
						{Player: 1, Type: "ability", Ability: &AbilityAction{CardID: 100513, CardName: "Utrom Scientists", AbilityType: "triggered"}},
						{Player: 1, Type: "target", Target: &TargetAction{CardID: 0, CardName: "", Targets: []string{"Genghis Frog"}}},
						{Player: 1, Type: "resolve", Resolve: &ResolveAction{CardID: 100605, CardName: "Genghis Frog"}},
					},
				}},
			}},
		},
	}
	data := gameSectionV3b(t, gs, "m1")
	turns := data["tn"].([]map[string]any)
	turn := turns[0]

	// Top-level turn fields renamed
	if turn["t"] != 5 {
		t.Errorf("expected t=5 (turnNumber), got %v", turn["t"])
	}
	if turn["ap"] != 2 {
		t.Errorf("expected ap=2 (activePlayer), got %v", turn["ap"])
	}
	if turn["ph"] != "main1" {
		t.Errorf("expected ph='main1', got %v", turn["ph"])
	}
	pl := turn["pl"].([]map[string]any)
	if pl[0]["s"] != 1 || pl[0]["l"] != 20 {
		t.Errorf("players[0] should have s=1, l=20, got %v", pl[0])
	}

	actions := turn["a"].([]map[string]any)
	// Cast renaming: c, m, and inner m has {k, n}
	cast := actions[0]["cast"].(map[string]any)
	if cast["c"] != 100605 {
		t.Errorf("cast.c: want 100605, got %v", cast["c"])
	}
	mana := cast["m"].([]map[string]any)
	if mana[0]["k"] != "U" || mana[0]["n"] != 1 {
		t.Errorf("mana[0]: want {k:U, n:1}, got %v", mana[0])
	}
	// Player key is renamed to p
	if actions[0]["p"] != 1 {
		t.Errorf("action.p: want 1, got %v", actions[0]["p"])
	}

	// Damage renaming: src, sid, am, ic
	dmg := actions[1]["damage"].(map[string]any)
	if dmg["src"] != "Genghis Frog" {
		t.Errorf("damage.src: want 'Genghis Frog', got %v", dmg["src"])
	}
	if dmg["sid"] != 100605 {
		t.Errorf("damage.sid: want 100605, got %v", dmg["sid"])
	}
	if dmg["am"] != 1 {
		t.Errorf("damage.am: want 1, got %v", dmg["am"])
	}
	if dmg["ic"] != true {
		t.Errorf("damage.ic: want true, got %v", dmg["ic"])
	}

	// StatMod renaming: c, pw, tf
	sm := actions[2]["statMod"].(map[string]any)
	if sm["c"] != 100503 {
		t.Errorf("statMod.c: want 100503, got %v", sm["c"])
	}
	if sm["pw"] != 1 || sm["tf"] != 1 {
		t.Errorf("statMod pw/tf: want 1/1, got %v/%v", sm["pw"], sm["tf"])
	}

	// Move renaming: c, mt
	mv := actions[3]["move"].(map[string]any)
	if mv["c"] != 100649 {
		t.Errorf("move.c: want 100649, got %v", mv["c"])
	}
	if mv["mt"] != "play_land" {
		t.Errorf("move.mt: want 'play_land', got %v", mv["mt"])
	}

	// Tap renaming: c, td
	tap := actions[4]["tap"].(map[string]any)
	if tap["c"] != 100649 {
		t.Errorf("tap.c: want 100649, got %v", tap["c"])
	}
	if tap["td"] != true {
		t.Errorf("tap.td: want true, got %v", tap["td"])
	}

	// Ability renaming: c, at
	ab := actions[5]["ability"].(map[string]any)
	if ab["c"] != 100513 {
		t.Errorf("ability.c: want 100513, got %v", ab["c"])
	}
	if ab["at"] != "triggered" {
		t.Errorf("ability.at: want 'triggered', got %v", ab["at"])
	}

	// Target renaming: tgs
	tgt := actions[6]["target"].(map[string]any)
	tgs := tgt["tgs"].([]string)
	if len(tgs) != 1 || tgs[0] != "Genghis Frog" {
		t.Errorf("target.tgs: want ['Genghis Frog'], got %v", tgs)
	}

	// Resolve: c only (cardName dropped)
	res := actions[7]["resolve"].(map[string]any)
	if res["c"] != 100605 {
		t.Errorf("resolve.c: want 100605, got %v", res["c"])
	}
	if _, ok := res["cardName"]; ok {
		t.Error("resolve should not retain cardName")
	}
}

func TestGameSectionV3b_ActionKindKeysNotRenamed(t *testing.T) {
	// Action kind keys (cast, tap, move, etc.) must remain as discriminators.
	gs := &GameState{
		GameLogs: &GameLogSection{
			Games: []GameLog{{
				MatchID: "m1",
				Turns: []TurnLog{{
					TurnNumber: 1,
					Phase:      "Phase_Main1",
					Actions: []GameAction{
						{Player: 1, Type: "cast", Cast: &CastAction{CardID: 1, CardName: "X"}},
						{Player: 1, Type: "tap", Tap: &TapAction{CardID: 1, CardName: "X", Tapped: true}},
					},
				}},
			}},
		},
	}
	data := gameSectionV3b(t, gs, "m1")
	turns := data["tn"].([]map[string]any)
	actions := turns[0]["a"].([]map[string]any)
	if _, ok := actions[0]["cast"]; !ok {
		t.Error("expected 'cast' key (not renamed)")
	}
	if _, ok := actions[1]["tap"]; !ok {
		t.Error("expected 'tap' key (not renamed)")
	}
}

func TestGameSectionV3b_OnlyAffectsGameSections(t *testing.T) {
	// Verify that player_summary, match:, deck: sections are not affected
	// by the v3b changes.
	gs := &GameState{
		DisplayName: "Tester",
		ActiveDecks: &ActiveDecksSection{
			Decks: []Deck{{ID: "d1", Name: "TestDeck", Format: "Standard", Cards: []DeckCard{{ArenaID: 100605, Name: "Genghis Frog", Count: 1}}}},
		},
		Matches: &MatchHistorySection{
			Matches: []MatchResult{{MatchID: "m1", EventID: "Ranked", Opponent: MatchPlayer{Name: "Rival"}, Result: "win"}},
		},
		GameLogs: &GameLogSection{
			Games: []GameLog{{MatchID: "m1", Turns: []TurnLog{{TurnNumber: 1, Phase: "Phase_Main1"}}}},
		},
	}
	sections := buildOutputSections(gs)

	// player_summary uses long key names
	ps := sections["player_summary"].(map[string]any)
	psData := ps["data"].(map[string]any)
	if psData["display_name"] != "Tester" {
		t.Errorf("player_summary.display_name should be unchanged, got %v", psData["display_name"])
	}

	// deck section uses the full Deck struct — cardName, arenaId preserved
	deckSection := sections["deck:TestDeck"].(map[string]any)
	deckData, ok := deckSection["data"].(Deck)
	if !ok {
		t.Fatalf("deck section data should be a Deck struct, got %T", deckSection["data"])
	}
	if deckData.Cards[0].Name != "Genghis Frog" {
		t.Errorf("deck preserves cardName: got %q", deckData.Cards[0].Name)
	}

	// match section uses full MatchResult struct
	matchSection := sections["match:m1"].(map[string]any)
	if _, ok := matchSection["data"].(MatchResult); !ok {
		t.Errorf("match section data should be a MatchResult struct, got %T", matchSection["data"])
	}
}

func TestGameSectionV3b_ProductionSampleUnder40KB(t *testing.T) {
	// Load the production sample (save ea28c178, match ed5759f4, 85KB uncompressed).
	// After v3b compression, the emitted section data must be under 40KB.
	raw, err := os.ReadFile("testdata/uzimy-ed5759f4.json")
	if err != nil {
		t.Skipf("fixture not present: %v", err)
	}
	var game GameLog
	if err := json.Unmarshal(raw, &game); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	gs := &GameState{GameLogs: &GameLogSection{Games: []GameLog{game}}}
	data := gameSectionV3b(t, gs, game.MatchID)

	// Serialize with compact JSON to measure section-size impact.
	compact, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal compact: %v", err)
	}
	size := len(compact)
	t.Logf("v3b compressed size: %d bytes (%.1f KB)", size, float64(size)/1024)
	if size > 40*1024 {
		t.Errorf("expected under 40 KB, got %d bytes (%.1f KB)", size, float64(size)/1024)
	}
}
