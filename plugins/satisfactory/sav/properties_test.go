package sav

import (
	"bytes"
	"strings"
	"testing"
)

// Synthetic property-blob builders mirroring the two tag formats verified
// against real fixtures (see properties.go layout comment).

type propWriter struct {
	buf       *bytes.Buffer
	newFormat bool
}

func newPropWriter(newFormat bool) *propWriter {
	return &propWriter{buf: &bytes.Buffer{}, newFormat: newFormat}
}

// tagMeta carries the per-type tag fields that differ between formats.
type tagMeta struct {
	subtype  string // array/set element type, struct type, map key type
	enumName string // legacy Byte/Enum tag metadata
	boolVal  bool
}

func (w *propWriter) writeTag(name, typ string, size int32, meta tagMeta) {
	writeFString(w.buf, name, false)
	if w.newFormat {
		// FPropertyTagNode tree: root type with optional subtype child.
		writeFString(w.buf, typ, false)
		children := 0
		if meta.subtype != "" || (typ == "ByteProperty" && meta.enumName != "" && meta.enumName != "None") {
			children = 1
		}
		writeI32(w.buf, int32(children))
		if children == 1 {
			child := meta.subtype
			if child == "" {
				child = meta.enumName
			}
			writeFString(w.buf, child, false)
			writeI32(w.buf, 0) // grandchildren
		}
		writeI32(w.buf, size)
		var flags byte
		if typ == "BoolProperty" && meta.boolVal {
			flags |= 0x10
		}
		w.buf.WriteByte(flags)
		return
	}

	writeFString(w.buf, typ, false)
	writeI32(w.buf, size)
	writeI32(w.buf, 0) // array index
	switch typ {
	case "ArrayProperty", "SetProperty":
		writeFString(w.buf, meta.subtype, false)
	case "StructProperty":
		writeFString(w.buf, meta.subtype, false)
		w.buf.Write(make([]byte, 16)) // struct GUID
	case "BoolProperty":
		if meta.boolVal {
			w.buf.WriteByte(1)
		} else {
			w.buf.WriteByte(0)
		}
	case "ByteProperty", "EnumProperty":
		writeFString(w.buf, meta.enumName, false)
	case "MapProperty":
		writeFString(w.buf, meta.subtype, false)
		writeFString(w.buf, "IntProperty", false)
	}
	w.buf.WriteByte(0) // no property GUID
}

func (w *propWriter) end() {
	writeFString(w.buf, "None", false)
	writeI32(w.buf, 0) // hasGuid
}

func fstringBytes(s string) []byte {
	b := &bytes.Buffer{}
	writeFString(b, s, false)
	return b.Bytes()
}

func refBytes(level, path string) []byte {
	b := &bytes.Buffer{}
	writeFString(b, level, false)
	writeFString(b, path, false)
	return b.Bytes()
}

// buildPropObject assembles an Object whose Data holds the given property
// blob, optionally prefixed with an entity prelude.
func buildPropObject(t *testing.T, newFormat, isActor bool, props func(w *propWriter)) Object {
	t.Helper()
	data := &bytes.Buffer{}
	if isActor {
		data.Write(refBytes("", ""))                                     // parent
		writeI32(data, 1)                                                // component count
		data.Write(refBytes("Persistent_Level", "Pawn.HealthComponent")) // component
	}
	if newFormat {
		data.WriteByte(0) // serialization control
	}
	w := newPropWriter(newFormat)
	props(w)
	w.end()
	data.Write(w.buf.Bytes())

	saveVersion := int32(46)
	ue5 := int32(1000)
	if newFormat {
		saveVersion = 58
		ue5 = 1017
	}
	return Object{
		ObjectHeader:      ObjectHeader{ClassPath: "/Test/Class", InstanceName: "Test.Instance", IsActor: isActor},
		SaveVersion:       saveVersion,
		PackageVersionUE5: ue5,
		Data:              data.Bytes(),
	}
}

func parseProps(t *testing.T, newFormat, isActor bool, props func(w *propWriter)) *ObjectData {
	t.Helper()
	od, err := ParseObjectData(buildPropObject(t, newFormat, isActor, props))
	if err != nil {
		t.Fatalf("ParseObjectData: %v", err)
	}
	return od
}

func writeAllPrimitives(w *propWriter) {
	w.writeTag("anInt", "IntProperty", 4, tagMeta{})
	writeI32(w.buf, -42)
	w.writeTag("aFloat", "FloatProperty", 4, tagMeta{})
	writeF32(w.buf, 2.5)
	w.writeTag("aBool", "BoolProperty", 0, tagMeta{boolVal: true})
	w.writeTag("aString", "StrProperty", int32(len(fstringBytes("hello"))), tagMeta{})
	w.buf.Write(fstringBytes("hello"))
	w.writeTag("aName", "NameProperty", int32(len(fstringBytes("SomeName"))), tagMeta{})
	w.buf.Write(fstringBytes("SomeName"))
	enumVal := fstringBytes("EGamePhase::EGP_MidGame")
	w.writeTag("anEnum", "EnumProperty", int32(len(enumVal)), tagMeta{enumName: "EGamePhase"})
	w.buf.Write(enumVal)
	ref := refBytes("Persistent_Level", "Some.Machine")
	w.writeTag("anObject", "ObjectProperty", int32(len(ref)), tagMeta{})
	w.buf.Write(ref)
	w.writeTag("anInt64", "Int64Property", 8, tagMeta{})
	writeI64(w.buf, 1<<40)
	w.writeTag("aDouble", "DoubleProperty", 8, tagMeta{})
	writeF64(w.buf, 0.125)
	w.writeTag("rawByte", "ByteProperty", 1, tagMeta{enumName: "None"})
	w.buf.WriteByte(7)
}

func checkPrimitives(t *testing.T, od *ObjectData) {
	t.Helper()
	want := map[string]any{
		"anInt":    int64(-42),
		"aFloat":   float64(2.5),
		"aBool":    true,
		"aString":  "hello",
		"aName":    "SomeName",
		"anEnum":   "EGamePhase::EGP_MidGame",
		"anObject": ObjectRef{Level: "Persistent_Level", Path: "Some.Machine"},
		"anInt64":  int64(1 << 40),
		"aDouble":  0.125,
		"rawByte":  int64(7),
	}
	for name, expect := range want {
		got, ok := od.Properties[name]
		if !ok {
			t.Errorf("property %q missing", name)
			continue
		}
		if got != expect {
			t.Errorf("property %q = %v (%T), want %v (%T)", name, got, got, expect, expect)
		}
	}
}

func TestParsePrimitivesLegacy(t *testing.T) {
	checkPrimitives(t, parseProps(t, false, false, writeAllPrimitives))
}

func TestParsePrimitivesNewFormat(t *testing.T) {
	checkPrimitives(t, parseProps(t, true, false, writeAllPrimitives))
}

func TestParseEntityPrelude(t *testing.T) {
	od := parseProps(t, false, true, func(w *propWriter) {
		w.writeTag("x", "IntProperty", 4, tagMeta{})
		writeI32(w.buf, 1)
	})
	if len(od.Components) != 1 || od.Components[0].Path != "Pawn.HealthComponent" {
		t.Errorf("Components = %v", od.Components)
	}
	if od.Properties["x"] != int64(1) {
		t.Errorf("x = %v", od.Properties["x"])
	}
}

func TestParseArrays(t *testing.T) {
	for _, newFormat := range []bool{false, true} {
		od := parseProps(t, newFormat, false, func(w *propWriter) {
			ints := &bytes.Buffer{}
			writeI32(ints, 3)
			for _, v := range []int32{10, 20, 30} {
				writeI32(ints, v)
			}
			w.writeTag("intArr", "ArrayProperty", int32(ints.Len()), tagMeta{subtype: "IntProperty"})
			w.buf.Write(ints.Bytes())

			objs := &bytes.Buffer{}
			writeI32(objs, 2)
			objs.Write(refBytes("", "A.B"))
			objs.Write(refBytes("", "C.D"))
			w.writeTag("objArr", "ArrayProperty", int32(objs.Len()), tagMeta{subtype: "ObjectProperty"})
			w.buf.Write(objs.Bytes())

			strs := &bytes.Buffer{}
			writeI32(strs, 1)
			strs.Write(fstringBytes("only"))
			w.writeTag("strArr", "ArrayProperty", int32(strs.Len()), tagMeta{subtype: "StrProperty"})
			w.buf.Write(strs.Bytes())
		})

		ints, _ := od.Properties["intArr"].([]any)
		if len(ints) != 3 || ints[0] != int64(10) || ints[2] != int64(30) {
			t.Errorf("newFormat=%v intArr = %v", newFormat, od.Properties["intArr"])
		}
		objs, _ := od.Properties["objArr"].([]any)
		if len(objs) != 2 || objs[1] != (ObjectRef{Path: "C.D"}) {
			t.Errorf("newFormat=%v objArr = %v", newFormat, od.Properties["objArr"])
		}
		strs, _ := od.Properties["strArr"].([]any)
		if len(strs) != 1 || strs[0] != "only" {
			t.Errorf("newFormat=%v strArr = %v", newFormat, od.Properties["strArr"])
		}
	}
}

func TestStructAndUnknownSkipped(t *testing.T) {
	for _, newFormat := range []bool{false, true} {
		od := parseProps(t, newFormat, false, func(w *propWriter) {
			w.writeTag("aVector", "StructProperty", 12, tagMeta{subtype: "Vector"})
			w.buf.Write(make([]byte, 12))
			w.writeTag("after", "IntProperty", 4, tagMeta{})
			writeI32(w.buf, 99)
		})
		if _, decoded := od.Properties["aVector"]; decoded {
			t.Errorf("newFormat=%v: struct property unexpectedly decoded", newFormat)
		}
		if od.Skipped["aVector"] == "" || !strings.Contains(od.Skipped["aVector"], "Struct") {
			t.Errorf("newFormat=%v Skipped = %v", newFormat, od.Skipped)
		}
		if od.Properties["after"] != int64(99) {
			t.Errorf("newFormat=%v property after struct = %v (skip misaligned?)", newFormat, od.Properties["after"])
		}
	}
}

func TestDuplicateNamesCoalesce(t *testing.T) {
	od := parseProps(t, false, false, func(w *propWriter) {
		for _, v := range []int32{1, 2, 3} {
			w.writeTag("multi", "IntProperty", 4, tagMeta{})
			writeI32(w.buf, v)
		}
	})
	vals, ok := od.Properties["multi"].([]any)
	if !ok || len(vals) != 3 || vals[2] != int64(3) {
		t.Errorf("multi = %v", od.Properties["multi"])
	}
}

func TestByteEnumValue(t *testing.T) {
	od := parseProps(t, false, false, func(w *propWriter) {
		val := fstringBytes("EFoo::Bar")
		w.writeTag("byteEnum", "ByteProperty", int32(len(val)), tagMeta{enumName: "EFoo"})
		w.buf.Write(val)
	})
	if od.Properties["byteEnum"] != "EFoo::Bar" {
		t.Errorf("byteEnum = %v", od.Properties["byteEnum"])
	}
}

func TestParseTruncated(t *testing.T) {
	obj := buildPropObject(t, false, false, writeAllPrimitives)
	for _, n := range []int{1, 10, len(obj.Data) / 2} {
		short := obj
		short.Data = obj.Data[:n]
		if _, err := ParseObjectData(short); err == nil {
			t.Errorf("ParseObjectData(%d bytes) = nil error, want failure", n)
		}
	}
}

func TestSizeMismatchErrors(t *testing.T) {
	obj := buildPropObject(t, false, false, func(w *propWriter) {
		w.writeTag("liar", "IntProperty", 8, tagMeta{}) // claims 8, writes 4
		writeI32(w.buf, 5)
	})
	if _, err := ParseObjectData(obj); err == nil {
		t.Error("ParseObjectData = nil error, want size mismatch failure")
	}
}
