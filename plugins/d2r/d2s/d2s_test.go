package d2s

import (
	"fmt"
	"os"
	"testing"
)

func TestParseSharedStash(t *testing.T) {
	data, err := os.ReadFile("../../../reference/Diablo II Resurrected/ModernSharedStashSoftCoreV2.d2i")
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	if !IsStash(data) {
		t.Fatal("IsStash returned false for .d2i file")
	}

	stash, err := ParseStash(data)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if stash.Kind != 2 {
		t.Errorf("kind = %d, want 2 (RotW softcore)", stash.Kind)
	}
	if stash.Version != 0x69 {
		t.Errorf("version = 0x%x, want 0x69", stash.Version)
	}
	if len(stash.Tabs) != 7 {
		t.Errorf("tabs = %d, want 7", len(stash.Tabs))
	}

	totalItems := 0
	for i, tab := range stash.Tabs {
		totalItems += len(tab.Items)
		t.Logf("  tab[%d]: type=%d items=%d", i, tab.Type, len(tab.Items))
		for j, item := range tab.Items {
			t.Logf("    item[%d]: code=%q name=%q quality=%s", j, item.Code, item.TypeName, item.Quality)
		}
	}

	if totalItems != 60 {
		t.Errorf("totalItems = %d, want 60", totalItems)
	}

	// Tab 0 has the uniques/sets, tab 5 (advanced) has gems/runes.
	if len(stash.Tabs[0].Items) != 14 {
		t.Errorf("tab[0] items = %d, want 14", len(stash.Tabs[0].Items))
	}
	if stash.Tabs[5].Type != 1 {
		t.Errorf("tab[5] type = %d, want 1 (advanced)", stash.Tabs[5].Type)
	}
	if len(stash.Tabs[5].Items) != 46 {
		t.Errorf("tab[5] items = %d, want 46", len(stash.Tabs[5].Items))
	}
	if stash.Tabs[6].Type != 2 {
		t.Errorf("tab[6] type = %d, want 2 (metadata)", stash.Tabs[6].Type)
	}
}

func TestIsStash_ReturnsFalseForD2S(t *testing.T) {
	data, err := os.ReadFile("../../../reference/Diablo II Resurrected/Atmus.d2s")
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	if IsStash(data) {
		t.Error("IsStash returned true for .d2s file")
	}
}

func TestIsStash_ReturnsFalseForSmallData(t *testing.T) {
	if IsStash(nil) {
		t.Error("IsStash returned true for nil")
	}
	if IsStash([]byte{0x55, 0xAA, 0x55, 0xAA}) {
		t.Error("IsStash returned true for 4 bytes")
	}
}

func TestParseStash_TooSmall(t *testing.T) {
	_, err := ParseStash([]byte{0x55, 0xAA, 0x55, 0xAA})
	if err == nil {
		t.Error("expected error for truncated stash data")
	}
}

func TestParseAtmus(t *testing.T) {
	data, err := os.ReadFile("../../../reference/Diablo II Resurrected/Atmus.d2s")
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	d, err := Parse(data)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if d.Header.Name != "Atmus" {
		t.Errorf("name = %q, want Atmus", d.Header.Name)
	}
	if d.Header.Level != 74 {
		t.Errorf("level = %d, want 74", d.Header.Level)
	}
	if d.Header.Class != Warlock {
		t.Errorf("class = %s, want Warlock", d.Header.Class)
	}
	if len(d.Items) != 45 {
		t.Errorf("items = %d, want 45", len(d.Items))
	}

	t.Logf("Name: %s, Level: %d, Class: %s, Realm: %d", d.Header.Name, d.Header.Level, d.Header.Class, d.Header.Realm)
	t.Logf("Total items: %d", len(d.Items))

	for i, item := range d.Items {
		socketInfo := ""
		if item.Socketed {
			socketInfo = fmt.Sprintf(" sockets=%d/%d", len(item.SocketedItems), item.TotalSockets)
		}
		t.Logf("  item[%d]: code=%q quality=%s name=%q simple=%v eth=%v loc=%d%s",
			i, item.Code, item.Quality, item.TypeName, item.SimpleItem, item.Ethereal, item.Location, socketInfo)
		if !item.SimpleItem {
			t.Logf("    id=%d level=%d defense=%d maxdur=%d curdur=%d qty=%d",
				item.ID, item.ItemLevel, item.Defense, item.MaxDurability, item.CurDurability, item.Quantity)
		}
		for _, attr := range item.MagicAttributes {
			t.Logf("    prop %d: %s = %v", attr.ID, attr.Name, attr.Values)
		}
		for si, sitem := range item.SocketedItems {
			t.Logf("    socket[%d]: code=%q name=%q", si, sitem.Code, sitem.TypeName)
		}
	}
}
