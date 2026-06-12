package sav

import (
	"bytes"
	"testing"
)

// writeInventoryItem serializes an FInventoryItem at sv >= 43: item class
// ref, then FGDynamicStruct state.
func writeInventoryItem(buf *bytes.Buffer, itemClass string, withState bool) {
	buf.Write(refBytes("", itemClass))
	if withState {
		writeI32(buf, 1)
		buf.Write(refBytes("", "/Script/FactoryGame.SomeStateStruct"))
		statePayload := fstringBytes("opaque")
		writeI32(buf, int32(len(statePayload)))
		buf.Write(statePayload)
	} else {
		writeI32(buf, 0)
	}
}

// writeInventoryStack writes the generic-struct property list for one
// InventoryStack: Item (StructProperty<InventoryItem>) + NumItems.
func writeInventoryStack(w *propWriter, itemClass string, count int32) {
	item := &bytes.Buffer{}
	writeInventoryItem(item, itemClass, false)
	w.writeTag("Item", "StructProperty", int32(item.Len()), tagMeta{subtype: "InventoryItem"})
	w.buf.Write(item.Bytes())
	w.writeTag("NumItems", "IntProperty", 4, tagMeta{})
	writeI32(w.buf, count)
	w.endList()
}

func TestStructVector(t *testing.T) {
	for _, newFormat := range []bool{false, true} {
		od := parseProps(t, newFormat, false, func(w *propWriter) {
			vec := &bytes.Buffer{}
			writeF64(vec, 1.5)
			writeF64(vec, -2.5)
			writeF64(vec, 300)
			w.writeTag("mPos", "StructProperty", int32(vec.Len()), tagMeta{subtype: "Vector"})
			w.buf.Write(vec.Bytes())
		})
		if od.Properties["mPos"] != [3]float64{1.5, -2.5, 300} {
			t.Errorf("newFormat=%v mPos = %v", newFormat, od.Properties["mPos"])
		}
	}
}

func TestStructGeneric(t *testing.T) {
	for _, newFormat := range []bool{false, true} {
		od := parseProps(t, newFormat, false, func(w *propWriter) {
			inner := newPropWriter(newFormat)
			inner.writeTag("Phase", "IntProperty", 4, tagMeta{})
			writeI32(inner.buf, 3)
			inner.writeTag("Label", "StrProperty", int32(len(fstringBytes("hub"))), tagMeta{})
			inner.buf.Write(fstringBytes("hub"))
			inner.endList()

			w.writeTag("mPhaseInfo", "StructProperty", int32(inner.buf.Len()), tagMeta{subtype: "GamePhaseRecord"})
			w.buf.Write(inner.buf.Bytes())
		})
		got, ok := od.Properties["mPhaseInfo"].(map[string]any)
		if !ok {
			t.Fatalf("newFormat=%v mPhaseInfo = %v (%T)",
				newFormat, od.Properties["mPhaseInfo"], od.Properties["mPhaseInfo"])
		}
		if got["Phase"] != int64(3) || got["Label"] != "hub" {
			t.Errorf("newFormat=%v mPhaseInfo = %v", newFormat, got)
		}
	}
}

func TestStructInventoryItem(t *testing.T) {
	od := parseProps(t, false, false, func(w *propWriter) {
		item := &bytes.Buffer{}
		writeInventoryItem(item, "/Game/.../Desc_IronPlate.Desc_IronPlate_C", true)
		w.writeTag("mItem", "StructProperty", int32(item.Len()), tagMeta{subtype: "InventoryItem"})
		w.buf.Write(item.Bytes())
	})
	item, ok := od.Properties["mItem"].(InventoryItem)
	if !ok {
		t.Fatalf("mItem = %v (%T)", od.Properties["mItem"], od.Properties["mItem"])
	}
	if item.ItemClass != "/Game/.../Desc_IronPlate.Desc_IronPlate_C" || !item.HasState {
		t.Errorf("mItem = %+v", item)
	}
}

// Array of InventoryStack — the central inventory shape. Legacy format
// carries an inner struct tag before the elements; the new format names the
// struct in the tag's node tree instead.
func TestStructArrayInventoryStacks(t *testing.T) {
	for _, newFormat := range []bool{false, true} {
		od := parseProps(t, newFormat, false, func(w *propWriter) {
			stacks := newPropWriter(newFormat)
			writeInventoryStack(stacks, "/Game/X.Desc_IronOre_C", 64)
			writeInventoryStack(stacks, "/Game/X.Desc_Coal_C", 13)

			arr := &bytes.Buffer{}
			writeI32(arr, 2) // element count
			if !newFormat {
				// Inner legacy struct tag.
				inner := newPropWriter(false)
				inner.writeTag("mInventoryStacks", "StructProperty",
					int32(stacks.buf.Len()), tagMeta{subtype: "InventoryStack"})
				arr.Write(inner.buf.Bytes())
			}
			arr.Write(stacks.buf.Bytes())

			w.writeTag("mInventoryStacks", "ArrayProperty", int32(arr.Len()),
				tagMeta{subtype: "StructProperty", subsubtype: "InventoryStack"})
			w.buf.Write(arr.Bytes())
		})

		vals, ok := od.Properties["mInventoryStacks"].([]any)
		if !ok || len(vals) != 2 {
			t.Fatalf("newFormat=%v mInventoryStacks = %v", newFormat, od.Properties["mInventoryStacks"])
		}
		first, ok := vals[0].(map[string]any)
		if !ok {
			t.Fatalf("newFormat=%v stack[0] = %T", newFormat, vals[0])
		}
		item, ok := first["Item"].(InventoryItem)
		if !ok || item.ItemClass != "/Game/X.Desc_IronOre_C" {
			t.Errorf("newFormat=%v stack[0].Item = %v", newFormat, first["Item"])
		}
		if first["NumItems"] != int64(64) {
			t.Errorf("newFormat=%v stack[0].NumItems = %v", newFormat, first["NumItems"])
		}
		second, ok := vals[1].(map[string]any)
		if !ok {
			t.Fatalf("newFormat=%v stack[1] = %T", newFormat, vals[1])
		}
		if second["NumItems"] != int64(13) {
			t.Errorf("newFormat=%v stack[1].NumItems = %v", newFormat, second["NumItems"])
		}
	}
}

func TestStructUnknownBinaryStillSkipped(t *testing.T) {
	od := parseProps(t, false, false, func(w *propWriter) {
		w.writeTag("mNetId", "StructProperty", 12, tagMeta{subtype: "UniqueNetIdRepl"})
		w.buf.Write(make([]byte, 12))
		w.writeTag("after", "IntProperty", 4, tagMeta{})
		writeI32(w.buf, 5)
	})
	if _, decoded := od.Properties["mNetId"]; decoded {
		t.Error("UniqueNetIdRepl unexpectedly decoded")
	}
	if od.Skipped["mNetId"] == "" {
		t.Errorf("Skipped = %v", od.Skipped)
	}
	if od.Properties["after"] != int64(5) {
		t.Errorf("after = %v (stream misaligned)", od.Properties["after"])
	}
}

// A value whose bytes don't parse must become a Skipped entry, never an
// error and never stream misalignment — the size field is authoritative.
func TestCorruptValueBecomesSkipped(t *testing.T) {
	od := parseProps(t, false, false, func(w *propWriter) {
		// Generic struct whose payload is garbage (not a property list).
		w.writeTag("mBroken", "StructProperty", 8, tagMeta{subtype: "SomeFutureStruct"})
		w.buf.Write([]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF})
		w.writeTag("after", "IntProperty", 4, tagMeta{})
		writeI32(w.buf, 11)
	})
	if od.Skipped["mBroken"] == "" {
		t.Errorf("mBroken not in Skipped: %v", od.Skipped)
	}
	if od.Properties["after"] != int64(11) {
		t.Errorf("after = %v (stream misaligned)", od.Properties["after"])
	}
}
