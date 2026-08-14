package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

const resultUnitBytes = 10_240

func main() {
	enc := json.NewEncoder(os.Stdout)

	writeStatus(enc, "Parsing Player.log...")

	entries := DecodeLog(os.Stdin)
	writeStatus(enc, fmt.Sprintf("Decoded %d log entries", len(entries)))

	gs := BuildGameState(entries)

	// Build the output sections.
	sections := buildOutputSections(gs)

	// Build identity and summary.
	saveName, displayName := buildIdentity(gs)

	summary := buildSummary(gs)

	enc.Encode(map[string]any{
		"type": "result",
		"identity": map[string]any{
			"saveName":    saveName,
			"gameId":      "magic",
			"extra":       buildExtra(gs),
			"displayName": displayName,
		},
		"summary":  summary,
		"sections": sections,
	})
}

// buildIdentity derives the save's stable identity key and display label.
// MTGA is one save per source, so saveName is a constant. displayName
// follows the chain screenName -> clientId -> empty; MTGA's AuthenticateResponse
// (the only screenName source) fires on match connection, not client login, so
// a session where the player never queues a match has no name available. An
// empty displayName is intentional here — the worker treats empty as "no
// update" and read surfaces supply their own fallback copy, so this plugin
// never emits a literal placeholder that could overwrite a previously-learned
// real screen name.
func buildIdentity(gs *GameState) (saveName, displayName string) {
	saveName = "player"

	displayName = gs.DisplayName
	if displayName == "" {
		displayName = gs.PlayerID
	}

	return saveName, displayName
}

func buildOutputSections(gs *GameState) map[string]any {
	sections := map[string]any{}

	// Always emit player_summary — the compact overview for get_save.
	sections["player_summary"] = map[string]any{
		"description": "Player overview: rank, currencies (gold/gems/wildcards/boosters), deck names, match results, and game log index — start here to understand the player's current state. Note: Magic Arena does not log the player's card collection to Player.log, so owned-card queries cannot be answered from save data. The matches/games indexes only cover matches recorded in the current Player.log — MTGA truncates this log on client restart, so older matches roll off and disappear from these indexes. For cumulative match history (summary stats and trends across all recorded matches, not turn-by-turn logs for rolled-off matches), use the magic match_stats reference module.",
		"data":        buildPlayerSummary(gs),
	}

	// Per-deck sections with full card lists.
	if gs.ActiveDecks != nil {
		for _, deck := range gs.ActiveDecks.Decks {
			if deck.Name == "" {
				continue
			}
			sections["deck:"+deck.Name] = map[string]any{
				"description": fmt.Sprintf("Deck list for %s (%s) — main deck, sideboard, and command zone cards", deck.Name, deck.Format),
				"data":        deck,
			}
		}
	}

	// Per-match sections with full match metadata (opponent cards seen, rank, game results).
	if gs.Matches != nil {
		for _, m := range gs.Matches.Matches {
			if m.MatchID == "" {
				continue
			}
			sections["match:"+m.MatchID] = map[string]any{
				"description": fmt.Sprintf("Match result for %s vs %s — includes opponent cards seen, rank, and per-game outcomes", m.MatchID, m.Opponent.Name),
				"data":        m,
			}
		}
	}

	// Per-game sections with full turn-by-turn data (v3b compressed — see
	// game_section_v3b.go for the transform rationale). Games that exceed
	// RESULT_UNIT_BYTES are emitted as contiguous `game:<matchId>:p<N>` phase
	// sections, numbered from 1 in turn-sequence order; smaller games retain
	// the unsuffixed `game:<matchId>` name.
	if gs.GameLogs != nil {
		for _, game := range gs.GameLogs.Games {
			if game.MatchID == "" {
				continue
			}
			for name, section := range buildGameOutputSections(game) {
				sections[name] = section
			}
		}
	}

	if gs.Drafts != nil && len(gs.Drafts.Drafts) > 0 {
		// Populate in_deck for each pick (cumulative pool of previously picked cards).
		for d := range gs.Drafts.Drafts {
			var pool []DraftCard
			for i := range gs.Drafts.Drafts[d].Picks {
				gs.Drafts.Drafts[d].Picks[i].InDeck = append([]DraftCard{}, pool...)
				if gs.Drafts.Drafts[d].Picks[i].Picked != "" {
					pool = append(pool, DraftCard{
						Name: gs.Drafts.Drafts[d].Picks[i].Picked,
						ID:   gs.Drafts.Drafts[d].Picks[i].PickedID,
					})
				}
			}
		}

		sections["draft_history"] = map[string]any{
			"description": "Draft picks with pool and pack at each selection. Each pick has in_deck (cards already drafted), available (pack contents), and picked (card chosen). Each card has a name and id (arena_id) for disambiguation — cards with similar names have different IDs.\n\nTo evaluate picks, use query_reference with draft_advisor:\n- BATCH OVERVIEW: Pass set + full pick_history to get a compact summary classifying every pick as optimal/good/questionable/miss. Use this first to identify which picks to examine.\n- DETAILED ANALYSIS: For specific picks, call draft_advisor with set + pool (= in_deck card names) + pack (= available card names) + pick_number. This returns full 6-axis contextual scores for every card in the pack.\n\nIf the last pick has no 'picked' card, the player is LIVE DRAFTING — call draft_advisor with pool + pack for a recommendation.\n\nDO NOT use card_stats to evaluate draft picks. The draft_advisor's contextual scoring (synergy, curve, role, signal, castability, baseline) is far more informative than raw GIH WR stats.",
			"data":        gs.Drafts,
		}
	}

	return sections
}

func buildGameOutputSections(game GameLog) map[string]any {
	baseName := "game:" + game.MatchID
	fullData := buildV3bGameSectionData(game)
	if serializedSize(fullData) <= resultUnitBytes {
		return map[string]any{baseName: map[string]any{
			"description": buildV3bGameSectionDescription(game.MatchID),
			"data":        fullData,
		}}
	}

	sections := map[string]any{}
	landIDs := collectLandIds(game)
	start := 0
	for phase := 1; start < len(game.Turns); phase++ {
		end := start + 1
		data := buildV3bGamePhaseData(game, landIDs, start, end, phase)
		for end < len(game.Turns) {
			candidate := buildV3bGamePhaseData(game, landIDs, start, end+1, phase)
			if serializedSize(candidate) > resultUnitBytes {
				break
			}
			end++
			data = candidate
		}

		name := fmt.Sprintf("%s:p%d", baseName, phase)
		sections[name] = map[string]any{
			"description": buildV3bGameSectionDescription(game.MatchID),
			"data":        data,
		}
		start = end
	}
	return sections
}

func buildV3bGamePhaseData(game GameLog, landIDs map[int]bool, start, end, phase int) map[string]any {
	phaseGame := game
	phaseGame.Turns = game.Turns[start:end]
	phaseGame.End = nil

	turns := make([]map[string]any, 0, len(phaseGame.Turns))
	for _, turn := range phaseGame.Turns {
		turns = append(turns, buildV3bTurn(turn, landIDs))
	}
	data := map[string]any{
		"matchId":   game.MatchID,
		"phase":     fmt.Sprintf("p%d", phase),
		"turnStart": game.Turns[start].TurnNumber,
		"turnEnd":   game.Turns[end-1].TurnNumber,
		"cd":        collectCardLookup(phaseGame),
		"tn":        turns,
	}
	if end == len(game.Turns) && game.End != nil {
		data["end"] = buildV3bGameEnd(game.End)
	}
	return data
}

func serializedSize(value any) int {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Sprintf("serialize game section: %v", err))
	}
	return len(encoded)
}

func buildPlayerSummary(gs *GameState) map[string]any {
	summary := map[string]any{}

	if gs.DisplayName != "" {
		summary["display_name"] = gs.DisplayName
	}

	if gs.Rank != nil {
		summary["rank"] = gs.Rank
	}

	if gs.Currencies != nil {
		summary["currencies"] = gs.Currencies
	}

	// Deck index: names, formats, and section pointers (no card lists).
	if gs.ActiveDecks != nil {
		deckList := make([]map[string]any, 0, len(gs.ActiveDecks.Decks))
		for _, deck := range gs.ActiveDecks.Decks {
			if deck.Name == "" {
				continue
			}
			deckList = append(deckList, map[string]any{
				"name":    deck.Name,
				"format":  deck.Format,
				"section": "deck:" + deck.Name,
			})
		}
		summary["decks"] = deckList
	}

	// Match index: matchId, eventId, opponent, result, and section pointer (no opponent cards).
	if gs.Matches != nil && len(gs.Matches.Matches) > 0 {
		matchList := make([]map[string]any, 0, len(gs.Matches.Matches))
		for _, m := range gs.Matches.Matches {
			if m.MatchID == "" {
				continue
			}
			matchList = append(matchList, map[string]any{
				"matchId":  m.MatchID,
				"eventId":  m.EventID,
				"date":     m.Date,
				"opponent": m.Opponent.Name,
				"result":   m.Result,
				"games":    m.Games,
				"section":  "match:" + m.MatchID,
			})
		}
		summary["matches"] = matchList
	}

	// Game log index: matchId, opponent, result, turn count, section pointer.
	if gs.GameLogs != nil && len(gs.GameLogs.Games) > 0 {
		gameIndex := make([]map[string]any, 0, len(gs.GameLogs.Games))
		for _, game := range gs.GameLogs.Games {
			if game.MatchID == "" {
				continue
			}
			entry := map[string]any{
				"matchId": game.MatchID,
				"turns":   maxTurnNumber(game.Turns),
				"section": "game:" + game.MatchID,
			}
			// Cross-reference match data for opponent/result if available.
			if gs.Matches != nil {
				for _, m := range gs.Matches.Matches {
					if m.MatchID == game.MatchID {
						entry["opponent"] = m.Opponent.Name
						entry["result"] = m.Result
						break
					}
				}
			}
			gameIndex = append(gameIndex, entry)
		}
		summary["games"] = gameIndex
	}

	return summary
}

// maxTurnNumber returns the highest TurnNumber across a game's turn snapshot
// entries, or 0 if there are none. MTGA emits multiple (turnNumber, phase)
// snapshots per real turn (one per phase), so len(turns) overcounts — the
// real turn count is the max TurnNumber observed.
func maxTurnNumber(turns []TurnLog) int {
	highest := 0
	for _, t := range turns {
		if t.TurnNumber > highest {
			highest = t.TurnNumber
		}
	}
	return highest
}

func buildSummary(gs *GameState) string {
	parts := []string{}
	if gs.DisplayName != "" {
		parts = append(parts, gs.DisplayName)
	}
	if gs.Rank != nil {
		if gs.Rank.Constructed.Class != "" {
			parts = append(parts, fmt.Sprintf("%s %d Constructed", gs.Rank.Constructed.Class, gs.Rank.Constructed.Level))
		}
		if gs.Rank.Limited.Class != "" {
			parts = append(parts, fmt.Sprintf("%s %d Limited", gs.Rank.Limited.Class, gs.Rank.Limited.Level))
		}
	}
	if len(parts) == 0 {
		return "MTG Arena Player"
	}
	return strings.Join(parts, ", ")
}

func buildExtra(gs *GameState) map[string]any {
	extra := map[string]any{}
	if gs.Rank != nil {
		if gs.Rank.Constructed.Class != "" {
			extra["constructedRank"] = fmt.Sprintf("%s %d", gs.Rank.Constructed.Class, gs.Rank.Constructed.Level)
		}
		if gs.Rank.Limited.Class != "" {
			extra["limitedRank"] = fmt.Sprintf("%s %d", gs.Rank.Limited.Class, gs.Rank.Limited.Level)
		}
	}
	if gs.ActiveDecks != nil {
		extra["deckCount"] = len(gs.ActiveDecks.Decks)
	}
	return extra
}

func writeStatus(enc *json.Encoder, msg string) {
	if err := enc.Encode(map[string]any{"type": "status", "message": msg}); err != nil {
		os.Exit(1)
	}
}
