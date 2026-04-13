package data

import (
	"testing"
)

func TestArenaCardsNotEmpty(t *testing.T) {
	if len(ArenaCards) == 0 {
		t.Fatal("ArenaCards map is empty — generated data missing")
	}
	t.Logf("ArenaCards: %d entries", len(ArenaCards))
}

func TestArenaCardsHaveRequiredFields(t *testing.T) {
	sampled := 0
	for id, card := range ArenaCards {
		if card.Name == "" {
			t.Errorf("arena_id %d: empty name", id)
		}
		if card.Set == "" {
			t.Errorf("arena_id %d (%s): empty set", id, card.Name)
		}
		if card.Rarity == "" {
			t.Errorf("arena_id %d (%s): empty rarity", id, card.Name)
		}
		sampled++
		if sampled >= 100 {
			break
		}
	}
}

func TestKnownArenaCard(t *testing.T) {
	card, ok := ArenaCards[82159]
	if !ok {
		t.Fatal("Sheoldred, the Apocalypse (82159) not found")
	}
	if card.Name != "Sheoldred, the Apocalypse" {
		t.Errorf("expected 'Sheoldred, the Apocalypse', got %q", card.Name)
	}
	if card.Set != "dmu" {
		t.Errorf("expected set 'dmu', got %q", card.Set)
	}
	if card.Rarity != "mythic" {
		t.Errorf("expected rarity 'mythic', got %q", card.Rarity)
	}
}
