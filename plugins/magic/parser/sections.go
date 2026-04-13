package main

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/joshsymonds/savecraft.gg/plugins/magic/parser/data"
)

var inventoryInfoKey = []byte("InventoryInfo")

// BuildGameState processes decoded log entries and accumulates game state.
func BuildGameState(entries []LogEntry) *GameState {
	gs := &GameState{}

	for _, e := range entries {
		switch {
		case e.Label == "AuthenticateResponse":
			processAuth(gs, e.JSON)
		case e.Arrow == "<==" && e.Label == "StartHook":
			processStartHook(gs, e.JSON)
		case e.Arrow == "<==" && e.Label == "RankGetCombinedRankInfo":
			processRank(gs, e.JSON)
		case e.Label == "MatchGameRoomStateChangedEvent":
			processMatchRoom(gs, e.JSON)
		case e.Label == "GreToClientEvent":
			processGRE(gs, e.JSON)
		case e.Arrow == "<==" && e.Label == "BotDraftDraftStatus":
			processBotDraftStatus(gs, e.JSON)
		case e.Arrow == "<==" && e.Label == "DraftNotify":
			processDraftNotify(gs, e.JSON)
		case e.Arrow == "<==" && (e.Label == "BotDraftDraftPick" || e.Label == "DraftMakeHumanDraftPick" || e.Label == "EventPlayerDraftMakePick"):
			processDraftPickResponse(gs, e.JSON)
		case e.Arrow == "==>" && (e.Label == "BotDraftDraftPick" || e.Label == "DraftMakeHumanDraftPick" || e.Label == "EventPlayerDraftMakePick"):
			processOutDraftPick(gs, e.JSON)
		}

		// Extract InventoryInfo from any response that contains it.
		// Guard with a cheap prefix check to avoid unmarshaling every entry.
		if e.JSON != nil && bytes.Contains(e.JSON, inventoryInfoKey) {
			processInventoryInfo(gs, e.JSON)
		}
	}

	return gs
}

func processAuth(gs *GameState, raw json.RawMessage) {
	var msg struct {
		AuthenticateResponse *struct {
			ClientID   string `json:"clientId"`
			ScreenName string `json:"screenName"`
		} `json:"authenticateResponse"`
	}
	if err := json.Unmarshal(raw, &msg); err != nil || msg.AuthenticateResponse == nil {
		return
	}
	gs.PlayerID = msg.AuthenticateResponse.ClientID
	gs.DisplayName = msg.AuthenticateResponse.ScreenName
}

func processStartHook(gs *GameState, raw json.RawMessage) {
	var hook struct {
		Decks           map[string]startHookDeck `json:"Decks"`
		DeckSummariesV2 []startHookDeckSummary   `json:"DeckSummariesV2"`
		InventoryInfo   *startHookInventory      `json:"InventoryInfo"`
	}
	if err := json.Unmarshal(raw, &hook); err != nil {
		return
	}

	// Build deck summaries index by ID for O(1) join.
	summaryByID := make(map[string]startHookDeckSummary, len(hook.DeckSummariesV2))
	for _, s := range hook.DeckSummariesV2 {
		summaryByID[s.DeckID] = s
	}

	// Join Decks + DeckSummariesV2 to produce complete Deck objects.
	// Skip precon decks with unlocalized names (MTGA internal localization keys).
	if len(hook.Decks) > 0 {
		section := &ActiveDecksSection{}
		for deckID, deckData := range hook.Decks {
			summary := summaryByID[deckID]
			if strings.HasPrefix(summary.Name, "?=?Loc/") {
				continue
			}
			deck := Deck{
				ID:          deckID,
				Name:        summary.Name,
				Format:      summary.format(),
				Cards:       parseCardEntries(deckData.MainDeck),
				Sideboard:   parseCardEntries(deckData.Sideboard),
				CommandZone: parseCardEntries(deckData.CommandZone),
			}
			section.Decks = append(section.Decks, deck)
		}
		gs.ActiveDecks = section
	}

	// Extract inventory snapshot.
	if hook.InventoryInfo != nil {
		applyInventorySnapshot(gs, hook.InventoryInfo)
	}
}

type startHookDeck struct {
	MainDeck    []cardEntry `json:"MainDeck"`
	Sideboard   []cardEntry `json:"Sideboard"`
	CommandZone []cardEntry `json:"CommandZone"`
}

type cardEntry struct {
	CardID   int `json:"cardId"`
	Quantity int `json:"quantity"`
}

type startHookDeckSummary struct {
	DeckID     string `json:"DeckId"`
	Name       string `json:"Name"`
	Attributes []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"Attributes"`
}

func (s startHookDeckSummary) format() string {
	for _, attr := range s.Attributes {
		if attr.Name == "Format" {
			return attr.Value
		}
	}
	return ""
}

type startHookInventory struct {
	Gems               int            `json:"Gems"`
	Gold               int            `json:"Gold"`
	WildCardCommons    int            `json:"WildCardCommons"`
	WildCardUnCommons  int            `json:"WildCardUnCommons"`
	WildCardRares      int            `json:"WildCardRares"`
	WildCardMythics    int            `json:"WildCardMythics"`
	TotalVaultProgress float64        `json:"TotalVaultProgress"`
	CustomTokens       map[string]int `json:"CustomTokens"`
	Boosters           []struct {
		CollationID int    `json:"CollationId"`
		SetCode     string `json:"SetCode"`
	} `json:"Boosters"`
}

func parseCardEntries(entries []cardEntry) []DeckCard {
	cards := make([]DeckCard, 0, len(entries))
	for _, e := range entries {
		card := data.ArenaCards[e.CardID]
		cards = append(cards, DeckCard{
			ArenaID: e.CardID,
			Name:    card.Name,
			Count:   e.Quantity,
		})
	}
	return cards
}

func applyInventorySnapshot(gs *GameState, inv *startHookInventory) {
	section := &InventorySection{
		Gold:          inv.Gold,
		Gems:          inv.Gems,
		WCCommon:      inv.WildCardCommons,
		WCUncommon:    inv.WildCardUnCommons,
		WCRare:        inv.WildCardRares,
		WCMythic:      inv.WildCardMythics,
		VaultProgress: inv.TotalVaultProgress / 10.0,
		Boosters:      []BoosterInfo{},
		CustomTokens:  inv.CustomTokens,
	}

	if inv.CustomTokens != nil {
		section.DraftTokens = inv.CustomTokens["DraftToken"]
		section.SealedTokens = inv.CustomTokens["SealedToken"]
	}

	// Aggregate booster entries by CollationId. MTGA logs have one entry per
	// booster (no Count field), so we count occurrences.
	boosterCounts := map[int]*BoosterInfo{}
	for _, b := range inv.Boosters {
		if existing, ok := boosterCounts[b.CollationID]; ok {
			existing.Count++
		} else {
			boosterCounts[b.CollationID] = &BoosterInfo{
				CollationID: b.CollationID,
				SetCode:     b.SetCode,
				Count:       1,
			}
		}
	}
	for _, bi := range boosterCounts {
		section.Boosters = append(section.Boosters, *bi)
	}

	gs.Inventory = section
}

// processInventoryInfo extracts InventoryInfo from any response that embeds it.
func processInventoryInfo(gs *GameState, raw json.RawMessage) {
	var msg struct {
		InventoryInfo    *startHookInventory `json:"InventoryInfo"`
		DTOInventoryInfo *startHookInventory `json:"DTO_InventoryInfo"`
	}
	if err := json.Unmarshal(raw, &msg); err != nil {
		return
	}
	inv := msg.InventoryInfo
	if inv == nil {
		inv = msg.DTOInventoryInfo
	}
	if inv == nil {
		return
	}
	applyInventorySnapshot(gs, inv)
}

func processRank(gs *GameState, raw json.RawMessage) {
	var rank struct {
		ConstructedClass            string  `json:"constructedClass"`
		ConstructedLevel            int     `json:"constructedLevel"`
		ConstructedStep             int     `json:"constructedStep"`
		ConstructedMatchesWon       int     `json:"constructedMatchesWon"`
		ConstructedMatchesLost      int     `json:"constructedMatchesLost"`
		ConstructedPercentile       float64 `json:"constructedPercentile"`
		ConstructedLeaderboardPlace int     `json:"constructedLeaderboardPlace"`
		LimitedClass                string  `json:"limitedClass"`
		LimitedLevel                int     `json:"limitedLevel"`
		LimitedStep                 int     `json:"limitedStep"`
		LimitedMatchesWon           int     `json:"limitedMatchesWon"`
		LimitedMatchesLost          int     `json:"limitedMatchesLost"`
		LimitedPercentile           float64 `json:"limitedPercentile"`
		LimitedLeaderboardPlace     int     `json:"limitedLeaderboardPlace"`
	}
	if err := json.Unmarshal(raw, &rank); err != nil {
		return
	}

	gs.Rank = &RankSection{
		Constructed: RankInfo{
			Class:            normalizeRankClass(rank.ConstructedClass, rank.ConstructedLevel),
			Level:            rank.ConstructedLevel,
			Step:             rank.ConstructedStep,
			MatchesWon:       rank.ConstructedMatchesWon,
			MatchesLost:      rank.ConstructedMatchesLost,
			Percentile:       rank.ConstructedPercentile,
			LeaderboardPlace: rank.ConstructedLeaderboardPlace,
		},
		Limited: RankInfo{
			Class:            normalizeRankClass(rank.LimitedClass, rank.LimitedLevel),
			Level:            rank.LimitedLevel,
			Step:             rank.LimitedStep,
			MatchesWon:       rank.LimitedMatchesWon,
			MatchesLost:      rank.LimitedMatchesLost,
			Percentile:       rank.LimitedPercentile,
			LeaderboardPlace: rank.LimitedLeaderboardPlace,
		},
	}
}

// normalizeRankClass returns "Bronze" when MTGA omits the rank class but the
// player has a non-zero level (indicating they've played ranked matches).
func normalizeRankClass(class string, level int) string {
	if class == "" && level > 0 {
		return "Bronze"
	}
	return class
}

func processMatchRoom(gs *GameState, raw json.RawMessage) {
	var msg struct {
		Timestamp                      string `json:"timestamp"`
		MatchGameRoomStateChangedEvent struct {
			GameRoomInfo struct {
				StateType      string `json:"stateType"`
				GameRoomConfig struct {
					MatchID         string `json:"matchId"`
					ReservedPlayers []struct {
						UserID       string `json:"userId"`
						PlayerName   string `json:"playerName"`
						SystemSeatID int    `json:"systemSeatId"`
						EventID      string `json:"eventId"`
					} `json:"reservedPlayers"`
				} `json:"gameRoomConfig"`
				FinalMatchResult *struct {
					MatchID              string `json:"matchId"`
					MatchCompletedReason string `json:"matchCompletedReason"`
					ResultList           []struct {
						Scope         string `json:"scope"`
						Result        string `json:"result"`
						WinningTeamID int    `json:"winningTeamId"`
					} `json:"resultList"`
				} `json:"finalMatchResult"`
			} `json:"gameRoomInfo"`
		} `json:"matchGameRoomStateChangedEvent"`
	}
	if err := json.Unmarshal(raw, &msg); err != nil {
		return
	}

	room := msg.MatchGameRoomStateChangedEvent.GameRoomInfo

	if gs.Matches == nil {
		gs.Matches = &MatchHistorySection{}
	}

	if room.StateType == "MatchGameRoomStateType_Playing" {
		var eventID string
		if len(room.GameRoomConfig.ReservedPlayers) > 0 {
			eventID = room.GameRoomConfig.ReservedPlayers[0].EventID
		}
		match := MatchResult{
			MatchID: room.GameRoomConfig.MatchID,
			EventID: eventID,
			Date:    formatTimestamp(msg.Timestamp),
		}
		for _, p := range room.GameRoomConfig.ReservedPlayers {
			if p.UserID == gs.PlayerID {
				match.Player = MatchPlayer{
					Name:   p.PlayerName,
					UserID: p.UserID,
					Seat:   p.SystemSeatID,
				}
			} else {
				match.Opponent = MatchPlayer{
					Name:   p.PlayerName,
					UserID: p.UserID,
					Seat:   p.SystemSeatID,
				}
			}
		}
		gs.Matches.Matches = append(gs.Matches.Matches, match)

		if gs.GameLogs == nil {
			gs.GameLogs = &GameLogSection{}
		}
		gs.GameLogs.Games = append(gs.GameLogs.Games, GameLog{
			MatchID: room.GameRoomConfig.MatchID,
		})
	}

	if room.StateType == "MatchGameRoomStateType_MatchCompleted" && room.FinalMatchResult != nil {
		matchID := room.FinalMatchResult.MatchID
		for i := range gs.Matches.Matches {
			if gs.Matches.Matches[i].MatchID == matchID {
				for _, r := range room.FinalMatchResult.ResultList {
					if r.Scope == "MatchScope_Game" {
						gs.Matches.Matches[i].Games = append(gs.Matches.Matches[i].Games, GameResult{
							GameNumber:  len(gs.Matches.Matches[i].Games) + 1,
							WinningSeat: r.WinningTeamID,
						})
					}
					if r.Scope == "MatchScope_Match" {
						playerSeat := gs.Matches.Matches[i].Player.Seat
						if r.WinningTeamID == playerSeat {
							gs.Matches.Matches[i].Result = "win"
						} else if r.Result == "ResultType_Draw" {
							gs.Matches.Matches[i].Result = "draw"
						} else {
							gs.Matches.Matches[i].Result = "loss"
						}
					}
				}
				break
			}
		}
	}
}

// greSessionState tracks persistent state across GRE messages within a game.
// MTGA sends incremental game state updates — objects persist until explicitly
// removed, so we accumulate them across messages for annotation resolution.
type greSessionState struct {
	objectRegistry map[int]greGameObject // instanceId → game object, persists across messages
	seenCards      map[int]bool          // GrpIDs already recorded for opponent card tracking
}

func newGRESessionState() *greSessionState {
	return &greSessionState{
		objectRegistry: make(map[int]greGameObject),
		seenCards:      make(map[int]bool),
	}
}

func processGRE(gs *GameState, raw json.RawMessage) {
	var msg struct {
		GreToClientEvent struct {
			GreToClientMessages []struct {
				Type             string `json:"type"`
				GameStateMessage *struct {
					GameStateId int `json:"gameStateId"`
					GameInfo    *struct {
						MatchID     string `json:"matchID"`
						SuperFormat string `json:"superFormat"`
					} `json:"gameInfo"`
					TurnInfo *struct {
						TurnNumber   int    `json:"turnNumber"`
						ActivePlayer int    `json:"activePlayer"`
						Phase        string `json:"phase"`
						Step         string `json:"step"`
					} `json:"turnInfo"`
					Zones []struct {
						ZoneID            int    `json:"zoneId"`
						Type              string `json:"type"`
						OwnerSeatID       int    `json:"ownerSeatId"`
						ObjectInstanceIDs []int  `json:"objectInstanceIds"`
					} `json:"zones"`
					GameObjects []greGameObject `json:"gameObjects"`
					Annotations []greAnnotation `json:"annotations"`
					Players     []struct {
						LifeTotal        int `json:"lifeTotal"`
						SystemSeatNumber int `json:"systemSeatNumber"`
						ManaPool         []struct {
							Color string `json:"color"`
							Count int    `json:"count"`
						} `json:"manaPool"`
					} `json:"players"`
				} `json:"gameStateMessage"`
			} `json:"greToClientMessages"`
		} `json:"greToClientEvent"`
	}
	if err := json.Unmarshal(raw, &msg); err != nil {
		return
	}

	if gs.GameLogs == nil || len(gs.GameLogs.Games) == 0 {
		return
	}

	currentGame := &gs.GameLogs.Games[len(gs.GameLogs.Games)-1]

	// Initialize or retrieve the persistent session state for this game.
	if currentGame.sessionState == nil {
		currentGame.sessionState = newGRESessionState()
	}
	session := currentGame.sessionState

	// Seed seenCards from existing opponent card data.
	if len(session.seenCards) == 0 && gs.Matches != nil && len(gs.Matches.Matches) > 0 {
		for _, c := range gs.Matches.Matches[len(gs.Matches.Matches)-1].Opponent.CardsSeen {
			session.seenCards[c.ArenaID] = true
		}
	}

	// Track seen annotation IDs to avoid duplicates across GRE messages within this event.
	seenAnnotations := map[int]bool{}

	for _, greMsg := range msg.GreToClientEvent.GreToClientMessages {
		if greMsg.GameStateMessage == nil {
			continue
		}
		gsm := greMsg.GameStateMessage

		// Accumulate game objects into the persistent registry and track opponent cards.
		var currentMatch *MatchResult
		if gs.Matches != nil && len(gs.Matches.Matches) > 0 {
			currentMatch = &gs.Matches.Matches[len(gs.Matches.Matches)-1]
		}
		for _, obj := range gsm.GameObjects {
			session.objectRegistry[obj.InstanceID] = obj
			if currentMatch != nil &&
				obj.OwnerSeatID == currentMatch.Opponent.Seat &&
				obj.GrpID > 0 &&
				obj.isCard() &&
				obj.Visibility == "Visibility_Public" &&
				!session.seenCards[obj.GrpID] {
				session.seenCards[obj.GrpID] = true
				card := data.ArenaCards[obj.GrpID]
				currentMatch.Opponent.CardsSeen = append(currentMatch.Opponent.CardsSeen, CardSeen{
					Name:    card.Name,
					ArenaID: obj.GrpID,
				})
			}
		}

		// Find or create the turn for this message. When turnInfo is present,
		// match by turnNumber+phase. When absent, append to the most recent turn
		// (many annotations like CastSpell arrive in messages without turnInfo).
		var turn *TurnLog
		if gsm.TurnInfo != nil {
			ti := gsm.TurnInfo
			for i := range currentGame.Turns {
				if currentGame.Turns[i].TurnNumber == ti.TurnNumber &&
					currentGame.Turns[i].Phase == ti.Phase {
					turn = &currentGame.Turns[i]
					break
				}
			}
			if turn == nil {
				currentGame.Turns = append(currentGame.Turns, TurnLog{
					TurnNumber:   ti.TurnNumber,
					ActivePlayer: ti.ActivePlayer,
					Phase:        ti.Phase,
					Actions:      []GameAction{},
				})
				turn = &currentGame.Turns[len(currentGame.Turns)-1]
			}
		} else if len(currentGame.Turns) > 0 {
			turn = &currentGame.Turns[len(currentGame.Turns)-1]
		}

		if turn != nil && gsm.Players != nil {
			var players []PlayerState
			for _, p := range gsm.Players {
				ps := PlayerState{
					Seat:      p.SystemSeatNumber,
					LifeTotal: p.LifeTotal,
				}
				for _, m := range p.ManaPool {
					ps.ManaPool = append(ps.ManaPool, ManaEntry{
						Color: m.Color,
						Count: m.Count,
					})
				}
				players = append(players, ps)
			}
			if gsm.Zones != nil {
				for _, zone := range gsm.Zones {
					if strings.Contains(zone.Type, "Hand") {
						for _, objID := range zone.ObjectInstanceIDs {
							if obj, ok := session.objectRegistry[objID]; ok {
								card := data.ArenaCards[obj.GrpID]
								if card.Name != "" {
									for i := range players {
										if players[i].Seat == zone.OwnerSeatID {
											players[i].HandCards = append(players[i].HandCards, card.Name)
										}
									}
								}
							}
						}
					}
					if strings.Contains(zone.Type, "Battlefield") {
						for _, objID := range zone.ObjectInstanceIDs {
							if obj, ok := session.objectRegistry[objID]; ok && obj.isCard() {
								name := data.ArenaCards[obj.GrpID].Name
								perm := Permanent{
									CardName:  name,
									CardID:    obj.GrpID,
									CardTypes: obj.CardTypes,
									SubTypes:  obj.SubTypes,
									Power:     obj.Power.Value,
									Toughness: obj.Toughness.Value,
									IsTapped:  obj.IsTapped,
									Damage:    obj.Damage,
								}
								// Use controllerSeatId for steal effects; fall back to ownerSeatId.
								seatID := obj.ControllerSeatID
								if seatID == 0 {
									seatID = zone.OwnerSeatID
								}
								for i := range players {
									if players[i].Seat == seatID {
										players[i].Battlefield = append(players[i].Battlefield, perm)
									}
								}
							}
						}
					}
				}
			}
			turn.Players = players
		}

		if turn != nil && gsm.Annotations != nil {
			for _, ann := range gsm.Annotations {
				if seenAnnotations[ann.ID] {
					continue
				}
				seenAnnotations[ann.ID] = true
				action := annotationToAction(ann, session.objectRegistry)
				if action != nil {
					turn.Actions = append(turn.Actions, *action)
				}
			}

			// Second pass: enrich CastActions with ManaPaid data.
			enrichManaPaid(turn, gsm.Annotations, session.objectRegistry)
		}
	}
}

// enrichManaPaid processes ManaPaid annotations and appends mana entries to
// the matching CastAction in the turn. ManaPaid annotations have affectedIds[0]
// pointing to the spell being paid for, and details with color (1=W,2=U,3=B,4=R,5=G).
func enrichManaPaid(turn *TurnLog, annotations []greAnnotation, registry map[int]greGameObject) {
	for _, ann := range annotations {
		isManaPaid := false
		for _, t := range ann.Type {
			if t == "AnnotationType_ManaPaid" {
				isManaPaid = true
				break
			}
		}
		if !isManaPaid || len(ann.AffectedIDs) == 0 {
			continue
		}

		// Find the spell this mana is paying for.
		spellObj, ok := registry[ann.AffectedIDs[0]]
		if !ok || !spellObj.isCard() {
			continue
		}

		// Extract mana color from details.
		var colorInt int
		for _, d := range ann.Details {
			if d.Key == "color" && len(d.ValueInt32) > 0 {
				colorInt = d.ValueInt32[0]
			}
		}
		color := manaColorName(colorInt)

		// Find the CastAction for this spell and append.
		for i := range turn.Actions {
			if turn.Actions[i].Type == "cast" && turn.Actions[i].Cast != nil &&
				turn.Actions[i].Cast.CardID == spellObj.GrpID {
				turn.Actions[i].Cast.ManaPaid = append(turn.Actions[i].Cast.ManaPaid, ManaEntry{
					Color: color,
					Count: 1,
				})
				break
			}
		}
	}
}

// manaColorName converts MTGA's integer mana color to a string symbol.
func manaColorName(color int) string {
	switch color {
	case 1:
		return "W"
	case 2:
		return "U"
	case 3:
		return "B"
	case 4:
		return "R"
	case 5:
		return "G"
	default:
		return "C" // colorless
	}
}

type greGameObject struct {
	InstanceID       int      `json:"instanceId"`
	GrpID            int      `json:"grpId"`
	Type             string   `json:"type"`
	ZoneID           int      `json:"zoneId"`
	OwnerSeatID      int      `json:"ownerSeatId"`
	ControllerSeatID int      `json:"controllerSeatId"`
	Visibility       string   `json:"visibility"`
	CardTypes        []string `json:"cardTypes"`
	SubTypes         []string `json:"subtypes"`
	Power            struct {
		Value int `json:"value"`
	} `json:"power"`
	Toughness struct {
		Value int `json:"value"`
	} `json:"toughness"`
	IsTapped bool `json:"isTapped"`
	Damage   int  `json:"damage"`
}

// isCard returns true if the game object represents a card or token (not an ability).
func (o greGameObject) isCard() bool {
	switch o.Type {
	case "GameObjectType_Card", "GameObjectType_Token", "GameObjectType_RevealedCard", "":
		// Empty type is treated as card for backwards compatibility with older logs.
		return true
	default:
		return false
	}
}

type greAnnotation struct {
	ID          int      `json:"id"`
	AffectorID  int      `json:"affectorId"`
	AffectedIDs []int    `json:"affectedIds"`
	Type        []string `json:"type"`
	Details     []struct {
		Key         string   `json:"key"`
		Type        string   `json:"type"`
		ValueString []string `json:"valueString"`
		ValueInt32  []int    `json:"valueInt32"`
	} `json:"details"`
}

func annotationToAction(ann greAnnotation, registry map[int]greGameObject) *GameAction {
	for _, annType := range ann.Type {
		switch annType {
		case "AnnotationType_ZoneTransfer":
			return handleZoneTransfer(ann, registry)
		case "AnnotationType_ResolutionComplete":
			return handleResolutionComplete(ann, registry)
		case "AnnotationType_DamageDealt":
			return handleDamageDealt(ann, registry)
		case "AnnotationType_TappedUntappedPermanent":
			return handleTapUntap(ann, registry)
		case "AnnotationType_AbilityInstanceCreated":
			return handleAbilityCreated(ann, registry)
		case "AnnotationType_PlayerSubmittedTargets":
			return handleTargetSubmitted(ann, registry)
		case "AnnotationType_PowerToughnessModCreated":
			return handleStatMod(ann, registry)
		}
	}
	return nil
}

func handleZoneTransfer(ann greAnnotation, registry map[int]greGameObject) *GameAction {
	// Try affectorId first (the card causing the move), then fall back to
	// affectedIds (the card being moved). Many zone transfers have affectorId=0
	// (system) or a player seat ID — in those cases the actual card is in affectedIds.
	obj, ok := registry[ann.AffectorID]
	if !ok || !obj.isCard() {
		// Fall back to first affectedId that resolves to a card.
		ok = false
		for _, aid := range ann.AffectedIDs {
			if candidate, found := registry[aid]; found && candidate.isCard() {
				obj = candidate
				ok = true
				break
			}
		}
	}
	if !ok {
		return nil
	}
	card := data.ArenaCards[obj.GrpID]
	if card.Name == "" {
		return nil
	}

	var zoneFrom, zoneTo, category string
	for _, d := range ann.Details {
		switch d.Key {
		case "zone_src":
			if len(d.ValueString) > 0 {
				zoneFrom = d.ValueString[0]
			}
		case "zone_dest":
			if len(d.ValueString) > 0 {
				zoneTo = d.ValueString[0]
			}
		case "category":
			if len(d.ValueString) > 0 {
				category = d.ValueString[0]
			}
		}
	}

	// Cast spells get their own action type.
	if strings.Contains(category, "Cast") {
		return &GameAction{
			Player: obj.OwnerSeatID,
			Type:   "cast",
			Cast: &CastAction{
				CardName: card.Name,
				CardID:   obj.GrpID,
			},
		}
	}

	// Everything else is a move action.
	moveType := categorizeZoneTransfer(category)
	if moveType == "put" {
		moveType = refinePutAction(zoneTo)
	}
	if moveType == "" || moveType == "cast" {
		moveType = inferAction(zoneFrom, zoneTo)
	}

	return &GameAction{
		Player: obj.OwnerSeatID,
		Type:   "move",
		Move: &MoveAction{
			CardName: card.Name,
			CardID:   obj.GrpID,
			MoveType: moveType,
			ZoneFrom: zoneFrom,
			ZoneTo:   zoneTo,
		},
	}
}

func handleResolutionComplete(ann greAnnotation, registry map[int]greGameObject) *GameAction {
	obj, ok := registry[ann.AffectorID]
	if !ok || !obj.isCard() || obj.GrpID == 0 {
		return nil
	}
	card := data.ArenaCards[obj.GrpID]
	return &GameAction{
		Player: obj.OwnerSeatID,
		Type:   "resolve",
		Resolve: &ResolveAction{
			CardName: card.Name,
			CardID:   obj.GrpID,
		},
	}
}

func handleDamageDealt(ann greAnnotation, registry map[int]greGameObject) *GameAction {
	obj, ok := registry[ann.AffectorID]
	if !ok || !obj.isCard() || obj.GrpID == 0 {
		return nil
	}
	card := data.ArenaCards[obj.GrpID]

	var amount, dmgType int
	for _, d := range ann.Details {
		switch d.Key {
		case "damage":
			if len(d.ValueInt32) > 0 {
				amount = d.ValueInt32[0]
			}
		case "type":
			if len(d.ValueInt32) > 0 {
				dmgType = d.ValueInt32[0]
			}
		}
	}

	// Resolve target: affectedIds may be a player seat or a game object.
	target := "unknown"
	if len(ann.AffectedIDs) > 0 {
		targetID := ann.AffectedIDs[0]
		if targetObj, found := registry[targetID]; found && targetObj.isCard() {
			targetCard := data.ArenaCards[targetObj.GrpID]
			if targetCard.Name != "" {
				target = targetCard.Name
			}
		} else if targetID == 1 || targetID == 2 {
			target = "player"
		}
	}

	return &GameAction{
		Player: obj.OwnerSeatID,
		Type:   "damage",
		Damage: &DamageAction{
			Source:   card.Name,
			SourceID: obj.GrpID,
			Target:   target,
			Amount:   amount,
			IsCombat: dmgType == 1,
		},
	}
}

func handleTapUntap(ann greAnnotation, registry map[int]greGameObject) *GameAction {
	// The tapped card is in affectedIds, not affectorId.
	if len(ann.AffectedIDs) == 0 {
		return nil
	}
	obj, ok := registry[ann.AffectedIDs[0]]
	if !ok || !obj.isCard() || obj.GrpID == 0 {
		return nil
	}
	card := data.ArenaCards[obj.GrpID]

	tapped := false
	for _, d := range ann.Details {
		if d.Key == "tapped" && len(d.ValueInt32) > 0 {
			tapped = d.ValueInt32[0] == 1
		}
	}

	return &GameAction{
		Player: obj.OwnerSeatID,
		Type:   "tap",
		Tap: &TapAction{
			CardName: card.Name,
			CardID:   obj.GrpID,
			Tapped:   tapped,
		},
	}
}

func handleAbilityCreated(ann greAnnotation, registry map[int]greGameObject) *GameAction {
	obj, ok := registry[ann.AffectorID]
	if !ok || !obj.isCard() || obj.GrpID == 0 {
		return nil
	}
	card := data.ArenaCards[obj.GrpID]

	return &GameAction{
		Player: obj.OwnerSeatID,
		Type:   "ability",
		Ability: &AbilityAction{
			CardName:    card.Name,
			CardID:      obj.GrpID,
			AbilityType: "triggered",
		},
	}
}

func handleTargetSubmitted(ann greAnnotation, registry map[int]greGameObject) *GameAction {
	// affectorId is the source card (or player seat), affectedIds are the targets.
	// Try affectorId first, fall back to looking up the source.
	obj, ok := registry[ann.AffectorID]
	if !ok || !obj.isCard() {
		// affectorId might be a player seat — still create the action if we have targets.
		// Use a zero-value source.
		obj = greGameObject{OwnerSeatID: ann.AffectorID}
	}
	card := data.ArenaCards[obj.GrpID]

	var targets []string
	for _, aid := range ann.AffectedIDs {
		if targetObj, found := registry[aid]; found && targetObj.isCard() {
			targetCard := data.ArenaCards[targetObj.GrpID]
			if targetCard.Name != "" {
				targets = append(targets, targetCard.Name)
			}
		}
	}
	if len(targets) == 0 {
		return nil
	}

	return &GameAction{
		Player: obj.OwnerSeatID,
		Type:   "target",
		Target: &TargetAction{
			CardName: card.Name,
			CardID:   obj.GrpID,
			Targets:  targets,
		},
	}
}

func handleStatMod(ann greAnnotation, registry map[int]greGameObject) *GameAction {
	// The modified card is in affectedIds.
	if len(ann.AffectedIDs) == 0 {
		return nil
	}
	obj, ok := registry[ann.AffectedIDs[0]]
	if !ok || !obj.isCard() || obj.GrpID == 0 {
		return nil
	}
	card := data.ArenaCards[obj.GrpID]

	var power, toughness int
	for _, d := range ann.Details {
		switch d.Key {
		case "power":
			if len(d.ValueInt32) > 0 {
				power = d.ValueInt32[0]
			}
		case "toughness":
			if len(d.ValueInt32) > 0 {
				toughness = d.ValueInt32[0]
			}
		}
	}

	return &GameAction{
		Player: obj.OwnerSeatID,
		Type:   "stat_mod",
		StatMod: &StatModAction{
			CardName:  card.Name,
			CardID:    obj.GrpID,
			Power:     power,
			Toughness: toughness,
		},
	}
}

func categorizeZoneTransfer(category string) string {
	switch {
	case strings.Contains(category, "Cast"):
		return "cast"
	case strings.Contains(category, "PlayLand"):
		return "play_land"
	case strings.Contains(category, "Draw"):
		return "draw"
	case strings.Contains(category, "Discard"):
		return "discard"
	case strings.Contains(category, "Countered"):
		return "countered"
	case strings.Contains(category, "Destroy"):
		return "destroy"
	case strings.Contains(category, "Exile"):
		return "exile"
	case strings.Contains(category, "Put"):
		return "put"
	default:
		return "move"
	}
}

// refinePutAction refines the generic "put" action into a zone-specific subtype.
func refinePutAction(zoneTo string) string {
	switch {
	case strings.Contains(zoneTo, "Battlefield"):
		return "put_into_play"
	case strings.Contains(zoneTo, "Graveyard"):
		return "put_into_graveyard"
	case strings.Contains(zoneTo, "Hand"):
		return "put_into_hand"
	case strings.Contains(zoneTo, "Library"):
		return "put_into_library"
	default:
		return "put"
	}
}

func inferAction(from, to string) string {
	switch {
	case strings.Contains(to, "Hand") && strings.Contains(from, "Library"):
		return "draw"
	case strings.Contains(to, "Stack"):
		return "cast"
	case strings.Contains(to, "Battlefield") && strings.Contains(from, "Hand"):
		return "play_land"
	case strings.Contains(to, "Graveyard"):
		return "destroy"
	case strings.Contains(to, "Exile"):
		return "exile"
	default:
		return "move"
	}
}

func processBotDraftStatus(gs *GameState, raw json.RawMessage) {
	processDraftPackStatus(gs, unwrapPayload(raw), false)
}

// processDraftPickResponse handles inbound draft pick responses which contain
// the next pick's pack contents in their Payload.
func processDraftPickResponse(gs *GameState, raw json.RawMessage) {
	processDraftPackStatus(gs, unwrapPayload(raw), true)
}

// processDraftPackStatus extracts a draft pack from a status payload and records
// the available cards. Used by both initial BotDraftDraftStatus and inbound
// BotDraftDraftPick responses (which embed the next pick's pack in their Payload).
func processDraftPackStatus(gs *GameState, raw json.RawMessage, requirePack bool) {
	var status struct {
		EventName  string   `json:"EventName"`
		DraftID    string   `json:"DraftId"`
		PackNumber int      `json:"PackNumber"`
		PickNumber int      `json:"PickNumber"`
		DraftPack  []string `json:"DraftPack"`
	}
	if err := json.Unmarshal(raw, &status); err != nil {
		return
	}
	if requirePack && len(status.DraftPack) == 0 {
		return
	}

	if gs.Drafts == nil {
		gs.Drafts = &DraftHistorySection{}
	}

	draft := findOrCreateDraft(gs, status.EventName, status.DraftID, "quick")

	available := make([]DraftCard, len(status.DraftPack))
	for i, idStr := range status.DraftPack {
		id := atoiSafe(idStr)
		available[i] = DraftCard{Name: resolveCardName(id, idStr), ID: id}
	}

	// Deduplicate: update existing pick if same (PackNumber, PickNumber).
	for i := range draft.Picks {
		if draft.Picks[i].PackNumber == status.PackNumber && draft.Picks[i].PickNumber == status.PickNumber {
			draft.Picks[i].Available = available
			return
		}
	}

	draft.Picks = append(draft.Picks, DraftPick{
		PackNumber: status.PackNumber,
		PickNumber: status.PickNumber,
		Available:  available,
	})
}

// unwrapPayload handles the double-encoded Payload pattern used by draft responses.
func unwrapPayload(raw json.RawMessage) json.RawMessage {
	var wrapper struct {
		Payload string `json:"Payload"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil || wrapper.Payload == "" {
		return raw
	}
	return json.RawMessage(wrapper.Payload)
}

// atoiSafe converts a string to int, returning 0 on error.
func atoiSafe(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func processDraftNotify(gs *GameState, raw json.RawMessage) {
	var notify struct {
		DraftID   string `json:"draftId"`
		SelfPick  int    `json:"SelfPick"`
		SelfPack  int    `json:"SelfPack"`
		PackCards string `json:"PackCards"`
	}
	if err := json.Unmarshal(raw, &notify); err != nil {
		return
	}

	if gs.Drafts == nil {
		gs.Drafts = &DraftHistorySection{}
	}

	draft := findOrCreateDraft(gs, "", notify.DraftID, "premier")

	var available []DraftCard
	for idStr := range strings.SplitSeq(notify.PackCards, ",") {
		idStr = strings.TrimSpace(idStr)
		if id, err := strconv.Atoi(idStr); err == nil {
			available = append(available, DraftCard{Name: resolveCardName(id, idStr), ID: id})
		}
	}

	// Deduplicate: update existing pick if same (PackNumber, PickNumber).
	for i := range draft.Picks {
		if draft.Picks[i].PackNumber == notify.SelfPack && draft.Picks[i].PickNumber == notify.SelfPick {
			draft.Picks[i].Available = available
			return
		}
	}

	draft.Picks = append(draft.Picks, DraftPick{
		PackNumber: notify.SelfPack,
		PickNumber: notify.SelfPick,
		Available:  available,
	})
}

// resolveCardName returns the card name from ArenaCards, or the fallback if unknown.
// If no fallback is provided, uses the card ID as string.
func resolveCardName(arenaID int, fallback ...string) string {
	card := data.ArenaCards[arenaID]
	if card.Name != "" {
		return card.Name
	}
	if len(fallback) > 0 {
		return fallback[0]
	}
	return strconv.Itoa(arenaID)
}

func processOutDraftPick(gs *GameState, raw json.RawMessage) {
	var msg struct {
		Request string `json:"request"`
	}
	if err := json.Unmarshal(raw, &msg); err != nil || msg.Request == "" {
		return
	}

	var req struct {
		PickInfo struct {
			CardIDs    []string `json:"CardIds"`
			PackNumber int      `json:"PackNumber"`
			PickNumber int      `json:"PickNumber"`
		} `json:"PickInfo"`
	}
	if err := json.Unmarshal([]byte(msg.Request), &req); err != nil {
		return
	}

	if gs.Drafts == nil || len(gs.Drafts.Drafts) == 0 {
		return
	}

	if len(req.PickInfo.CardIDs) == 0 {
		return
	}
	cardID, _ := strconv.Atoi(req.PickInfo.CardIDs[0])
	cardName := resolveCardName(cardID)

	draft := &gs.Drafts.Drafts[len(gs.Drafts.Drafts)-1]
	packNum := req.PickInfo.PackNumber
	pickNum := req.PickInfo.PickNumber

	for i := range draft.Picks {
		if draft.Picks[i].PackNumber == packNum && draft.Picks[i].PickNumber == pickNum {
			draft.Picks[i].Picked = cardName
			draft.Picks[i].PickedID = cardID
			return
		}
	}

	draft.Picks = append(draft.Picks, DraftPick{
		PackNumber: packNum,
		PickNumber: pickNum,
		Picked:     cardName,
		PickedID:   cardID,
	})
}

func findOrCreateDraft(gs *GameState, eventName, draftID, draftType string) *DraftSession {
	// Match by DraftID if available, otherwise by EventName.
	for i := range gs.Drafts.Drafts {
		if draftID != "" && gs.Drafts.Drafts[i].DraftID == draftID {
			return &gs.Drafts.Drafts[i]
		}
		if draftID == "" && eventName != "" && gs.Drafts.Drafts[i].EventName == eventName {
			return &gs.Drafts.Drafts[i]
		}
	}
	gs.Drafts.Drafts = append(gs.Drafts.Drafts, DraftSession{
		EventName: eventName,
		DraftID:   draftID,
		DraftType: draftType,
	})
	return &gs.Drafts.Drafts[len(gs.Drafts.Drafts)-1]
}

// formatTimestamp converts a millisecond epoch string to ISO 8601 (RFC 3339).
// Returns the original string if parsing fails.
func formatTimestamp(ts string) string {
	ms, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return ts
	}
	return time.UnixMilli(ms).UTC().Format(time.RFC3339)
}
