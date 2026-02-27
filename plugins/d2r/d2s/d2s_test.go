package d2s

import (
	"fmt"
	"os"
	"testing"
)

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
