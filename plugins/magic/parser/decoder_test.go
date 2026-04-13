package main

import (
	"encoding/json"
	"testing"
)

func TestDecodeNewFormatInbound(t *testing.T) {
	// New format: <== Label(uuid) followed by JSON on next line.
	log := `<== RankGetCombinedRankInfo(a3ca115b-6ed5-4ea7-9504-30b4d9ee9d42)
{"constructedClass":"Gold","constructedLevel":4}
`
	entries := DecodeLogString(log)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Arrow != "<==" {
		t.Errorf("expected arrow '<==', got %q", e.Arrow)
	}
	if e.Label != "RankGetCombinedRankInfo" {
		t.Errorf("expected label 'RankGetCombinedRankInfo', got %q", e.Label)
	}
	if e.JSON == nil {
		t.Fatal("expected JSON payload")
	}
	var data map[string]any
	if err := json.Unmarshal(e.JSON, &data); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if data["constructedClass"] != "Gold" {
		t.Errorf("expected constructedClass 'Gold', got %v", data["constructedClass"])
	}
}

func TestDecodeOutboundInlineJSON(t *testing.T) {
	// Old-format outbound with inline JSON on the same line.
	log := `[UnityCrossThreadLogger]==> BotDraftDraftPick {"id":"43d6e0b5-4483-4707-a805-089c9e70a745","request":"{\"EventName\":\"QuickDraft_TMT\"}"}
`
	entries := DecodeLogString(log)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Arrow != "==>" {
		t.Errorf("expected arrow '==>', got %q", e.Arrow)
	}
	if e.Label != "BotDraftDraftPick" {
		t.Errorf("expected label 'BotDraftDraftPick', got %q", e.Label)
	}
	if e.JSON == nil {
		t.Fatal("expected inline JSON payload")
	}
	var data map[string]any
	if err := json.Unmarshal(e.JSON, &data); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if data["id"] != "43d6e0b5-4483-4707-a805-089c9e70a745" {
		t.Errorf("expected id in JSON, got %v", data["id"])
	}
}

func TestDecodeEventEntryWithTimestamp(t *testing.T) {
	// Event entries now have timestamps with colons: 11:03:08 AM.
	log := `[UnityCrossThreadLogger]3/22/2026 11:03:08 AM: Match to 47BADBEB1045E08A: GreToClientEvent
{"greToClientEvent":{"greToClientMessages":[{"type":"GREMessageType_GameStateMessage"}]}}
`
	entries := DecodeLogString(log)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Arrow != "" {
		t.Errorf("expected empty arrow for event entry, got %q", e.Arrow)
	}
	if e.Label != "GreToClientEvent" {
		t.Errorf("expected label 'GreToClientEvent', got %q", e.Label)
	}
	if e.PlayerID == "" {
		t.Error("expected playerID to be set")
	}
	if e.PlayerID != "47BADBEB1045E08A" {
		t.Errorf("expected playerID '47BADBEB1045E08A', got %q", e.PlayerID)
	}
}

func TestDecodeEventAuthenticateResponse(t *testing.T) {
	// AuthenticateResponse also uses the event pattern in new format.
	log := `[UnityCrossThreadLogger]3/22/2026 11:03:08 AM: Match to 47BADBEB1045E08A: AuthenticateResponse
{ "transactionId": "864b3dac", "authenticateResponse": { "clientId": "47BADBEB1045E08A", "screenName":"Aure Silvershield" } }
`
	entries := DecodeLogString(log)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Label != "AuthenticateResponse" {
		t.Errorf("expected label 'AuthenticateResponse', got %q", entries[0].Label)
	}
	if entries[0].JSON == nil {
		t.Fatal("expected JSON payload")
	}
}

func TestDecodeEventMatchGameRoom(t *testing.T) {
	log := `[UnityCrossThreadLogger]3/22/2026 11:03:09 AM: Match to 47BADBEB1045E08A: MatchGameRoomStateChangedEvent
{ "matchGameRoomStateChangedEvent": { "gameRoomInfo": { "stateType": "MatchGameRoomStateType_Playing" } } }
`
	entries := DecodeLogString(log)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Label != "MatchGameRoomStateChangedEvent" {
		t.Errorf("expected label 'MatchGameRoomStateChangedEvent', got %q", entries[0].Label)
	}
}

func TestDecodeNewFormatDraftStatus(t *testing.T) {
	// New-format inbound draft status with double-encoded Payload.
	log := `<== BotDraftDraftStatus(71f6a503-4791-4b50-8a46-7513830f1107)
{"CurrentModule":"BotDraft","Payload":"{\"Result\":\"Success\",\"PackNumber\":0,\"PickNumber\":0}"}
`
	entries := DecodeLogString(log)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Label != "BotDraftDraftStatus" {
		t.Errorf("expected label 'BotDraftDraftStatus', got %q", entries[0].Label)
	}
	if entries[0].Arrow != "<==" {
		t.Errorf("expected arrow '<==', got %q", entries[0].Arrow)
	}
}

func TestDecodeLabelEntry(t *testing.T) {
	// Some entries are still plain labels without arrows or event patterns.
	log := `[UnityCrossThreadLogger]Client.SceneChange
`
	entries := DecodeLogString(log)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Label != "Client.SceneChange" {
		t.Errorf("expected label 'Client.SceneChange', got %q", entries[0].Label)
	}
}

func TestDecodeMultiLineJSON(t *testing.T) {
	log := `<== RankGetCombinedRankInfo(abc-123)
{
  "constructedClass": "Gold",
  "constructedLevel": 4
}
`
	entries := DecodeLogString(log)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	var data map[string]any
	if err := json.Unmarshal(entries[0].JSON, &data); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if data["constructedClass"] != "Gold" {
		t.Errorf("expected constructedClass 'Gold', got %v", data["constructedClass"])
	}
}

func TestDecodeSkipsDetailedLogs(t *testing.T) {
	log := `DETAILED LOGS: ENABLED
<== RankGetCombinedRankInfo(abc-123)
{"constructedClass":"Gold"}
`
	entries := DecodeLogString(log)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry (skipping DETAILED LOGS), got %d", len(entries))
	}
	if entries[0].Label != "RankGetCombinedRankInfo" {
		t.Errorf("expected RankGetCombinedRankInfo, got %q", entries[0].Label)
	}
}

func TestDecodeMultipleEntries(t *testing.T) {
	log := `<== RankGetCombinedRankInfo(abc-123)
{"constructedClass":"Gold"}
<== BotDraftDraftStatus(def-456)
{"CurrentModule":"BotDraft"}
[UnityCrossThreadLogger]==> BotDraftDraftPick {"id":"ghi-789","request":"{}"}
`
	entries := DecodeLogString(log)
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	labels := []string{entries[0].Label, entries[1].Label, entries[2].Label}
	expected := []string{"RankGetCombinedRankInfo", "BotDraftDraftStatus", "BotDraftDraftPick"}
	for i, l := range labels {
		if l != expected[i] {
			t.Errorf("entry %d: expected label %q, got %q", i, expected[i], l)
		}
	}
	// Third entry should have inline JSON.
	if entries[2].JSON == nil {
		t.Error("expected inline JSON on outbound entry")
	}
}

func TestDecodeSkipsNonLogLines(t *testing.T) {
	log := `some random unity output
another line of noise
<== RankGetCombinedRankInfo(abc-123)
{"constructedClass":"Gold"}
more noise here
`
	entries := DecodeLogString(log)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
}

func TestDecodeEntryWithoutJSON(t *testing.T) {
	log := `[UnityCrossThreadLogger]Client.SceneChange
some non-json text
<== RankGetCombinedRankInfo(abc-123)
{"constructedClass":"Gold"}
`
	entries := DecodeLogString(log)
	found := false
	for _, e := range entries {
		if e.Label == "RankGetCombinedRankInfo" {
			found = true
		}
	}
	if !found {
		t.Error("expected to find RankGetCombinedRankInfo entry")
	}
}

func TestDecodeWindowsLineEndings(t *testing.T) {
	log := "<== RankGetCombinedRankInfo(abc-123)\r\n{\"constructedClass\":\"Gold\"}\r\n"
	entries := DecodeLogString(log)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Label != "RankGetCombinedRankInfo" {
		t.Errorf("expected label 'RankGetCombinedRankInfo', got %q", entries[0].Label)
	}
}

func TestDecodeOutboundWithoutInlineJSON(t *testing.T) {
	// Old-format outbound that happens to have JSON on the next line.
	log := `[UnityCrossThreadLogger]==> RankGetCombinedRankInfo
{"request":"{}"}
`
	entries := DecodeLogString(log)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Arrow != "==>" {
		t.Errorf("expected arrow '==>', got %q", entries[0].Arrow)
	}
	if entries[0].JSON == nil {
		t.Fatal("expected JSON on next line")
	}
}

func TestDecodeOldFormatInbound(t *testing.T) {
	// Old-format inbound that still appears in some logs.
	log := `[UnityCrossThreadLogger]<== RankGetCombinedRankInfo
{"constructedClass":"Gold"}
`
	entries := DecodeLogString(log)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Arrow != "<==" {
		t.Errorf("expected arrow '<==', got %q", entries[0].Arrow)
	}
	if entries[0].Label != "RankGetCombinedRankInfo" {
		t.Errorf("expected label 'RankGetCombinedRankInfo', got %q", entries[0].Label)
	}
}

func TestDecodeInlineJSONNotConfusedWithNextLine(t *testing.T) {
	// When inline JSON is present, the next line should not be consumed as JSON for this entry.
	log := `[UnityCrossThreadLogger]==> BotDraftDraftPick {"id":"abc"}
<== BotDraftDraftPick(abc)
{"CurrentModule":"BotDraft"}
`
	entries := DecodeLogString(log)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	// First: outbound with inline JSON.
	if entries[0].Label != "BotDraftDraftPick" {
		t.Errorf("entry 0: expected label 'BotDraftDraftPick', got %q", entries[0].Label)
	}
	if entries[0].Arrow != "==>" {
		t.Errorf("entry 0: expected arrow '==>', got %q", entries[0].Arrow)
	}
	var d0 map[string]any
	if err := json.Unmarshal(entries[0].JSON, &d0); err != nil {
		t.Fatalf("entry 0: invalid JSON: %v", err)
	}
	if d0["id"] != "abc" {
		t.Errorf("entry 0: expected id 'abc', got %v", d0["id"])
	}
	// Second: inbound with next-line JSON.
	if entries[1].Label != "BotDraftDraftPick" {
		t.Errorf("entry 1: expected label 'BotDraftDraftPick', got %q", entries[1].Label)
	}
	if entries[1].Arrow != "<==" {
		t.Errorf("entry 1: expected arrow '<==', got %q", entries[1].Arrow)
	}
	var d1 map[string]any
	if err := json.Unmarshal(entries[1].JSON, &d1); err != nil {
		t.Fatalf("entry 1: invalid JSON: %v", err)
	}
	if d1["CurrentModule"] != "BotDraft" {
		t.Errorf("entry 1: expected CurrentModule 'BotDraft', got %v", d1["CurrentModule"])
	}
}
