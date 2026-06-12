package sav

import (
	"fmt"
	"math"
)

// math64 reinterprets little-endian int64 bits as a float64.
func math64(v int64) float64 {
	return math.Float64frombits(uint64(v))
}

// Object data layout, verified empirically against the Char_Player object in
// both a sv46 (legacy tags, UE5 package version 1000) and a sv58 fixture
// (new tags, UE5 1017). Reference: etothepii4 SaveObject/FPropertyTag/
// PropertiesList; UE gate PROPERTY_TAG_COMPLETE_TYPE_NAME = 1012.
//
// Entity (actor) data prelude:
//
//	FObjectReferenceDisc parent (2 FStrings)
//	int32 componentCount x FObjectReferenceDisc
//
// Components have no prelude. Then, in the NEW format only, one
// serialization-control byte. Then the property list: repeated tagged
// properties terminated by a property named "None", followed by int32
// hasGuid (+16 bytes when 1) and class-specific trailing data (ignored).
//
// Legacy tag (object version < 53 OR package UE5 version < 1012):
//
//	FString name ("None" ends the list, nothing follows it in the tag)
//	FString type
//	int32 valueSize, int32 arrayIndex
//	type-specific: Array/Set: FString elementType
//	               Struct: FString structType + 16-byte GUID
//	               Bool: int8 value (the value lives in the tag; size is 0)
//	               Byte/Enum: FString enumName
//	               Map: FString keyType + FString valueType
//	byte hasPropertyGuid (+16 bytes when 1)
//
// New tag (object version >= 53 AND package UE5 version >= 1012):
//
//	FString name
//	FPropertyTagNode tree: FString typeName, int32 childCount, recursive
//	    (e.g. ArrayProperty<IntProperty>, StructProperty<Vector</Script/...>>)
//	int32 valueSize
//	uint8 flags: 0x1 +int32 arrayIndex, 0x2 +16-byte GUID, 0x10 = bool value
//
// valueSize counts only the value bytes that follow the tag, so unknown or
// not-yet-supported property types skip cleanly.
const ue5PropertyTagCompleteTypeName = 1012

// ObjectRef is an FObjectReferenceDisc: a level name + object path pair.
type ObjectRef struct {
	Level string
	Path  string
}

// ObjectData is the decoded form of one object's raw data bytes.
type ObjectData struct {
	Parent     ObjectRef   // actors only
	Components []ObjectRef // actors only
	// Properties maps property name to a decoded value: int64, float64,
	// bool, string, ObjectRef, or []any (arrays and repeated names).
	Properties map[string]any
	// Skipped maps property names that were present but not decoded
	// (structs, maps, unsupported types) to a type description.
	Skipped map[string]string
}

// parseCtx carries the format decisions through nested value parsing.
type parseCtx struct {
	newFormat   bool
	saveVersion int32
}

// ParseObjectData decodes an extracted object's entity prelude and tagged
// property list. Supported struct types decode to typed values; everything
// else (maps, sets, unknown structs, undecodable values) is recorded in
// Skipped — the declared value size keeps the stream aligned regardless.
func ParseObjectData(o Object) (*ObjectData, error) {
	ctx := parseCtx{
		newFormat: o.SaveVersion >= saveVersionPackageVersions &&
			o.PackageVersionUE5 >= ue5PropertyTagCompleteTypeName,
		saveVersion: o.SaveVersion,
	}
	r := newSliceReader(o.Data)
	od := &ObjectData{Properties: map[string]any{}, Skipped: map[string]string{}}

	if o.IsActor {
		var err error
		if od.Parent, err = readObjectRef(r); err != nil {
			return nil, fmt.Errorf("parent ref: %w", err)
		}
		count, err := r.int32("component count")
		if err != nil {
			return nil, err
		}
		if count < 0 || count > maxObjectsPerLevel {
			return nil, fmt.Errorf("implausible component count %d", count)
		}
		for i := range count {
			ref, err := readObjectRef(r)
			if err != nil {
				return nil, fmt.Errorf("component %d/%d: %w", i+1, count, err)
			}
			od.Components = append(od.Components, ref)
		}
	}

	if ctx.newFormat {
		if _, err := r.byte("serialization control"); err != nil {
			return nil, err
		}
	}

	for {
		name, value, skippedType, done, err := parseProperty(r, ctx)
		if err != nil {
			return nil, err
		}
		if done {
			break
		}
		if skippedType != "" {
			od.Skipped[name] = skippedType
			continue
		}
		if existing, dup := od.Properties[name]; dup {
			if slice, ok := existing.([]any); ok {
				od.Properties[name] = append(slice, value)
			} else {
				od.Properties[name] = []any{existing, value}
			}
		} else {
			od.Properties[name] = value
		}
	}

	// int32 hasGuid (+16 bytes) and class-specific trailing data follow;
	// nothing there is needed yet.
	return od, nil
}

func readObjectRef(r *reader) (ObjectRef, error) {
	level, err := r.fstring("reference level")
	if err != nil {
		return ObjectRef{}, err
	}
	path, err := r.fstring("reference path")
	if err != nil {
		return ObjectRef{}, err
	}
	return ObjectRef{Level: level, Path: path}, nil
}

// propertyTag is the format-independent view of one property's tag.
type propertyTag struct {
	name       string
	typ        string
	subtype    string // array/set element type, struct type, map key type
	subsubtype string // array-of-struct struct type (new-format node tree)
	enumName   string // legacy Byte/Enum metadata; new-format Byte child
	size       int32
	boolVal    bool
}

func parsePropertyTag(r *reader, newFormat bool) (propertyTag, bool, error) {
	var tag propertyTag
	var err error
	if tag.name, err = r.fstring("property name"); err != nil {
		return tag, false, err
	}
	if tag.name == "None" {
		return tag, true, nil
	}

	if newFormat {
		if tag.typ, tag.subtype, tag.subsubtype, err = readTagNode(r, 0); err != nil {
			return tag, false, err
		}
		if tag.typ == "ByteProperty" {
			tag.enumName = tag.subtype
		}
		if tag.size, err = r.int32("property size"); err != nil {
			return tag, false, err
		}
		flags, err := r.byte("property flags")
		if err != nil {
			return tag, false, err
		}
		if flags&0x1 != 0 {
			if _, err := r.int32("array index"); err != nil {
				return tag, false, err
			}
		}
		if flags&0x2 != 0 {
			if err := r.discard(16, "property guid"); err != nil {
				return tag, false, err
			}
		}
		tag.boolVal = flags&0x10 != 0
		return tag, false, nil
	}

	if tag.typ, err = r.fstring("property type"); err != nil {
		return tag, false, err
	}
	if tag.size, err = r.int32("property size"); err != nil {
		return tag, false, err
	}
	if _, err := r.int32("array index"); err != nil {
		return tag, false, err
	}
	switch tag.typ {
	case "ArrayProperty", "SetProperty":
		if tag.subtype, err = r.fstring("element type"); err != nil {
			return tag, false, err
		}
	case "StructProperty":
		if tag.subtype, err = r.fstring("struct type"); err != nil {
			return tag, false, err
		}
		if err := r.discard(16, "struct guid"); err != nil {
			return tag, false, err
		}
	case "BoolProperty":
		b, err := r.byte("bool value")
		if err != nil {
			return tag, false, err
		}
		tag.boolVal = b != 0
	case "ByteProperty", "EnumProperty":
		if tag.enumName, err = r.fstring("enum name"); err != nil {
			return tag, false, err
		}
	case "MapProperty":
		if tag.subtype, err = r.fstring("map key type"); err != nil {
			return tag, false, err
		}
		if _, err := r.fstring("map value type"); err != nil {
			return tag, false, err
		}
	}
	hasGUID, err := r.byte("has property guid")
	if err != nil {
		return tag, false, err
	}
	if hasGUID == 1 {
		if err := r.discard(16, "property guid"); err != nil {
			return tag, false, err
		}
	}
	return tag, false, nil
}

// readTagNode parses an FPropertyTagNode tree, returning the root type name,
// the first child's name (element/struct subtype), and the first
// grandchild's name (array-of-struct struct type).
func readTagNode(r *reader, depth int) (name, firstChild, firstGrandchild string, err error) {
	if depth > 8 {
		return "", "", "", fmt.Errorf("property tag type tree deeper than %d", depth)
	}
	if name, err = r.fstring("tag type name"); err != nil {
		return "", "", "", err
	}
	count, err := r.int32("tag type child count")
	if err != nil {
		return "", "", "", err
	}
	if count < 0 || count > 16 {
		return "", "", "", fmt.Errorf("implausible tag type child count %d", count)
	}
	for i := range count {
		child, grandchild, _, err := readTagNode(r, depth+1)
		if err != nil {
			return "", "", "", err
		}
		if i == 0 {
			firstChild, firstGrandchild = child, grandchild
		}
	}
	return name, firstChild, firstGrandchild, nil
}

// parseProperty reads one tagged property. done is true at the "None"
// terminator. A non-empty skippedType means the property was present but
// not decoded. The declared value size frames the stream, so value-level
// failures degrade to Skipped entries instead of aborting the object.
func parseProperty(r *reader, ctx parseCtx) (name string, value any, skippedType string, done bool, err error) {
	tag, done, err := parsePropertyTag(r, ctx.newFormat)
	if err != nil || done {
		return tag.name, nil, "", done, err
	}
	if tag.size < 0 || int64(tag.size) > maxObjectData {
		return tag.name, nil, "", false, fmt.Errorf("property %q: implausible size %d", tag.name, tag.size)
	}

	valueBytes, err := r.bytes(int(tag.size), "property value")
	if err != nil {
		return tag.name, nil, "", false, err
	}
	vr := newSliceReader(valueBytes)
	value, skippedType, valueErr := parsePropertyValue(vr, tag, ctx)
	switch {
	case valueErr != nil:
		return tag.name, nil, fmt.Sprintf("%s (undecoded: %v)", describeTag(tag), valueErr), false, nil
	case skippedType != "":
		return tag.name, nil, skippedType, false, nil
	case vr.off != int64(tag.size):
		return tag.name, nil, fmt.Sprintf("%s (decoded %d of %d bytes)", describeTag(tag), vr.off, tag.size), false, nil
	}
	return tag.name, value, "", false, nil
}

func describeTag(tag propertyTag) string {
	desc := tag.typ
	if tag.subtype != "" {
		desc += "<" + tag.subtype
		if tag.subsubtype != "" {
			desc += "<" + tag.subsubtype + ">"
		}
		desc += ">"
	}
	return desc
}

func parsePropertyValue(r *reader, tag propertyTag, ctx parseCtx) (any, string, error) {
	switch tag.typ {
	case "BoolProperty":
		return tag.boolVal, "", nil
	case "Int8Property":
		b, err := r.byte("int8")
		return int64(int8(b)), "", err
	case "UInt8Property":
		b, err := r.byte("uint8")
		return int64(b), "", err
	case "IntProperty":
		v, err := r.int32("int")
		return int64(v), "", err
	case "UInt32Property":
		v, err := r.int32("uint32")
		return int64(uint32(v)), "", err
	case "Int64Property", "UInt64Property":
		v, err := r.int64("int64")
		return v, "", err
	case "FloatProperty", "SingleProperty":
		buf, err := r.bytes(4, "float")
		if err != nil {
			return nil, "", err
		}
		return float64(f32(buf)), "", nil
	case "DoubleProperty":
		v, err := r.int64("double")
		if err != nil {
			return nil, "", err
		}
		return math64(v), "", nil
	case "StrProperty", "NameProperty":
		s, err := r.fstring("string value")
		return s, "", err
	case "EnumProperty":
		s, err := r.fstring("enum value")
		return s, "", err
	case "ByteProperty":
		if tag.enumName == "" || tag.enumName == "None" {
			b, err := r.byte("byte value")
			return int64(b), "", err
		}
		s, err := r.fstring("byte enum value")
		return s, "", err
	case "ObjectProperty", "InterfaceProperty":
		ref, err := readObjectRef(r)
		return ref, "", err
	case "TextProperty":
		return parseTextValue(r)
	case "ArrayProperty":
		return parseArrayValue(r, tag, ctx)
	case "StructProperty":
		if isUndecodableStruct(tag.subtype) {
			return nil, describeTag(tag), nil
		}
		v, err := parseStructValue(r, tag.subtype, ctx)
		return v, "", err
	default:
		// Map, Set, Text, SoftObject, and anything newer: the value bytes
		// are already framed by size, just record the type.
		return nil, describeTag(tag), nil
	}
}

func parseArrayValue(r *reader, tag propertyTag, ctx parseCtx) (any, string, error) {
	count, err := r.int32("array element count")
	if err != nil {
		return nil, "", err
	}
	if count < 0 || count > maxObjectsPerLevel {
		return nil, "", fmt.Errorf("implausible array element count %d", count)
	}

	if tag.subtype == "StructProperty" {
		return parseStructArray(r, tag, ctx, count)
	}

	readElement := arrayElementReader(tag.subtype)
	if readElement == nil {
		// Unsupported element type: the value bytes are framed by size.
		return nil, describeTag(tag), nil
	}

	values := make([]any, 0, count)
	for i := range count {
		v, err := readElement(r)
		if err != nil {
			return nil, "", fmt.Errorf("element %d/%d: %w", i+1, count, err)
		}
		values = append(values, v)
	}
	return values, "", nil
}

// parseStructArray reads an array of struct values. Legacy saves (< sv53)
// carry an inner property tag naming the struct; new-format saves name it
// in the outer tag's node tree.
func parseStructArray(r *reader, tag propertyTag, ctx parseCtx, count int32) (any, string, error) {
	structName := tag.subsubtype
	if !ctx.newFormat {
		if ctx.saveVersion >= saveVersionPackageVersions {
			// sv >= 53 with legacy tags: no struct name available anywhere.
			// Not observed in real saves (sv53+ always ships UE5 >= 1012).
			return nil, describeTag(tag), nil
		}
		inner, _, err := parsePropertyTag(r, false)
		if err != nil {
			return nil, "", fmt.Errorf("array inner struct tag: %w", err)
		}
		structName = inner.subtype
	}
	if structName == "" || isUndecodableStruct(structName) {
		return nil, "ArrayProperty<StructProperty<" + structName + ">>", nil
	}

	values := make([]any, 0, count)
	for i := range count {
		v, err := parseStructValue(r, structName, ctx)
		if err != nil {
			return nil, "", fmt.Errorf("struct element %d/%d (%s): %w", i+1, count, structName, err)
		}
		values = append(values, v)
	}
	return values, "", nil
}

// parseTextValue decodes an FText: uint32 flags, int8 history type, then a
// history-specific payload. Culture-invariant (None, the form user-entered
// names like train stations take) and Base texts decode; other history
// types degrade to Skipped via the lenient value path.
func parseTextValue(r *reader) (any, string, error) {
	if err := r.discard(4, "text flags"); err != nil {
		return nil, "", err
	}
	historyType, err := r.byte("text history type")
	if err != nil {
		return nil, "", err
	}
	switch int8(historyType) {
	case -1: // None
		hasString, err := r.int32("culture invariant flag")
		if err != nil {
			return nil, "", err
		}
		if hasString != 1 {
			return "", "", nil
		}
		s, err := r.fstring("text value")
		return s, "", err
	case 0: // Base
		if _, err := r.fstring("text namespace"); err != nil {
			return nil, "", err
		}
		if _, err := r.fstring("text key"); err != nil {
			return nil, "", err
		}
		s, err := r.fstring("text source")
		return s, "", err
	default:
		return nil, "", fmt.Errorf("unsupported text history type %d", int8(historyType))
	}
}

// arrayElementReader returns a reader for supported array element types, or
// nil for types that must be skipped.
func arrayElementReader(subtype string) func(*reader) (any, error) {
	switch subtype {
	case "IntProperty":
		return func(r *reader) (any, error) { v, err := r.int32("int element"); return int64(v), err }
	case "Int64Property":
		return func(r *reader) (any, error) { return r.int64("int64 element") }
	case "FloatProperty":
		return func(r *reader) (any, error) {
			buf, err := r.bytes(4, "float element")
			if err != nil {
				return nil, err
			}
			return float64(f32(buf)), nil
		}
	case "DoubleProperty":
		return func(r *reader) (any, error) {
			v, err := r.int64("double element")
			if err != nil {
				return nil, err
			}
			return math64(v), nil
		}
	case "ByteProperty":
		return func(r *reader) (any, error) { b, err := r.byte("byte element"); return int64(b), err }
	case "BoolProperty":
		return func(r *reader) (any, error) { b, err := r.byte("bool element"); return b != 0, err }
	case "StrProperty", "NameProperty", "EnumProperty":
		return func(r *reader) (any, error) { return r.fstring("string element") }
	case "ObjectProperty", "InterfaceProperty":
		return func(r *reader) (any, error) { v, err := readObjectRef(r); return v, err }
	default:
		return nil
	}
}
