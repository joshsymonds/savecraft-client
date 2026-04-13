package main

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestBuildAuth(t *testing.T) {
	entries := []LogEntry{{
		Label: "AuthenticateResponse",
		JSON:  json.RawMessage(`{"authenticateResponse":{"clientId":"47BADBEB1045E08A","screenName":"Aure Silvershield"}}`),
	}}
	gs := BuildGameState(entries)
	if gs.PlayerID != "47BADBEB1045E08A" {
		t.Errorf("expected playerID '47BADBEB1045E08A', got %q", gs.PlayerID)
	}
	if gs.DisplayName != "Aure Silvershield" {
		t.Errorf("expected displayName 'Aure Silvershield', got %q", gs.DisplayName)
	}
}

func TestBuildStartHookDecks(t *testing.T) {
	hookJSON := `{
		"Decks": {
			"deck-uuid-1": {
				"MainDeck": [{"cardId": 82159, "quantity": 4}, {"cardId": 82160, "quantity": 3}],
				"Sideboard": [{"cardId": 82159, "quantity": 1}],
				"CommandZone": []
			}
		},
		"DeckSummariesV2": [{
			"DeckId": "deck-uuid-1",
			"Name": "My Arena Deck",
			"Attributes": [{"name": "Format", "value": "Standard"}]
		}]
	}`
	entries := []LogEntry{{
		Arrow: "<==",
		Label: "StartHook",
		JSON:  json.RawMessage(hookJSON),
	}}
	gs := BuildGameState(entries)
	if gs.ActiveDecks == nil {
		t.Fatal("expected active_decks section")
	}
	if len(gs.ActiveDecks.Decks) != 1 {
		t.Fatalf("expected 1 deck, got %d", len(gs.ActiveDecks.Decks))
	}
	deck := gs.ActiveDecks.Decks[0]
	if deck.Name != "My Arena Deck" {
		t.Errorf("expected deck name 'My Arena Deck', got %q", deck.Name)
	}
	if deck.Format != "Standard" {
		t.Errorf("expected format 'Standard', got %q", deck.Format)
	}
	if len(deck.Cards) != 2 {
		t.Fatalf("expected 2 main deck entries, got %d", len(deck.Cards))
	}
	if deck.Cards[0].Name != "Sheoldred, the Apocalypse" {
		t.Errorf("expected first card 'Sheoldred, the Apocalypse', got %q", deck.Cards[0].Name)
	}
	if deck.Cards[0].Count != 4 {
		t.Errorf("expected first card count 4, got %d", deck.Cards[0].Count)
	}
	if len(deck.Sideboard) != 1 {
		t.Fatalf("expected 1 sideboard entry, got %d", len(deck.Sideboard))
	}
}

func TestPreconDecksFiltered(t *testing.T) {
	hookJSON := `{
		"Decks": {
			"deck-user-1": {
				"MainDeck": [{"cardId": 82159, "quantity": 4}],
				"Sideboard": [],
				"CommandZone": []
			},
			"deck-precon-1": {
				"MainDeck": [{"cardId": 82160, "quantity": 4}],
				"Sideboard": [],
				"CommandZone": []
			},
			"deck-precon-2": {
				"MainDeck": [{"cardId": 82159, "quantity": 2}],
				"Sideboard": [],
				"CommandZone": []
			}
		},
		"DeckSummariesV2": [
			{"DeckId": "deck-user-1", "Name": "[S] Landfall", "Attributes": [{"name": "Format", "value": "Standard"}]},
			{"DeckId": "deck-precon-1", "Name": "?=?Loc/Decks/Precon/Precon_EPP2024_RW", "Attributes": [{"name": "Format", "value": "Alchemy"}]},
			{"DeckId": "deck-precon-2", "Name": "?=?Loc/Decks/Precon/CC_ANB_B", "Attributes": [{"name": "Format", "value": "Alchemy"}]}
		]
	}`
	entries := []LogEntry{{
		Arrow: "<==",
		Label: "StartHook",
		JSON:  json.RawMessage(hookJSON),
	}}
	gs := BuildGameState(entries)
	if gs.ActiveDecks == nil {
		t.Fatal("expected active_decks section")
	}
	if len(gs.ActiveDecks.Decks) != 1 {
		t.Fatalf("expected 1 deck after filtering precons, got %d", len(gs.ActiveDecks.Decks))
	}
	if gs.ActiveDecks.Decks[0].Name != "[S] Landfall" {
		t.Errorf("expected user deck '[S] Landfall', got %q", gs.ActiveDecks.Decks[0].Name)
	}
}

func TestBuildStartHookInventory(t *testing.T) {
	// CollationId is an integer in real MTGA logs, not a string.
	hookJSON := `{
		"InventoryInfo": {
			"Gems": 5050,
			"Gold": 63155,
			"WildCardCommons": 1397,
			"WildCardUnCommons": 1596,
			"WildCardRares": 183,
			"WildCardMythics": 90,
			"TotalVaultProgress": 377,
			"CustomTokens": {"DraftToken": 11, "SealedToken": 0, "PlayInToken": 5},
			"Boosters": [{"CollationId": 12345, "SetCode": "TMT"}]
		}
	}`
	entries := []LogEntry{{
		Arrow: "<==",
		Label: "StartHook",
		JSON:  json.RawMessage(hookJSON),
	}}
	gs := BuildGameState(entries)
	if gs.Inventory == nil {
		t.Fatal("expected inventory section")
	}
	if gs.Inventory.Gold != 63155 {
		t.Errorf("expected gold 63155, got %d", gs.Inventory.Gold)
	}
	if gs.Inventory.Gems != 5050 {
		t.Errorf("expected gems 5050, got %d", gs.Inventory.Gems)
	}
	if gs.Inventory.WCRare != 183 {
		t.Errorf("expected wcRare 183, got %d", gs.Inventory.WCRare)
	}
	if gs.Inventory.DraftTokens != 11 {
		t.Errorf("expected draftTokens 11, got %d", gs.Inventory.DraftTokens)
	}
	if len(gs.Inventory.Boosters) != 1 {
		t.Fatalf("expected 1 booster entry, got %d", len(gs.Inventory.Boosters))
	}
	if gs.Inventory.Boosters[0].CollationID != 12345 {
		t.Errorf("expected booster collationId 12345, got %d", gs.Inventory.Boosters[0].CollationID)
	}
	if gs.Inventory.Boosters[0].SetCode != "TMT" {
		t.Errorf("expected booster set 'TMT', got %q", gs.Inventory.Boosters[0].SetCode)
	}
	if gs.Inventory.Boosters[0].Count != 1 {
		t.Errorf("expected booster count 1 (single entry), got %d", gs.Inventory.Boosters[0].Count)
	}
}

func TestBoosterAggregation(t *testing.T) {
	// Real MTGA logs have one entry per booster (no Count field).
	// Multiple entries with same CollationId should aggregate.
	hookJSON := `{
		"InventoryInfo": {
			"Gems": 0, "Gold": 0,
			"WildCardCommons": 0, "WildCardUnCommons": 0, "WildCardRares": 0, "WildCardMythics": 0,
			"TotalVaultProgress": 0,
			"Boosters": [
				{"CollationId": 400058, "SetCode": "Y26ECL"},
				{"CollationId": 400058, "SetCode": "Y26ECL"},
				{"CollationId": 400058, "SetCode": "Y26ECL"},
				{"CollationId": 900980}
			]
		}
	}`
	entries := []LogEntry{{
		Arrow: "<==",
		Label: "StartHook",
		JSON:  json.RawMessage(hookJSON),
	}}
	gs := BuildGameState(entries)
	if gs.Inventory == nil {
		t.Fatal("expected inventory section")
	}
	if len(gs.Inventory.Boosters) != 2 {
		t.Fatalf("expected 2 aggregated booster entries, got %d", len(gs.Inventory.Boosters))
	}
	// Find the Y26ECL entry
	var found bool
	for _, b := range gs.Inventory.Boosters {
		if b.CollationID == 400058 {
			if b.Count != 3 {
				t.Errorf("expected count 3 for collation 400058, got %d", b.Count)
			}
			if b.SetCode != "Y26ECL" {
				t.Errorf("expected set 'Y26ECL', got %q", b.SetCode)
			}
			found = true
		}
		if b.CollationID == 900980 {
			if b.Count != 1 {
				t.Errorf("expected count 1 for collation 900980, got %d", b.Count)
			}
		}
	}
	if !found {
		t.Error("expected booster entry for collation 400058")
	}
}

func TestBuildInventoryFromDraftResponse(t *testing.T) {
	// DTO_InventoryInfo appears in draft pick responses.
	draftJSON := `{
		"CurrentModule": "BotDraft",
		"Payload": "{}",
		"DTO_InventoryInfo": {
			"Gems": 4000,
			"Gold": 50000,
			"WildCardCommons": 100,
			"WildCardUnCommons": 200,
			"WildCardRares": 50,
			"WildCardMythics": 10,
			"TotalVaultProgress": 100,
			"Boosters": []
		}
	}`
	entries := []LogEntry{{
		Arrow: "<==",
		Label: "BotDraftDraftPick",
		JSON:  json.RawMessage(draftJSON),
	}}
	gs := BuildGameState(entries)
	if gs.Inventory == nil {
		t.Fatal("expected inventory from DTO_InventoryInfo")
	}
	if gs.Inventory.Gems != 4000 {
		t.Errorf("expected gems 4000, got %d", gs.Inventory.Gems)
	}
	if gs.Inventory.Gold != 50000 {
		t.Errorf("expected gold 50000, got %d", gs.Inventory.Gold)
	}
}

func TestBuildRank(t *testing.T) {
	rankJSON := `{
		"constructedClass": "Gold",
		"constructedLevel": 4,
		"constructedStep": 5,
		"constructedMatchesWon": 18,
		"constructedMatchesLost": 14,
		"limitedLevel": 4
	}`
	entries := []LogEntry{{
		Arrow: "<==",
		Label: "RankGetCombinedRankInfo",
		JSON:  json.RawMessage(rankJSON),
	}}
	gs := BuildGameState(entries)
	if gs.Rank == nil {
		t.Fatal("expected rank section")
	}
	if gs.Rank.Constructed.Class != "Gold" {
		t.Errorf("expected constructed class 'Gold', got %q", gs.Rank.Constructed.Class)
	}
	if gs.Rank.Constructed.Level != 4 {
		t.Errorf("expected constructed level 4, got %d", gs.Rank.Constructed.Level)
	}
}

func TestRankNormalizationBronze(t *testing.T) {
	// When limitedClass is empty but level > 0, normalize to "Bronze".
	rankJSON := `{
		"constructedClass": "Gold",
		"constructedLevel": 4,
		"limitedClass": "",
		"limitedLevel": 3
	}`
	entries := []LogEntry{{
		Arrow: "<==",
		Label: "RankGetCombinedRankInfo",
		JSON:  json.RawMessage(rankJSON),
	}}
	gs := BuildGameState(entries)
	if gs.Rank == nil {
		t.Fatal("expected rank section")
	}
	if gs.Rank.Limited.Class != "Bronze" {
		t.Errorf("expected limited class 'Bronze', got %q", gs.Rank.Limited.Class)
	}
	if gs.Rank.Constructed.Class != "Gold" {
		t.Errorf("expected constructed class 'Gold', got %q", gs.Rank.Constructed.Class)
	}
}

func TestRankNormalizationEmptyZeroLevel(t *testing.T) {
	// When class is empty and level is 0, leave empty (truly unranked).
	rankJSON := `{
		"constructedClass": "Gold",
		"constructedLevel": 4,
		"limitedClass": "",
		"limitedLevel": 0
	}`
	entries := []LogEntry{{
		Arrow: "<==",
		Label: "RankGetCombinedRankInfo",
		JSON:  json.RawMessage(rankJSON),
	}}
	gs := BuildGameState(entries)
	if gs.Rank == nil {
		t.Fatal("expected rank section")
	}
	if gs.Rank.Limited.Class != "" {
		t.Errorf("expected empty limited class for level 0, got %q", gs.Rank.Limited.Class)
	}
}

func TestBuildMatchHistory(t *testing.T) {
	// eventId lives inside reservedPlayers[], not at gameRoomConfig level — matches real MTGA JSON.
	entries := []LogEntry{
		{Label: "AuthenticateResponse", JSON: json.RawMessage(`{"authenticateResponse":{"clientId":"player1","screenName":"Me"}}`)},
		{Label: "MatchGameRoomStateChangedEvent", JSON: json.RawMessage(`{
			"timestamp": "1774191789619",
			"matchGameRoomStateChangedEvent": {
				"gameRoomInfo": {
					"stateType": "MatchGameRoomStateType_Playing",
					"gameRoomConfig": {
						"matchId": "match-001",
						"reservedPlayers": [
							{"userId": "player1", "playerName": "Me", "systemSeatId": 1, "eventId": "QuickDraft_TMT_20260313"},
							{"userId": "opp1", "playerName": "Opponent", "systemSeatId": 2, "eventId": "QuickDraft_TMT_20260313"}
						]
					}
				}
			}
		}`)},
		{Label: "MatchGameRoomStateChangedEvent", JSON: json.RawMessage(`{
			"matchGameRoomStateChangedEvent": {
				"gameRoomInfo": {
					"stateType": "MatchGameRoomStateType_MatchCompleted",
					"gameRoomConfig": {
						"matchId": "match-001",
						"reservedPlayers": [
							{"userId": "player1", "playerName": "Me", "systemSeatId": 1, "eventId": "QuickDraft_TMT_20260313"},
							{"userId": "opp1", "playerName": "Opponent", "systemSeatId": 2, "eventId": "QuickDraft_TMT_20260313"}
						]
					},
					"finalMatchResult": {
						"matchId": "match-001",
						"resultList": [
							{"scope": "MatchScope_Game", "result": "ResultType_WinLoss", "winningTeamId": 1},
							{"scope": "MatchScope_Match", "result": "ResultType_WinLoss", "winningTeamId": 1}
						]
					}
				}
			}
		}`)},
	}
	gs := BuildGameState(entries)
	if gs.Matches == nil {
		t.Fatal("expected match_history section")
	}
	if len(gs.Matches.Matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(gs.Matches.Matches))
	}
	m := gs.Matches.Matches[0]
	if m.MatchID != "match-001" {
		t.Errorf("expected matchId 'match-001', got %q", m.MatchID)
	}
	if m.EventID != "QuickDraft_TMT_20260313" {
		t.Errorf("expected eventId 'QuickDraft_TMT_20260313', got %q", m.EventID)
	}
	if m.Result != "win" {
		t.Errorf("expected result 'win', got %q", m.Result)
	}
	if m.Opponent.Name != "Opponent" {
		t.Errorf("expected opponent name 'Opponent', got %q", m.Opponent.Name)
	}
	if m.Date != "2026-03-22T15:03:09Z" {
		t.Errorf("expected date '2026-03-22T15:03:09Z', got %q", m.Date)
	}
}

func TestBuildDraftHistory(t *testing.T) {
	// Bot draft with Payload wrapping: status → outbound pick.
	entries := []LogEntry{
		{Arrow: "<==", Label: "BotDraftDraftStatus", JSON: json.RawMessage(`{
			"CurrentModule": "BotDraft",
			"Payload": "{\"Result\":\"Success\",\"EventName\":\"QuickDraft_TMT_20260313\",\"DraftId\":\"draft-001\",\"PackNumber\":0,\"PickNumber\":0,\"DraftPack\":[\"82159\",\"82160\",\"82268\"]}"
		}`)},
		{Arrow: "==>", Label: "BotDraftDraftPick", JSON: json.RawMessage(`{
			"id": "pick-uuid",
			"request": "{\"PickInfo\":{\"CardIds\":[\"82159\"],\"PackNumber\":0,\"PickNumber\":0}}"
		}`)},
	}
	gs := BuildGameState(entries)
	if gs.Drafts == nil {
		t.Fatal("expected draft_history section")
	}
	if len(gs.Drafts.Drafts) != 1 {
		t.Fatalf("expected 1 draft, got %d", len(gs.Drafts.Drafts))
	}
	draft := gs.Drafts.Drafts[0]
	if draft.DraftType != "quick" {
		t.Errorf("expected draftType 'quick', got %q", draft.DraftType)
	}
	if len(draft.Picks) != 1 {
		t.Fatalf("expected 1 pick, got %d", len(draft.Picks))
	}
	pick := draft.Picks[0]
	if len(pick.Available) != 3 {
		t.Errorf("expected 3 available cards, got %d", len(pick.Available))
	}
	if pick.Picked != "Sheoldred, the Apocalypse" {
		t.Errorf("expected chosen 'Sheoldred, the Apocalypse', got %q", pick.Picked)
	}
}

func TestBuildGameLog(t *testing.T) {
	entries := []LogEntry{
		{Label: "AuthenticateResponse", JSON: json.RawMessage(`{"authenticateResponse":{"clientId":"player1","screenName":"Me"}}`)},
		{Label: "MatchGameRoomStateChangedEvent", JSON: json.RawMessage(`{
			"matchGameRoomStateChangedEvent": {
				"gameRoomInfo": {
					"stateType": "MatchGameRoomStateType_Playing",
					"gameRoomConfig": {
						"matchId": "match-001",
						"reservedPlayers": [
							{"userId": "player1", "playerName": "Me", "systemSeatId": 1, "eventId": "Test"},
							{"userId": "opp1", "playerName": "Opp", "systemSeatId": 2, "eventId": "Test"}
						]
					}
				}
			}
		}`)},
		{Label: "GreToClientEvent", JSON: json.RawMessage(`{
			"greToClientEvent": {
				"greToClientMessages": [{
					"type": "GREMessageType_GameStateMessage",
					"gameStateMessage": {
						"turnInfo": {
							"turnNumber": 1,
							"activePlayer": 1,
							"phase": "Phase_Main1"
						},
						"gameObjects": [
							{"instanceId": 100, "grpId": 82159, "zoneId": 1, "ownerSeatId": 1, "visibility": "Visibility_Public"}
						],
						"annotations": [{
							"id": 1,
							"affectorId": 100,
							"affectedIds": [100],
							"type": ["AnnotationType_ZoneTransfer"],
							"details": [
								{"key": "zone_src", "type": "string", "valueString": ["ZoneType_Hand"]},
								{"key": "zone_dest", "type": "string", "valueString": ["ZoneType_Stack"]},
								{"key": "category", "type": "string", "valueString": ["CastSpell"]}
							]
						}]
					}
				}]
			}
		}`)},
	}
	gs := BuildGameState(entries)
	if gs.GameLogs == nil {
		t.Fatal("expected game_log section")
	}
	if len(gs.GameLogs.Games) != 1 {
		t.Fatalf("expected 1 game, got %d", len(gs.GameLogs.Games))
	}
	game := gs.GameLogs.Games[0]
	if game.MatchID != "match-001" {
		t.Errorf("expected matchId 'match-001', got %q", game.MatchID)
	}
	if len(game.Turns) == 0 {
		t.Fatal("expected at least 1 turn")
	}
	turn := game.Turns[0]
	if turn.TurnNumber != 1 {
		t.Errorf("expected turn 1, got %d", turn.TurnNumber)
	}
	if len(turn.Actions) == 0 {
		t.Fatal("expected at least 1 action")
	}
	action := turn.Actions[0]
	if action.Type != "cast" {
		t.Errorf("expected action type 'cast', got %q", action.Type)
	}
	if action.Cast == nil {
		t.Fatal("expected Cast subtype to be non-nil")
	}
	if action.Cast.CardName != "Sheoldred, the Apocalypse" {
		t.Errorf("expected card 'Sheoldred, the Apocalypse', got %q", action.Cast.CardName)
	}
	if action.Cast.CardID != 82159 {
		t.Errorf("expected cardId 82159, got %d", action.Cast.CardID)
	}
}

func TestCrossMessageObjectResolution(t *testing.T) {
	// Object appears in message 1, annotation referencing it appears in message 2.
	// Persistent object registry should resolve the object across messages.
	entries := []LogEntry{
		{Label: "AuthenticateResponse", JSON: json.RawMessage(`{"authenticateResponse":{"clientId":"player1","screenName":"Me"}}`)},
		{Label: "MatchGameRoomStateChangedEvent", JSON: json.RawMessage(`{
			"matchGameRoomStateChangedEvent": {
				"gameRoomInfo": {
					"stateType": "MatchGameRoomStateType_Playing",
					"gameRoomConfig": {
						"matchId": "match-001",
						"reservedPlayers": [
							{"userId": "player1", "playerName": "Me", "systemSeatId": 1, "eventId": "Test"},
							{"userId": "opp1", "playerName": "Opp", "systemSeatId": 2, "eventId": "Test"}
						]
					}
				}
			}
		}`)},
		// Message 1: introduces game object 100 (Sheoldred)
		{Label: "GreToClientEvent", JSON: json.RawMessage(`{
			"greToClientEvent": {
				"greToClientMessages": [{
					"type": "GREMessageType_GameStateMessage",
					"gameStateMessage": {
						"turnInfo": {"turnNumber": 1, "activePlayer": 1, "phase": "Phase_Main1"},
						"gameObjects": [
							{"instanceId": 100, "grpId": 82159, "zoneId": 1, "ownerSeatId": 1, "visibility": "Visibility_Public"}
						]
					}
				}]
			}
		}`)},
		// Message 2: annotation references object 100 but does NOT include it in gameObjects
		{Label: "GreToClientEvent", JSON: json.RawMessage(`{
			"greToClientEvent": {
				"greToClientMessages": [{
					"type": "GREMessageType_GameStateMessage",
					"gameStateMessage": {
						"turnInfo": {"turnNumber": 1, "activePlayer": 1, "phase": "Phase_Main1"},
						"annotations": [{
							"id": 10,
							"affectorId": 100,
							"affectedIds": [100],
							"type": ["AnnotationType_ResolutionComplete"]
						}]
					}
				}]
			}
		}`)},
	}
	gs := BuildGameState(entries)
	if gs.GameLogs == nil || len(gs.GameLogs.Games) == 0 {
		t.Fatal("expected game_log section")
	}
	game := gs.GameLogs.Games[0]

	// Find the resolve action — it should exist because the persistent registry
	// resolves object 100 even though it's not in message 2's gameObjects.
	var found bool
	for _, turn := range game.Turns {
		for _, action := range turn.Actions {
			if action.Type == "resolve" && action.Resolve != nil {
				if action.Resolve.CardName != "Sheoldred, the Apocalypse" {
					t.Errorf("expected resolved card 'Sheoldred, the Apocalypse', got %q", action.Resolve.CardName)
				}
				found = true
			}
		}
	}
	if !found {
		t.Error("expected a resolve action from cross-message object resolution, got none")
	}
}

func TestZoneTransferFallsBackToAffectedIds(t *testing.T) {
	// When affectorId is 0 (system) or a player seat, the card being moved
	// is in affectedIds. The parser should fall back to affectedIds.
	entries := []LogEntry{
		{Label: "AuthenticateResponse", JSON: json.RawMessage(`{"authenticateResponse":{"clientId":"player1","screenName":"Me"}}`)},
		{Label: "MatchGameRoomStateChangedEvent", JSON: json.RawMessage(`{
			"matchGameRoomStateChangedEvent": {
				"gameRoomInfo": {
					"stateType": "MatchGameRoomStateType_Playing",
					"gameRoomConfig": {
						"matchId": "match-001",
						"reservedPlayers": [
							{"userId": "player1", "playerName": "Me", "systemSeatId": 1, "eventId": "Test"},
							{"userId": "opp1", "playerName": "Opp", "systemSeatId": 2, "eventId": "Test"}
						]
					}
				}
			}
		}`)},
		{Label: "GreToClientEvent", JSON: json.RawMessage(`{
			"greToClientEvent": {
				"greToClientMessages": [{
					"type": "GREMessageType_GameStateMessage",
					"gameStateMessage": {
						"turnInfo": {"turnNumber": 1, "activePlayer": 1, "phase": "Phase_Beginning"},
						"gameObjects": [
							{"instanceId": 100, "grpId": 82159, "zoneId": 1, "ownerSeatId": 1, "visibility": "Visibility_Public"}
						],
						"annotations": [{
							"id": 1,
							"affectorId": 0,
							"affectedIds": [100],
							"type": ["AnnotationType_ZoneTransfer"],
							"details": [
								{"key": "zone_src", "type": "string", "valueString": ["ZoneType_Library"]},
								{"key": "zone_dest", "type": "string", "valueString": ["ZoneType_Hand"]},
								{"key": "category", "type": "string", "valueString": ["Draw"]}
							]
						}]
					}
				}]
			}
		}`)},
	}
	gs := BuildGameState(entries)
	if gs.GameLogs == nil || len(gs.GameLogs.Games) == 0 {
		t.Fatal("expected game_log section")
	}
	game := gs.GameLogs.Games[0]
	var found bool
	for _, turn := range game.Turns {
		for _, action := range turn.Actions {
			if action.Type == "move" && action.Move != nil {
				if action.Move.CardName != "Sheoldred, the Apocalypse" {
					t.Errorf("expected card 'Sheoldred, the Apocalypse', got %q", action.Move.CardName)
				}
				if action.Move.MoveType != "draw" {
					t.Errorf("expected moveType 'draw', got %q", action.Move.MoveType)
				}
				found = true
			}
		}
	}
	if !found {
		t.Error("expected a move action from affectedIds fallback, got none")
	}
}

// greTestEntries builds log entries that set up a match and inject a GRE message
// with the given game objects and annotations. This is a test helper to avoid
// repeating the match setup boilerplate in every annotation handler test.
func greTestEntries(objects, annotations string) []LogEntry {
	return []LogEntry{
		{Label: "AuthenticateResponse", JSON: json.RawMessage(`{"authenticateResponse":{"clientId":"player1","screenName":"Me"}}`)},
		{Label: "MatchGameRoomStateChangedEvent", JSON: json.RawMessage(`{
			"matchGameRoomStateChangedEvent": {"gameRoomInfo": {
				"stateType": "MatchGameRoomStateType_Playing",
				"gameRoomConfig": {"matchId": "m1",
					"reservedPlayers": [
						{"userId": "player1", "playerName": "Me", "systemSeatId": 1, "eventId": "Test"},
						{"userId": "opp", "playerName": "Opp", "systemSeatId": 2, "eventId": "Test"}
					]}
			}}
		}`)},
		{Label: "GreToClientEvent", JSON: json.RawMessage(`{
			"greToClientEvent": {"greToClientMessages": [{"type": "GREMessageType_GameStateMessage",
				"gameStateMessage": {
					"turnInfo": {"turnNumber": 1, "activePlayer": 1, "phase": "Phase_Main1"},
					"gameObjects": [` + objects + `],
					"annotations": [` + annotations + `]
				}
			}]}
		}`)},
	}
}

// findAction searches all turns for a GameAction with the given type.
func findAction(gs *GameState, actionType string) *GameAction {
	if gs.GameLogs == nil {
		return nil
	}
	for _, game := range gs.GameLogs.Games {
		for _, turn := range game.Turns {
			for i := range turn.Actions {
				if turn.Actions[i].Type == actionType {
					return &turn.Actions[i]
				}
			}
		}
	}
	return nil
}

func TestManaPaidEnrichesCastAction(t *testing.T) {
	// ManaPaid annotations enrich the CastAction for the spell being paid for.
	// affectorId=mana source, affectedIds=[spell instance], details: color=int
	objects := `
		{"instanceId": 100, "grpId": 82159, "ownerSeatId": 1, "visibility": "Visibility_Public"},
		{"instanceId": 200, "grpId": 82160, "ownerSeatId": 1, "visibility": "Visibility_Public"}
	`
	annotations := `
		{
			"id": 1, "affectorId": 0, "affectedIds": [100],
			"type": ["AnnotationType_ZoneTransfer"],
			"details": [
				{"key": "zone_src", "valueString": ["ZoneType_Hand"]},
				{"key": "zone_dest", "valueString": ["ZoneType_Stack"]},
				{"key": "category", "valueString": ["CastSpell"]}
			]
		},
		{
			"id": 2, "affectorId": 200, "affectedIds": [100],
			"type": ["AnnotationType_ManaPaid"],
			"details": [
				{"key": "color", "type": "KeyValuePairValueType_int32", "valueInt32": [1]}
			]
		},
		{
			"id": 3, "affectorId": 200, "affectedIds": [100],
			"type": ["AnnotationType_ManaPaid"],
			"details": [
				{"key": "color", "type": "KeyValuePairValueType_int32", "valueInt32": [4]}
			]
		}
	`
	gs := BuildGameState(greTestEntries(objects, annotations))
	action := findAction(gs, "cast")
	if action == nil {
		t.Fatal("expected cast action")
	}
	if action.Cast == nil {
		t.Fatal("expected Cast subtype")
	}
	if len(action.Cast.ManaPaid) != 2 {
		t.Fatalf("expected 2 mana entries, got %d", len(action.Cast.ManaPaid))
	}
	// Check that both colors are present (White=1, Red=4)
	colors := map[string]int{}
	for _, m := range action.Cast.ManaPaid {
		colors[m.Color] += m.Count
	}
	if colors["W"] != 1 {
		t.Errorf("expected 1 White mana, got %d", colors["W"])
	}
	if colors["R"] != 1 {
		t.Errorf("expected 1 Red mana, got %d", colors["R"])
	}
}

func TestHandleDamageDealt(t *testing.T) {
	// DamageDealt: affectorId=source card, affectedIds=[target player seat or card]
	// details: damage=amount, type=1(combat)/2(non-combat)
	objects := `{"instanceId": 200, "grpId": 82159, "ownerSeatId": 1, "visibility": "Visibility_Public"}`
	annotations := `{
		"id": 1, "affectorId": 200, "affectedIds": [2],
		"type": ["AnnotationType_DamageDealt"],
		"details": [
			{"key": "damage", "type": "KeyValuePairValueType_int32", "valueInt32": [3]},
			{"key": "type", "type": "KeyValuePairValueType_int32", "valueInt32": [1]}
		]
	}`
	gs := BuildGameState(greTestEntries(objects, annotations))
	action := findAction(gs, "damage")
	if action == nil {
		t.Fatal("expected damage action")
	}
	if action.Damage == nil {
		t.Fatal("expected Damage subtype")
	}
	if action.Damage.Source != "Sheoldred, the Apocalypse" {
		t.Errorf("expected source 'Sheoldred, the Apocalypse', got %q", action.Damage.Source)
	}
	if action.Damage.Amount != 3 {
		t.Errorf("expected damage 3, got %d", action.Damage.Amount)
	}
	if !action.Damage.IsCombat {
		t.Error("expected isCombat=true for type=1")
	}
}

func TestHandleDamageDealtNonCombat(t *testing.T) {
	objects := `{"instanceId": 200, "grpId": 82159, "ownerSeatId": 1, "visibility": "Visibility_Public"}`
	annotations := `{
		"id": 1, "affectorId": 200, "affectedIds": [1],
		"type": ["AnnotationType_DamageDealt"],
		"details": [
			{"key": "damage", "type": "KeyValuePairValueType_int32", "valueInt32": [2]},
			{"key": "type", "type": "KeyValuePairValueType_int32", "valueInt32": [2]}
		]
	}`
	gs := BuildGameState(greTestEntries(objects, annotations))
	action := findAction(gs, "damage")
	if action == nil {
		t.Fatal("expected damage action")
	}
	if action.Damage.IsCombat {
		t.Error("expected isCombat=false for type=2")
	}
}

func TestHandleTapUntap(t *testing.T) {
	// TappedUntappedPermanent: affectedIds=[card being tapped], details: tapped=1/0
	objects := `{"instanceId": 200, "grpId": 82159, "ownerSeatId": 1, "visibility": "Visibility_Public"}`
	annotations := `{
		"id": 1, "affectorId": 201, "affectedIds": [200],
		"type": ["AnnotationType_TappedUntappedPermanent"],
		"details": [{"key": "tapped", "type": "KeyValuePairValueType_int32", "valueInt32": [1]}]
	}`
	gs := BuildGameState(greTestEntries(objects, annotations))
	action := findAction(gs, "tap")
	if action == nil {
		t.Fatal("expected tap action")
	}
	if action.Tap == nil {
		t.Fatal("expected Tap subtype")
	}
	if action.Tap.CardName != "Sheoldred, the Apocalypse" {
		t.Errorf("expected card 'Sheoldred, the Apocalypse', got %q", action.Tap.CardName)
	}
	if !action.Tap.Tapped {
		t.Error("expected tapped=true")
	}
}

func TestHandleTapUntapUntapped(t *testing.T) {
	objects := `{"instanceId": 200, "grpId": 82159, "ownerSeatId": 1, "visibility": "Visibility_Public"}`
	annotations := `{
		"id": 1, "affectorId": 201, "affectedIds": [200],
		"type": ["AnnotationType_TappedUntappedPermanent"],
		"details": [{"key": "tapped", "type": "KeyValuePairValueType_int32", "valueInt32": [0]}]
	}`
	gs := BuildGameState(greTestEntries(objects, annotations))
	action := findAction(gs, "tap")
	if action == nil {
		t.Fatal("expected tap action")
	}
	if action.Tap.Tapped {
		t.Error("expected tapped=false for untap")
	}
}

func TestHandleAbilityCreated(t *testing.T) {
	// AbilityInstanceCreated: affectorId=source card
	objects := `{"instanceId": 200, "grpId": 82159, "ownerSeatId": 1, "visibility": "Visibility_Public"}`
	annotations := `{
		"id": 1, "affectorId": 200, "affectedIds": [300],
		"type": ["AnnotationType_AbilityInstanceCreated"],
		"details": [{"key": "source_zone", "type": "KeyValuePairValueType_int32", "valueInt32": [28]}]
	}`
	gs := BuildGameState(greTestEntries(objects, annotations))
	action := findAction(gs, "ability")
	if action == nil {
		t.Fatal("expected ability action")
	}
	if action.Ability == nil {
		t.Fatal("expected Ability subtype")
	}
	if action.Ability.CardName != "Sheoldred, the Apocalypse" {
		t.Errorf("expected card 'Sheoldred, the Apocalypse', got %q", action.Ability.CardName)
	}
}

func TestHandleTargetSubmitted(t *testing.T) {
	// PlayerSubmittedTargets: affectedIds=target objects
	objects := `
		{"instanceId": 200, "grpId": 82159, "ownerSeatId": 1, "visibility": "Visibility_Public"},
		{"instanceId": 300, "grpId": 82160, "ownerSeatId": 2, "visibility": "Visibility_Public"}
	`
	annotations := `{
		"id": 1, "affectorId": 200, "affectedIds": [300],
		"type": ["AnnotationType_PlayerSubmittedTargets"]
	}`
	gs := BuildGameState(greTestEntries(objects, annotations))
	action := findAction(gs, "target")
	if action == nil {
		t.Fatal("expected target action")
	}
	if action.Target == nil {
		t.Fatal("expected Target subtype")
	}
	if len(action.Target.Targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(action.Target.Targets))
	}
	if action.Target.Targets[0] != "Sheoldred's Restoration" {
		t.Errorf("expected target 'Sheoldred's Restoration', got %q", action.Target.Targets[0])
	}
}

func TestHandleStatMod(t *testing.T) {
	// PowerToughnessModCreated: affectedIds=[modified card], details: power=N, toughness=N
	objects := `{"instanceId": 200, "grpId": 82159, "ownerSeatId": 1, "visibility": "Visibility_Public"}`
	annotations := `{
		"id": 1, "affectorId": 300, "affectedIds": [200],
		"type": ["AnnotationType_PowerToughnessModCreated"],
		"details": [
			{"key": "power", "type": "KeyValuePairValueType_int32", "valueInt32": [2]},
			{"key": "toughness", "type": "KeyValuePairValueType_int32", "valueInt32": [3]}
		]
	}`
	gs := BuildGameState(greTestEntries(objects, annotations))
	action := findAction(gs, "stat_mod")
	if action == nil {
		t.Fatal("expected stat_mod action")
	}
	if action.StatMod == nil {
		t.Fatal("expected StatMod subtype")
	}
	if action.StatMod.CardName != "Sheoldred, the Apocalypse" {
		t.Errorf("expected card 'Sheoldred, the Apocalypse', got %q", action.StatMod.CardName)
	}
	if action.StatMod.Power != 2 {
		t.Errorf("expected power 2, got %d", action.StatMod.Power)
	}
	if action.StatMod.Toughness != 3 {
		t.Errorf("expected toughness 3, got %d", action.StatMod.Toughness)
	}
}

func TestOpponentCardsSeen(t *testing.T) {
	entries := []LogEntry{
		{Label: "AuthenticateResponse", JSON: json.RawMessage(`{"authenticateResponse":{"clientId":"player1"}}`)},
		{Label: "MatchGameRoomStateChangedEvent", JSON: json.RawMessage(`{
			"matchGameRoomStateChangedEvent": {
				"gameRoomInfo": {
					"stateType": "MatchGameRoomStateType_Playing",
					"gameRoomConfig": {
						"matchId": "match-001",
						"reservedPlayers": [
							{"userId": "player1", "playerName": "Me", "systemSeatId": 1, "eventId": "Test"},
							{"userId": "opp1", "playerName": "Opp", "systemSeatId": 2, "eventId": "Test"}
						]
					}
				}
			}
		}`)},
		{Label: "GreToClientEvent", JSON: json.RawMessage(`{
			"greToClientEvent": {
				"greToClientMessages": [{
					"type": "GREMessageType_GameStateMessage",
					"gameStateMessage": {
						"gameObjects": [
							{"instanceId": 200, "grpId": 82159, "zoneId": 5, "ownerSeatId": 2, "visibility": "Visibility_Public"},
							{"instanceId": 201, "grpId": 82160, "zoneId": 5, "ownerSeatId": 2, "visibility": "Visibility_Public"},
							{"instanceId": 202, "grpId": 82159, "zoneId": 5, "ownerSeatId": 2, "visibility": "Visibility_Public"}
						]
					}
				}]
			}
		}`)},
	}
	gs := BuildGameState(entries)
	if gs.Matches == nil || len(gs.Matches.Matches) == 0 {
		t.Fatal("expected match data")
	}
	opp := gs.Matches.Matches[0].Opponent
	if len(opp.CardsSeen) != 2 {
		t.Fatalf("expected 2 unique opponent cards seen, got %d: %v", len(opp.CardsSeen), opp.CardsSeen)
	}
	// CardsSeen should contain resolved names and arena IDs.
	for _, cs := range opp.CardsSeen {
		if cs.ArenaID == 0 {
			t.Error("expected non-zero ArenaID in CardsSeen")
		}
		if cs.Name == "" {
			t.Errorf("expected non-empty Name in CardsSeen for ArenaID %d", cs.ArenaID)
		}
	}
}

func TestAbilityObjectsFilteredFromCardsSeen(t *testing.T) {
	entries := []LogEntry{
		{Label: "AuthenticateResponse", JSON: json.RawMessage(`{"authenticateResponse":{"clientId":"player1"}}`)},
		{Label: "MatchGameRoomStateChangedEvent", JSON: json.RawMessage(`{
			"matchGameRoomStateChangedEvent": {
				"gameRoomInfo": {
					"stateType": "MatchGameRoomStateType_Playing",
					"gameRoomConfig": {
						"matchId": "match-001",
						"reservedPlayers": [
							{"userId": "player1", "playerName": "Me", "systemSeatId": 1, "eventId": "Test"},
							{"userId": "opp1", "playerName": "Opp", "systemSeatId": 2, "eventId": "Test"}
						]
					}
				}
			}
		}`)},
		{Label: "GreToClientEvent", JSON: json.RawMessage(`{
			"greToClientEvent": {
				"greToClientMessages": [{
					"type": "GREMessageType_GameStateMessage",
					"gameStateMessage": {
						"gameObjects": [
							{"instanceId": 200, "grpId": 82159, "type": "GameObjectType_Card", "zoneId": 5, "ownerSeatId": 2, "visibility": "Visibility_Public"},
							{"instanceId": 201, "grpId": 203096, "type": "GameObjectType_Ability", "zoneId": 5, "ownerSeatId": 2, "visibility": "Visibility_Public"},
							{"instanceId": 202, "grpId": 82160, "type": "GameObjectType_Token", "zoneId": 5, "ownerSeatId": 2, "visibility": "Visibility_Public"}
						]
					}
				}]
			}
		}`)},
	}
	gs := BuildGameState(entries)
	if gs.Matches == nil || len(gs.Matches.Matches) == 0 {
		t.Fatal("expected match data")
	}
	opp := gs.Matches.Matches[0].Opponent
	// Should have 2 cards (Card + Token), not 3 (ability filtered out).
	if len(opp.CardsSeen) != 2 {
		t.Errorf("expected 2 cards seen (ability filtered), got %d: %v", len(opp.CardsSeen), opp.CardsSeen)
	}
	for _, cs := range opp.CardsSeen {
		if cs.ArenaID == 203096 {
			t.Error("ability grpId 203096 should not appear in CardsSeen")
		}
	}
}

func TestInventorySnapshotOverwritesDelta(t *testing.T) {
	// Multiple InventoryInfo snapshots should use the latest, not accumulate.
	entries := []LogEntry{
		{Arrow: "<==", Label: "StartHook", JSON: json.RawMessage(`{
			"InventoryInfo": {"Gems": 5000, "Gold": 10000, "WildCardCommons": 0, "WildCardUnCommons": 0, "WildCardRares": 0, "WildCardMythics": 0, "TotalVaultProgress": 0, "Boosters": []}
		}`)},
		{Arrow: "<==", Label: "BotDraftDraftPick", JSON: json.RawMessage(`{
			"CurrentModule": "BotDraft",
			"Payload": "{}",
			"DTO_InventoryInfo": {"Gems": 4500, "Gold": 9000, "WildCardCommons": 0, "WildCardUnCommons": 0, "WildCardRares": 0, "WildCardMythics": 0, "TotalVaultProgress": 0, "Boosters": []}
		}`)},
	}
	gs := BuildGameState(entries)
	if gs.Inventory == nil {
		t.Fatal("expected inventory section")
	}
	// Should be the latest snapshot, not accumulated.
	if gs.Inventory.Gems != 4500 {
		t.Errorf("expected gems 4500 (latest snapshot), got %d", gs.Inventory.Gems)
	}
	if gs.Inventory.Gold != 9000 {
		t.Errorf("expected gold 9000 (latest snapshot), got %d", gs.Inventory.Gold)
	}
}

func TestOutDraftPickWithUnknownCard(t *testing.T) {
	// Real data uses card IDs from newer sets not in ArenaCards.
	// resolveCardName should fall back to the ID as a string.
	statusJSON := `{"CurrentModule":"BotDraft","Payload":"{\"EventName\":\"QuickDraft_TMT\",\"PackNumber\":0,\"PickNumber\":0,\"DraftPack\":[\"999999\",\"999998\"]}"}`
	pickJSON := `{"id":"abc","request":"{\"PickInfo\":{\"CardIds\":[\"999999\"],\"PackNumber\":0,\"PickNumber\":0}}"}`

	entries := []LogEntry{
		{Arrow: "<==", Label: "BotDraftDraftStatus", JSON: json.RawMessage(statusJSON)},
		{Arrow: "==>", Label: "BotDraftDraftPick", JSON: json.RawMessage(pickJSON)},
	}
	gs := BuildGameState(entries)
	if gs.Drafts == nil {
		t.Fatal("expected drafts")
	}
	pick := gs.Drafts.Drafts[0].Picks[0]
	if pick.Picked != "999999" {
		t.Errorf("expected chosen '999999' (fallback), got %q", pick.Picked)
	}
	if pick.PickedID != 999999 {
		t.Errorf("expected chosenId 999999, got %d", pick.PickedID)
	}
}

func TestDraftPickResponseContainsNextPack(t *testing.T) {
	// Inbound BotDraftDraftPick responses contain the next pick's pack in Payload.
	entries := []LogEntry{
		{Arrow: "<==", Label: "BotDraftDraftStatus", JSON: json.RawMessage(`{"CurrentModule":"BotDraft","Payload":"{\"EventName\":\"QuickDraft\",\"PackNumber\":0,\"PickNumber\":0,\"DraftPack\":[\"82159\",\"82160\"]}"}`)},
		{Arrow: "==>", Label: "BotDraftDraftPick", JSON: json.RawMessage(`{"id":"a","request":"{\"PickInfo\":{\"CardIds\":[\"82159\"],\"PackNumber\":0,\"PickNumber\":0}}"}`)},
		{Arrow: "<==", Label: "BotDraftDraftPick", JSON: json.RawMessage(`{"CurrentModule":"BotDraft","Payload":"{\"EventName\":\"QuickDraft\",\"PackNumber\":0,\"PickNumber\":1,\"DraftPack\":[\"82160\",\"82268\"]}"}`)},
		{Arrow: "==>", Label: "BotDraftDraftPick", JSON: json.RawMessage(`{"id":"b","request":"{\"PickInfo\":{\"CardIds\":[\"82268\"],\"PackNumber\":0,\"PickNumber\":1}}"}`)},
	}
	gs := BuildGameState(entries)
	if gs.Drafts == nil {
		t.Fatal("expected drafts")
	}
	draft := gs.Drafts.Drafts[0]
	if len(draft.Picks) != 2 {
		t.Fatalf("expected 2 picks, got %d", len(draft.Picks))
	}
	// First pick from initial status
	if draft.Picks[0].Picked != "Sheoldred, the Apocalypse" {
		t.Errorf("pick 0: expected 'Sheoldred, the Apocalypse', got %q", draft.Picks[0].Picked)
	}
	// Second pick from inbound response's Payload
	if len(draft.Picks[1].Available) != 2 {
		t.Errorf("pick 1: expected 2 available, got %d", len(draft.Picks[1].Available))
	}
	if draft.Picks[1].PickedID != 82268 {
		t.Errorf("pick 1: expected chosenId 82268, got %d", draft.Picks[1].PickedID)
	}
}

func TestDuplicateBotDraftStatusDedup(t *testing.T) {
	// MTGA can emit duplicate BotDraftDraftStatus for the same pack/pick.
	// We should deduplicate, not append a second DraftPick.
	entries := []LogEntry{
		{Arrow: "<==", Label: "BotDraftDraftStatus", JSON: json.RawMessage(`{"CurrentModule":"BotDraft","Payload":"{\"EventName\":\"QuickDraft\",\"DraftId\":\"d1\",\"PackNumber\":0,\"PickNumber\":0,\"DraftPack\":[\"82159\",\"82160\"]}"}`)},
		// Duplicate status for same pack/pick (different pack order is fine — latest wins)
		{Arrow: "<==", Label: "BotDraftDraftStatus", JSON: json.RawMessage(`{"CurrentModule":"BotDraft","Payload":"{\"EventName\":\"QuickDraft\",\"DraftId\":\"d1\",\"PackNumber\":0,\"PickNumber\":0,\"DraftPack\":[\"82160\",\"82159\"]}"}`)},
		{Arrow: "==>", Label: "BotDraftDraftPick", JSON: json.RawMessage(`{"id":"a","request":"{\"PickInfo\":{\"CardIds\":[\"82159\"],\"PackNumber\":0,\"PickNumber\":0}}"}`)},
	}
	gs := BuildGameState(entries)
	if gs.Drafts == nil {
		t.Fatal("expected drafts")
	}
	draft := gs.Drafts.Drafts[0]
	if len(draft.Picks) != 1 {
		t.Fatalf("expected 1 pick (deduped), got %d", len(draft.Picks))
	}
	// Available should reflect the latest status (82160 first)
	if draft.Picks[0].Available[0].Name == "Sheoldred, the Apocalypse" {
		t.Error("expected Available to be updated to latest status (82160 first)")
	}
	if draft.Picks[0].Picked != "Sheoldred, the Apocalypse" {
		t.Errorf("expected chosen card set, got %q", draft.Picks[0].Picked)
	}
}

func TestDuplicatePremierDraftNotifyDedup(t *testing.T) {
	// Premier draft uses DraftNotify — same dedup requirement.
	entries := []LogEntry{
		{Arrow: "<==", Label: "DraftNotify", JSON: json.RawMessage(`{"draftId":"d1","SelfPack":0,"SelfPick":0,"PackCards":"82159, 82160"}`)},
		// Duplicate notify for same pack/pick
		{Arrow: "<==", Label: "DraftNotify", JSON: json.RawMessage(`{"draftId":"d1","SelfPack":0,"SelfPick":0,"PackCards":"82160, 82159"}`)},
	}
	gs := BuildGameState(entries)
	if gs.Drafts == nil {
		t.Fatal("expected drafts")
	}
	draft := gs.Drafts.Drafts[0]
	if len(draft.Picks) != 1 {
		t.Fatalf("expected 1 pick (deduped), got %d", len(draft.Picks))
	}
	// Available should reflect the latest notify (82160 first)
	if draft.Picks[0].Available[0].Name == "Sheoldred, the Apocalypse" {
		t.Error("expected Available to be updated to latest notify (82160 first)")
	}
}

func TestBuildMatchLoss(t *testing.T) {
	entries := []LogEntry{
		{Label: "AuthenticateResponse", JSON: json.RawMessage(`{"authenticateResponse":{"clientId":"player1"}}`)},
		{Label: "MatchGameRoomStateChangedEvent", JSON: json.RawMessage(`{
			"matchGameRoomStateChangedEvent": {"gameRoomInfo": {
				"stateType": "MatchGameRoomStateType_Playing",
				"gameRoomConfig": {"matchId": "m1",
					"reservedPlayers": [
						{"userId": "player1", "playerName": "Me", "systemSeatId": 1, "eventId": "Test"},
						{"userId": "opp", "playerName": "Opp", "systemSeatId": 2, "eventId": "Test"}
					]}
			}}
		}`)},
		{Label: "MatchGameRoomStateChangedEvent", JSON: json.RawMessage(`{
			"matchGameRoomStateChangedEvent": {"gameRoomInfo": {
				"stateType": "MatchGameRoomStateType_MatchCompleted",
				"gameRoomConfig": {"matchId": "m1"},
				"finalMatchResult": {"matchId": "m1", "resultList": [
					{"scope": "MatchScope_Game", "result": "ResultType_WinLoss", "winningTeamId": 2},
					{"scope": "MatchScope_Match", "result": "ResultType_WinLoss", "winningTeamId": 2}
				]}
			}}
		}`)},
	}
	gs := BuildGameState(entries)
	if gs.Matches.Matches[0].Result != "loss" {
		t.Errorf("expected 'loss', got %q", gs.Matches.Matches[0].Result)
	}
}

func TestBuildMatchDraw(t *testing.T) {
	entries := []LogEntry{
		{Label: "AuthenticateResponse", JSON: json.RawMessage(`{"authenticateResponse":{"clientId":"player1"}}`)},
		{Label: "MatchGameRoomStateChangedEvent", JSON: json.RawMessage(`{
			"matchGameRoomStateChangedEvent": {"gameRoomInfo": {
				"stateType": "MatchGameRoomStateType_Playing",
				"gameRoomConfig": {"matchId": "m1",
					"reservedPlayers": [
						{"userId": "player1", "playerName": "Me", "systemSeatId": 1, "eventId": "Test"},
						{"userId": "opp", "playerName": "Opp", "systemSeatId": 2, "eventId": "Test"}
					]}
			}}
		}`)},
		{Label: "MatchGameRoomStateChangedEvent", JSON: json.RawMessage(`{
			"matchGameRoomStateChangedEvent": {"gameRoomInfo": {
				"stateType": "MatchGameRoomStateType_MatchCompleted",
				"gameRoomConfig": {"matchId": "m1"},
				"finalMatchResult": {"matchId": "m1", "resultList": [
					{"scope": "MatchScope_Match", "result": "ResultType_Draw", "winningTeamId": 0}
				]}
			}}
		}`)},
	}
	gs := BuildGameState(entries)
	if gs.Matches.Matches[0].Result != "draw" {
		t.Errorf("expected 'draw', got %q", gs.Matches.Matches[0].Result)
	}
}

func TestFormatTimestamp(t *testing.T) {
	cases := []struct {
		input, want string
	}{
		{"1774191789619", "2026-03-22T15:03:09Z"},
		{"not-a-number", "not-a-number"},
		{"", ""},
	}
	for _, tc := range cases {
		got := formatTimestamp(tc.input)
		if got != tc.want {
			t.Errorf("formatTimestamp(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestInferAction(t *testing.T) {
	cases := []struct {
		from, to string
		want     string
	}{
		{"ZoneType_Library", "ZoneType_Hand", "draw"},
		{"ZoneType_Hand", "ZoneType_Stack", "cast"},
		{"ZoneType_Hand", "ZoneType_Battlefield", "play_land"},
		{"ZoneType_Battlefield", "ZoneType_Graveyard", "destroy"},
		{"ZoneType_Battlefield", "ZoneType_Exile", "exile"},
		{"ZoneType_Library", "ZoneType_Battlefield", "move"},
	}
	for _, tc := range cases {
		got := inferAction(tc.from, tc.to)
		if got != tc.want {
			t.Errorf("inferAction(%q, %q) = %q, want %q", tc.from, tc.to, got, tc.want)
		}
	}
}

func TestRefinePutAction(t *testing.T) {
	cases := []struct {
		zoneTo string
		want   string
	}{
		{"ZoneType_Battlefield", "put_into_play"},
		{"ZoneType_Graveyard", "put_into_graveyard"},
		{"ZoneType_Hand", "put_into_hand"},
		{"ZoneType_Library", "put_into_library"},
		{"ZoneType_Exile", "put"}, // no special subtype for exile — use generic
		{"ZoneType_Stack", "put"}, // fallback
		{"", "put"},               // empty zone
	}
	for _, tc := range cases {
		got := refinePutAction(tc.zoneTo)
		if got != tc.want {
			t.Errorf("refinePutAction(%q) = %q, want %q", tc.zoneTo, got, tc.want)
		}
	}
}

func TestBuildOutputSectionsPlayerSummary(t *testing.T) {
	gs := &GameState{
		DisplayName: "TestPlayer",
		PlayerID:    "abc123",
		Rank: &RankSection{
			Constructed: RankInfo{Class: "Gold", Level: 4, Step: 5, MatchesWon: 18, MatchesLost: 14},
			Limited:     RankInfo{Class: "Silver", Level: 2},
		},
		Inventory: &InventorySection{
			Gold: 63155, Gems: 5050,
			WCCommon: 100, WCUncommon: 200, WCRare: 50, WCMythic: 10,
			VaultProgress: 37.7,
			DraftTokens:   2,
			Boosters:      []BoosterInfo{{CollationID: 12345, SetCode: "TMT", Count: 3}},
		},
		ActiveDecks: &ActiveDecksSection{
			Decks: []Deck{
				{ID: "d1", Name: "[S] Landfall", Format: "Standard", Cards: []DeckCard{{ArenaID: 82159, Name: "Card A", Count: 4}}},
				{ID: "d2", Name: "[HB] Slivers", Format: "Brawl", Cards: []DeckCard{{ArenaID: 82160, Name: "Card B", Count: 1}}},
			},
		},
		Matches: &MatchHistorySection{
			Matches: []MatchResult{{
				MatchID: "match-001", EventID: "Ranked", Date: "2026-03-22T15:03:09Z",
				Result:   "win",
				Opponent: MatchPlayer{Name: "Opp1", Seat: 2, Rank: "Platinum", Tier: 3, CardsSeen: []CardSeen{{Name: "Sheoldred, the Apocalypse", ArenaID: 87521}}},
				Player:   MatchPlayer{Name: "TestPlayer", Seat: 1},
				Games:    []GameResult{{GameNumber: 1, WinningSeat: 1}},
			}},
		},
		GameLogs: &GameLogSection{
			Games: []GameLog{{
				MatchID: "match-001",
				Turns:   []TurnLog{{TurnNumber: 1, ActivePlayer: 1, Phase: "Phase_Main1", Actions: []GameAction{}}},
			}},
		},
		Drafts: &DraftHistorySection{
			Drafts: []DraftSession{{EventName: "QuickDraft", DraftType: "quick", Picks: []DraftPick{{PackNumber: 0, PickNumber: 0, Available: []DraftCard{{Name: "A", ID: 1}}, Picked: "A"}}}},
		},
	}

	sections := buildOutputSections(gs)

	// player_summary must exist
	ps, ok := sections["player_summary"]
	if !ok {
		t.Fatal("expected player_summary section")
	}
	psMap := ps.(map[string]any)
	data := psMap["data"].(map[string]any)
	if data["display_name"] != "TestPlayer" {
		t.Errorf("expected display_name 'TestPlayer', got %v", data["display_name"])
	}
	if data["rank"] == nil {
		t.Error("expected rank in player_summary")
	}
	if data["inventory"] == nil {
		t.Error("expected inventory in player_summary")
	}
	decks := data["decks"].([]map[string]any)
	if len(decks) != 2 {
		t.Errorf("expected 2 decks in summary, got %d", len(decks))
	}
	if decks[0]["name"] != "[S] Landfall" {
		t.Errorf("expected deck name '[S] Landfall', got %v", decks[0]["name"])
	}
	if decks[0]["format"] != "Standard" {
		t.Errorf("expected deck format 'Standard', got %v", decks[0]["format"])
	}
	// Deck summary should reference the section name
	if decks[0]["section"] != "deck:[S] Landfall" {
		t.Errorf("expected section 'deck:[S] Landfall', got %v", decks[0]["section"])
	}
	matches := data["matches"].([]map[string]any)
	if len(matches) != 1 {
		t.Errorf("expected 1 match in summary, got %d", len(matches))
	}
	if matches[0]["section"] != "match:match-001" {
		t.Errorf("expected match section 'match:match-001', got %v", matches[0]["section"])
	}
	games := data["games"].([]map[string]any)
	if len(games) != 1 {
		t.Errorf("expected 1 game index entry, got %d", len(games))
	}
	if games[0]["section"] != "game:match-001" {
		t.Errorf("expected game section 'game:match-001', got %v", games[0]["section"])
	}

	// Old sections must NOT exist
	for _, name := range []string{"active_decks", "match_history", "game_log", "rank", "inventory"} {
		if _, ok := sections[name]; ok {
			t.Errorf("section %q should not exist in new layout", name)
		}
	}

	// Per-deck sections
	if _, ok := sections["deck:[S] Landfall"]; !ok {
		t.Error("expected deck:[S] Landfall section")
	}
	if _, ok := sections["deck:[HB] Slivers"]; !ok {
		t.Error("expected deck:[HB] Slivers section")
	}

	// Per-match sections with full metadata including opponent cards
	matchSection, ok := sections["match:match-001"]
	if !ok {
		t.Error("expected match:match-001 section")
	} else {
		matchData := matchSection.(map[string]any)["data"].(MatchResult)
		if matchData.Opponent.Name != "Opp1" {
			t.Errorf("expected opponent name 'Opp1', got %q", matchData.Opponent.Name)
		}
		if matchData.Result != "win" {
			t.Errorf("expected result 'win', got %q", matchData.Result)
		}
		if matchData.Opponent.Rank != "Platinum" {
			t.Errorf("expected opponent rank 'Platinum', got %q", matchData.Opponent.Rank)
		}
		if len(matchData.Opponent.CardsSeen) != 1 {
			t.Errorf("expected 1 opponent card seen, got %d", len(matchData.Opponent.CardsSeen))
		} else if matchData.Opponent.CardsSeen[0].Name != "Sheoldred, the Apocalypse" {
			t.Errorf("expected card 'Sheoldred, the Apocalypse', got %q", matchData.Opponent.CardsSeen[0].Name)
		}
	}

	// Per-game sections
	if _, ok := sections["game:match-001"]; !ok {
		t.Error("expected game:match-001 section")
	}

	// Draft history unchanged
	if _, ok := sections["draft_history"]; !ok {
		t.Error("expected draft_history section")
	}
}

func TestBuildOutputSectionsEmpty(t *testing.T) {
	// Empty GameState should still produce player_summary
	gs := &GameState{}
	sections := buildOutputSections(gs)
	if _, ok := sections["player_summary"]; !ok {
		t.Fatal("expected player_summary even with empty state")
	}
}

func TestBuildOutputSectionsSize(t *testing.T) {
	// player_summary with many decks should stay small (no card lists)
	decks := make([]Deck, 80)
	for i := range decks {
		cards := make([]DeckCard, 60)
		for j := range cards {
			cards[j] = DeckCard{ArenaID: 80000 + j, Name: "Some Card Name Here", Count: 4}
		}
		decks[i] = Deck{ID: fmt.Sprintf("d%d", i), Name: fmt.Sprintf("[HB] Deck %d", i), Format: "Brawl", Cards: cards}
	}
	gs := &GameState{
		DisplayName: "TestPlayer",
		ActiveDecks: &ActiveDecksSection{Decks: decks},
	}
	sections := buildOutputSections(gs)
	psMap := sections["player_summary"].(map[string]any)
	psJSON, _ := json.Marshal(psMap["data"])
	if len(psJSON) > 15*1024 {
		t.Errorf("player_summary is %d bytes, expected < 15KB", len(psJSON))
	}
}

func TestBuildOutputSectionsGameWithoutMatch(t *testing.T) {
	// Game log entry with no corresponding match should still appear in game index
	// but without opponent/result fields.
	gs := &GameState{
		GameLogs: &GameLogSection{
			Games: []GameLog{{
				MatchID: "orphan-match",
				Turns:   []TurnLog{{TurnNumber: 1, ActivePlayer: 1, Phase: "Phase_Main1", Actions: []GameAction{}}},
			}},
		},
	}
	sections := buildOutputSections(gs)
	psMap := sections["player_summary"].(map[string]any)
	data := psMap["data"].(map[string]any)
	games := data["games"].([]map[string]any)
	if len(games) != 1 {
		t.Fatalf("expected 1 game index entry, got %d", len(games))
	}
	entry := games[0]
	if entry["matchId"] != "orphan-match" {
		t.Errorf("expected matchId 'orphan-match', got %v", entry["matchId"])
	}
	if entry["section"] != "game:orphan-match" {
		t.Errorf("expected section 'game:orphan-match', got %v", entry["section"])
	}
	if _, ok := entry["opponent"]; ok {
		t.Error("expected no opponent key when match is absent")
	}
	if _, ok := entry["result"]; ok {
		t.Error("expected no result key when match is absent")
	}
}

func TestBuildSummaryWithRank(t *testing.T) {
	gs := &GameState{
		DisplayName: "TestPlayer",
		Rank: &RankSection{
			Constructed: RankInfo{Class: "Gold", Level: 4},
			Limited:     RankInfo{Class: "Silver", Level: 2},
		},
	}
	summary := buildSummary(gs)
	if summary != "TestPlayer, Gold 4 Constructed, Silver 2 Limited" {
		t.Errorf("expected full summary, got %q", summary)
	}
}

func TestBuildSummaryEmptyLimitedClass(t *testing.T) {
	gs := &GameState{
		DisplayName: "TestPlayer",
		Rank: &RankSection{
			Constructed: RankInfo{Class: "Gold", Level: 4},
			Limited:     RankInfo{Class: "", Level: 3}, // MTGA omits limitedClass when unranked
		},
	}
	summary := buildSummary(gs)
	if summary != "TestPlayer, Gold 4 Constructed" {
		t.Errorf("expected summary without Limited, got %q", summary)
	}
	extra := buildExtra(gs)
	if _, ok := extra["limitedRank"]; ok {
		t.Errorf("expected no limitedRank in extra, got %v", extra["limitedRank"])
	}
	if extra["constructedRank"] != "Gold 4" {
		t.Errorf("expected constructedRank 'Gold 4', got %v", extra["constructedRank"])
	}
}

func TestBuildSummaryNoRank(t *testing.T) {
	gs := &GameState{}
	summary := buildSummary(gs)
	if summary != "MTG Arena Player" {
		t.Errorf("expected 'MTG Arena Player', got %q", summary)
	}
}

func TestBuildExtra(t *testing.T) {
	gs := &GameState{
		Rank: &RankSection{
			Constructed: RankInfo{Class: "Gold", Level: 4},
		},
		ActiveDecks: &ActiveDecksSection{
			Decks: []Deck{{Name: "A"}, {Name: "B"}},
		},
	}
	extra := buildExtra(gs)
	if extra["constructedRank"] != "Gold 4" {
		t.Errorf("expected constructedRank 'Gold 4', got %v", extra["constructedRank"])
	}
	if extra["deckCount"] != 2 {
		t.Errorf("expected deckCount 2, got %v", extra["deckCount"])
	}
}

func TestVaultProgressNormalized(t *testing.T) {
	// MTGA stores vault progress on a 0-1000 scale (1000 = one vault).
	// We normalize to percentage: 377 → 37.7%, 1050 → 105.0%.
	tests := []struct {
		name     string
		raw      float64
		expected float64
	}{
		{"typical progress", 377, 37.7},
		{"zero", 0, 0},
		{"full vault", 1000, 100.0},
		{"over one vault", 1050, 105.0},
		{"fractional", 505, 50.5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hookJSON, _ := json.Marshal(map[string]any{
				"InventoryInfo": map[string]any{
					"Gems": 0, "Gold": 0,
					"WildCardCommons": 0, "WildCardUnCommons": 0,
					"WildCardRares": 0, "WildCardMythics": 0,
					"TotalVaultProgress": tt.raw,
					"Boosters":           []any{},
				},
			})
			entries := []LogEntry{{Arrow: "<==", Label: "StartHook", JSON: json.RawMessage(hookJSON)}}
			gs := BuildGameState(entries)
			if gs.Inventory == nil {
				t.Fatal("expected inventory section")
			}
			if gs.Inventory.VaultProgress != tt.expected {
				t.Errorf("expected vault progress %.1f, got %.1f", tt.expected, gs.Inventory.VaultProgress)
			}
		})
	}
}

func TestTurnActionsNeverNull(t *testing.T) {
	// Turns created from GRE messages without annotations must have an empty
	// Actions slice (not nil), so JSON serialization produces [] not null.
	entries := []LogEntry{
		{Label: "AuthenticateResponse", JSON: json.RawMessage(`{"authenticateResponse":{"clientId":"p1","screenName":"Me"}}`)},
		{Label: "MatchGameRoomStateChangedEvent", JSON: json.RawMessage(`{
			"matchGameRoomStateChangedEvent": {
				"gameRoomInfo": {
					"stateType": "MatchGameRoomStateType_Playing",
					"gameRoomConfig": {
						"matchId": "m1",
						"reservedPlayers": [
							{"userId": "p1", "playerName": "Me", "systemSeatId": 1, "eventId": "Test"},
							{"userId": "p2", "playerName": "Opp", "systemSeatId": 2, "eventId": "Test"}
						]
					}
				}
			}
		}`)},
		// GRE message with turnInfo but no annotations → creates a turn with zero actions.
		{Label: "GreToClientEvent", JSON: json.RawMessage(`{
			"greToClientEvent": {
				"greToClientMessages": [{
					"type": "GREMessageType_GameStateMessage",
					"gameStateMessage": {
						"turnInfo": {"turnNumber": 1, "activePlayer": 1, "phase": "Phase_Beginning"},
						"gameObjects": [],
						"annotations": []
					}
				}, {
					"type": "GREMessageType_GameStateMessage",
					"gameStateMessage": {
						"turnInfo": {"turnNumber": 1, "activePlayer": 1, "phase": "Phase_Combat"},
						"gameObjects": [],
						"annotations": []
					}
				}]
			}
		}`)},
	}
	gs := BuildGameState(entries)
	if gs.GameLogs == nil || len(gs.GameLogs.Games) == 0 {
		t.Fatal("expected game log")
	}
	for _, turn := range gs.GameLogs.Games[0].Turns {
		if turn.Actions == nil {
			t.Errorf("turn %d phase %s has nil Actions, want empty slice", turn.TurnNumber, turn.Phase)
		}
		// Verify JSON serialization produces [] not null.
		b, err := json.Marshal(turn)
		if err != nil {
			t.Fatal(err)
		}
		if !json.Valid(b) {
			t.Fatal("invalid JSON from turn marshal")
		}
		var m map[string]any
		json.Unmarshal(b, &m)
		actions, ok := m["actions"]
		if !ok {
			t.Errorf("turn %d phase %s: missing 'actions' key in JSON", turn.TurnNumber, turn.Phase)
		} else if actions == nil {
			t.Errorf("turn %d phase %s: 'actions' is null in JSON, want []", turn.TurnNumber, turn.Phase)
		}
	}
}
