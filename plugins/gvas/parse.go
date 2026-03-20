package gvas

import (
	"bytes"
	"fmt"
	"io"
)

const gvasMagic = 0x53415647

// Safety limits for untrusted input.
const (
	maxStringBytes  = 10 << 20  // 10 MB max FString length
	maxArrayCount   = 1_000_000 // 1M max elements in array/map/set
	maxRecurseDepth = 64        // max nesting depth for type trees / struct properties
)

// Property tag flag bits (UE 5.4 EPropertyTagFlags).
const (
	flagHasArrayIndex            = 0x01
	flagHasPropertyGuid          = 0x02
	flagBoolTrue                 = 0x04
	flagHasPropertyExtensions    = 0x08
	flagHasBinaryOrNativeSeriali = 0x10
)

// Parse reads a GVAS binary save file and returns the parsed Save.
func Parse(r io.Reader) (*Save, error) {
	rd := newReader(r)
	save := &Save{}

	if err := readHeader(rd, &save.Header); err != nil {
		return nil, fmt.Errorf("header: %w", err)
	}

	sgt, err := rd.readFString()
	if err != nil {
		return nil, fmt.Errorf("save game type: %w", err)
	}
	save.SaveGameType = sgt

	// Reserved byte present when engine >= 5.4.
	if save.Header.EngineVersionMajor > 5 ||
		(save.Header.EngineVersionMajor == 5 && save.Header.EngineVersionMinor >= 4) {
		if _, err := rd.readU8(); err != nil {
			return nil, fmt.Errorf("reserved byte: %w", err)
		}
	}

	props, err := readProperties(rd)
	if err != nil {
		return nil, fmt.Errorf("root properties: %w", err)
	}
	save.Properties = props

	extra, err := io.ReadAll(rd.r)
	if err != nil {
		return nil, fmt.Errorf("extra data: %w", err)
	}
	save.Extra = extra

	return save, nil
}

func readHeader(rd *reader, h *Header) error {
	magic, err := rd.readU32()
	if err != nil {
		return fmt.Errorf("magic: %w", err)
	}
	if magic != gvasMagic {
		return fmt.Errorf("bad magic: got 0x%08X, want 0x%08X", magic, gvasMagic)
	}
	h.Magic = magic

	h.SaveGameVersion, err = rd.readU32()
	if err != nil {
		return fmt.Errorf("save game version: %w", err)
	}

	h.PackageVersionUE4, err = rd.readU32()
	if err != nil {
		return fmt.Errorf("package version ue4: %w", err)
	}

	// PackageVersionUE5 present when save_game_version >= 3 && != 34.
	if h.SaveGameVersion >= 3 && h.SaveGameVersion != 34 {
		h.PackageVersionUE5, err = rd.readU32()
		if err != nil {
			return fmt.Errorf("package version ue5: %w", err)
		}
	}

	h.EngineVersionMajor, err = rd.readU16()
	if err != nil {
		return fmt.Errorf("engine major: %w", err)
	}
	h.EngineVersionMinor, err = rd.readU16()
	if err != nil {
		return fmt.Errorf("engine minor: %w", err)
	}
	h.EngineVersionPatch, err = rd.readU16()
	if err != nil {
		return fmt.Errorf("engine patch: %w", err)
	}
	h.EngineVersionBuild, err = rd.readU32()
	if err != nil {
		return fmt.Errorf("engine build: %w", err)
	}
	h.EngineVersionStr, err = rd.readFString()
	if err != nil {
		return fmt.Errorf("engine version string: %w", err)
	}

	cvFormat, err := rd.readU32()
	if err != nil {
		return fmt.Errorf("custom version format: %w", err)
	}
	_ = cvFormat

	cvCount, err := rd.readU32()
	if err != nil {
		return fmt.Errorf("custom version count: %w", err)
	}

	h.CustomVersions = make([]CustomVersion, cvCount)
	for i := range h.CustomVersions {
		g, err := rd.readGuid()
		if err != nil {
			return fmt.Errorf("custom version %d guid: %w", i, err)
		}
		v, err := rd.readI32()
		if err != nil {
			return fmt.Errorf("custom version %d value: %w", i, err)
		}
		h.CustomVersions[i] = CustomVersion{GUID: g, Value: v}
	}

	return nil
}

// readTypeTree reads a recursive PropertyTagNode.
func readTypeTree(rd *reader) (TypeTreeNode, error) {
	return readTypeTreeDepth(rd, 0)
}

func readTypeTreeDepth(rd *reader, depth int) (TypeTreeNode, error) {
	if depth > maxRecurseDepth {
		return TypeTreeNode{}, fmt.Errorf("type tree exceeds max depth %d", maxRecurseDepth)
	}
	name, err := rd.readFString()
	if err != nil {
		return TypeTreeNode{}, fmt.Errorf("type tree name: %w", err)
	}
	count, err := rd.readU32()
	if err != nil {
		return TypeTreeNode{}, fmt.Errorf("type tree inner count: %w", err)
	}
	if count > maxArrayCount {
		return TypeTreeNode{}, fmt.Errorf("type tree child count %d exceeds limit", count)
	}
	children := make([]TypeTreeNode, count)
	for i := range children {
		children[i], err = readTypeTreeDepth(rd, depth+1)
		if err != nil {
			return TypeTreeNode{}, fmt.Errorf("type tree child %d: %w", i, err)
		}
	}
	return TypeTreeNode{Name: name, Children: children}, nil
}

// readProperties reads a property list terminated by a "None" name.
func readProperties(rd *reader) (Properties, error) {
	var props Properties
	for {
		name, err := rd.readFString()
		if err != nil {
			return nil, fmt.Errorf("property name: %w", err)
		}
		if name == "None" {
			return props, nil
		}

		tree, err := readTypeTree(rd)
		if err != nil {
			return nil, fmt.Errorf("property %q type tree: %w", name, err)
		}

		size, err := rd.readU32()
		if err != nil {
			return nil, fmt.Errorf("property %q size: %w", name, err)
		}

		flags, err := rd.readU8()
		if err != nil {
			return nil, fmt.Errorf("property %q flags: %w", name, err)
		}

		prop := Property{
			Name:     name,
			TypeTree: tree,
		}

		if flags&flagHasArrayIndex != 0 {
			prop.ArrayIndex, err = rd.readU32()
			if err != nil {
				return nil, fmt.Errorf("property %q array index: %w", name, err)
			}
		}

		if flags&flagHasPropertyGuid != 0 {
			g, err := rd.readGuid()
			if err != nil {
				return nil, fmt.Errorf("property %q guid: %w", name, err)
			}
			prop.PropGUID = &g
		}

		val, err := readPropertyValue(rd, tree, size, flags)
		if err != nil {
			return nil, fmt.Errorf("property %q value: %w", name, err)
		}
		prop.Value = val

		props = append(props, prop)
	}
}

// readPropertyValue reads a property value based on its type tree, size, and flags.
func readPropertyValue(rd *reader, tree TypeTreeNode, size uint32, flags uint8) (Value, error) {
	switch tree.Name {
	case "IntProperty":
		v, err := rd.readI32()
		if err != nil {
			return nil, err
		}
		return IntValue{V: v}, nil

	case "UInt32Property":
		v, err := rd.readU32()
		if err != nil {
			return nil, err
		}
		return UInt32Value{V: v}, nil

	case "Int64Property":
		v, err := rd.readI64()
		if err != nil {
			return nil, err
		}
		return Int64Value{V: v}, nil

	case "UInt64Property":
		v, err := rd.readU64()
		if err != nil {
			return nil, err
		}
		return UInt64Value{V: v}, nil

	case "FloatProperty":
		v, err := rd.readF32()
		if err != nil {
			return nil, err
		}
		return FloatValue{V: v}, nil

	case "DoubleProperty":
		v, err := rd.readF64()
		if err != nil {
			return nil, err
		}
		return Float64Value{V: v}, nil

	case "BoolProperty":
		// In property tag context, value is in the flags byte. Size is 0.
		return BoolValue{V: flags&flagBoolTrue != 0}, nil

	case "NameProperty":
		v, err := rd.readFString()
		if err != nil {
			return nil, err
		}
		return NameValue{V: v}, nil

	case "StrProperty":
		v, err := rd.readFString()
		if err != nil {
			return nil, err
		}
		return StrValue{V: v}, nil

	case "ObjectProperty":
		v, err := rd.readFString()
		if err != nil {
			return nil, err
		}
		return ObjectValue{V: v}, nil

	case "EnumProperty":
		v, err := rd.readFString()
		if err != nil {
			return nil, err
		}
		return EnumValue{V: v}, nil

	case "ByteProperty":
		if len(tree.Children) > 0 {
			// Enum-style ByteProperty: read FString.
			v, err := rd.readFString()
			if err != nil {
				return nil, err
			}
			return ByteEnumValue{V: v}, nil
		}
		v, err := rd.readU8()
		if err != nil {
			return nil, err
		}
		return ByteValue{V: v}, nil

	case "SoftObjectProperty":
		return readSoftObject(rd)

	case "TextProperty":
		data, err := rd.readBytes(int(size))
		if err != nil {
			return nil, err
		}
		return TextValue{Data: data}, nil

	case "StructProperty":
		return readStructProperty(rd, tree, size, flags)

	case "ArrayProperty":
		return readArrayProperty(rd, tree, size)

	case "MapProperty":
		return readMapProperty(rd, tree)

	case "SetProperty":
		return readSetProperty(rd, tree, size)

	default:
		// Unknown type: read raw bytes.
		data, err := rd.readBytes(int(size))
		if err != nil {
			return nil, fmt.Errorf("unknown type %q: %w", tree.Name, err)
		}
		return RawValue{TypeName: tree.Name, Data: data}, nil
	}
}

func readSoftObject(rd *reader) (Value, error) {
	s1, err := rd.readFString()
	if err != nil {
		return nil, fmt.Errorf("soft object asset path: %w", err)
	}
	s2, err := rd.readFString()
	if err != nil {
		return nil, fmt.Errorf("soft object package: %w", err)
	}
	s3, err := rd.readFString()
	if err != nil {
		return nil, fmt.Errorf("soft object asset name: %w", err)
	}
	return SoftObjectValue{AssetPathName: s1, PackageName: s2, AssetName: s3}, nil
}

// structShortName extracts the short type name from a struct type tree node.
// The tree for a struct has children: [StructName[PackageName], optional GUID].
// The short name is just the StructName child's Name.
func structShortName(tree TypeTreeNode) string {
	if len(tree.Children) == 0 {
		return ""
	}
	name := tree.Children[0].Name
	if len(tree.Children[0].Children) > 0 {
		// Full path = "{Package}.{Name}" — name is the first child's Name.
		return name
	}
	return name
}

// structFullPath builds the full struct type path from the type tree.
func structFullPath(tree TypeTreeNode) string {
	if len(tree.Children) == 0 {
		return ""
	}
	structNode := tree.Children[0]
	name := structNode.Name
	if len(structNode.Children) > 0 {
		pkg := structNode.Children[0].Name
		return pkg + "." + name
	}
	return name
}

func readStructProperty(rd *reader, tree TypeTreeNode, size uint32, flags uint8) (Value, error) {
	shortName := structShortName(tree)
	fullPath := structFullPath(tree)

	if flags&flagHasBinaryOrNativeSeriali != 0 {
		data, err := rd.readBytes(int(size))
		if err != nil {
			return nil, fmt.Errorf("binary struct %q: %w", shortName, err)
		}
		return RawStructValue{StructType: fullPath, Data: data}, nil
	}

	return readStructValue(rd, shortName, fullPath)
}

// readStructValue reads a struct value given its short name and full path.
// Used by both property tags and array/map element reading.
func readStructValue(rd *reader, shortName, fullPath string) (Value, error) {
	switch shortName {
	case "Guid":
		g, err := rd.readGuid()
		if err != nil {
			return nil, err
		}
		return GuidValue{V: g}, nil

	case "DateTime":
		v, err := rd.readU64()
		if err != nil {
			return nil, err
		}
		return DateTimeValue{V: v}, nil

	case "Vector":
		x, err := rd.readF64()
		if err != nil {
			return nil, err
		}
		y, err := rd.readF64()
		if err != nil {
			return nil, err
		}
		z, err := rd.readF64()
		if err != nil {
			return nil, err
		}
		return VectorValue{X: x, Y: y, Z: z}, nil

	case "Quat":
		x, err := rd.readF64()
		if err != nil {
			return nil, err
		}
		y, err := rd.readF64()
		if err != nil {
			return nil, err
		}
		z, err := rd.readF64()
		if err != nil {
			return nil, err
		}
		w, err := rd.readF64()
		if err != nil {
			return nil, err
		}
		return QuatValue{X: x, Y: y, Z: z, W: w}, nil

	case "Rotator":
		p, err := rd.readF64()
		if err != nil {
			return nil, err
		}
		y, err := rd.readF64()
		if err != nil {
			return nil, err
		}
		r, err := rd.readF64()
		if err != nil {
			return nil, err
		}
		return RotatorValue{Pitch: p, Yaw: y, Roll: r}, nil

	case "LinearColor":
		r, err := rd.readF32()
		if err != nil {
			return nil, err
		}
		g, err := rd.readF32()
		if err != nil {
			return nil, err
		}
		b, err := rd.readF32()
		if err != nil {
			return nil, err
		}
		a, err := rd.readF32()
		if err != nil {
			return nil, err
		}
		return LinearColorValue{R: r, G: g, B: b, A: a}, nil

	case "GameplayTagContainer":
		count, err := rd.readU32()
		if err != nil {
			return nil, err
		}
		tags := make([]string, count)
		for i := range tags {
			tags[i], err = rd.readFString()
			if err != nil {
				return nil, fmt.Errorf("tag %d: %w", i, err)
			}
		}
		return GameplayTagContainerValue{Tags: tags}, nil

	case "Vector2D":
		x, err := rd.readF64()
		if err != nil {
			return nil, err
		}
		y, err := rd.readF64()
		if err != nil {
			return nil, err
		}
		return Vector2DValue{X: x, Y: y}, nil

	case "IntPoint":
		x, err := rd.readI32()
		if err != nil {
			return nil, err
		}
		y, err := rd.readI32()
		if err != nil {
			return nil, err
		}
		return IntPointValue{X: x, Y: y}, nil

	default:
		// Generic struct: read nested properties until "None".
		props, err := readProperties(rd)
		if err != nil {
			return nil, fmt.Errorf("struct %q properties: %w", shortName, err)
		}
		return StructValue{StructType: fullPath, Properties: props}, nil
	}
}

func readArrayProperty(rd *reader, tree TypeTreeNode, size uint32) (Value, error) {
	if len(tree.Children) == 0 {
		return nil, fmt.Errorf("array with no element type tree")
	}
	elemTree := tree.Children[0]

	count, err := rd.readU32()
	if err != nil {
		return nil, fmt.Errorf("array count: %w", err)
	}
	if count > maxArrayCount {
		return nil, fmt.Errorf("array count %d exceeds limit", count)
	}

	elements := make([]Value, count)

	switch elemTree.Name {
	case "StructProperty":
		shortName := structShortName(elemTree)
		fullPath := structFullPath(elemTree)
		for i := range elements {
			v, err := readStructValue(rd, shortName, fullPath)
			if err != nil {
				return nil, fmt.Errorf("array struct element %d: %w", i, err)
			}
			elements[i] = v
		}

	case "ByteProperty":
		if len(elemTree.Children) > 0 {
			// Enum byte array: each element is an FString.
			for i := range elements {
				s, err := rd.readFString()
				if err != nil {
					return nil, fmt.Errorf("array byte enum element %d: %w", i, err)
				}
				elements[i] = ByteEnumValue{V: s}
			}
		} else {
			// Raw byte array.
			for i := range elements {
				b, err := rd.readU8()
				if err != nil {
					return nil, fmt.Errorf("array byte element %d: %w", i, err)
				}
				elements[i] = ByteValue{V: b}
			}
		}

	default:
		for i := range elements {
			v, err := readValueByType(rd, elemTree)
			if err != nil {
				return nil, fmt.Errorf("array element %d: %w", i, err)
			}
			elements[i] = v
		}
	}

	return ArrayValue{Elements: elements}, nil
}

// readValueByType reads a single value in a non-property-tag context
// (used for array elements and map keys/values).
func readValueByType(rd *reader, tree TypeTreeNode) (Value, error) {
	switch tree.Name {
	case "IntProperty":
		v, err := rd.readI32()
		if err != nil {
			return nil, err
		}
		return IntValue{V: v}, nil

	case "UInt32Property":
		v, err := rd.readU32()
		if err != nil {
			return nil, err
		}
		return UInt32Value{V: v}, nil

	case "Int64Property":
		v, err := rd.readI64()
		if err != nil {
			return nil, err
		}
		return Int64Value{V: v}, nil

	case "UInt64Property":
		v, err := rd.readU64()
		if err != nil {
			return nil, err
		}
		return UInt64Value{V: v}, nil

	case "FloatProperty":
		v, err := rd.readF32()
		if err != nil {
			return nil, err
		}
		return FloatValue{V: v}, nil

	case "DoubleProperty":
		v, err := rd.readF64()
		if err != nil {
			return nil, err
		}
		return Float64Value{V: v}, nil

	case "BoolProperty":
		// In non-tag context, bool is a u8.
		b, err := rd.readU8()
		if err != nil {
			return nil, err
		}
		return BoolValue{V: b != 0}, nil

	case "NameProperty":
		v, err := rd.readFString()
		if err != nil {
			return nil, err
		}
		return NameValue{V: v}, nil

	case "StrProperty":
		v, err := rd.readFString()
		if err != nil {
			return nil, err
		}
		return StrValue{V: v}, nil

	case "ObjectProperty":
		v, err := rd.readFString()
		if err != nil {
			return nil, err
		}
		return ObjectValue{V: v}, nil

	case "EnumProperty":
		v, err := rd.readFString()
		if err != nil {
			return nil, err
		}
		return EnumValue{V: v}, nil

	case "ByteProperty":
		if len(tree.Children) > 0 {
			v, err := rd.readFString()
			if err != nil {
				return nil, err
			}
			return ByteEnumValue{V: v}, nil
		}
		v, err := rd.readU8()
		if err != nil {
			return nil, err
		}
		return ByteValue{V: v}, nil

	case "SoftObjectProperty":
		return readSoftObject(rd)

	case "StructProperty":
		shortName := structShortName(tree)
		fullPath := structFullPath(tree)
		return readStructValue(rd, shortName, fullPath)

	default:
		return nil, fmt.Errorf("unsupported value type %q in non-tag context", tree.Name)
	}
}

func readMapProperty(rd *reader, tree TypeTreeNode) (Value, error) {
	if len(tree.Children) < 2 {
		return nil, fmt.Errorf("map with fewer than 2 type tree children")
	}
	keyTree := tree.Children[0]
	valTree := tree.Children[1]

	keysToRemove, err := rd.readU32()
	if err != nil {
		return nil, fmt.Errorf("map keys to remove count: %w", err)
	}
	// Skip removed keys.
	for range keysToRemove {
		if _, err := readValueByType(rd, keyTree); err != nil {
			return nil, fmt.Errorf("map skip removed key: %w", err)
		}
	}

	entryCount, err := rd.readU32()
	if err != nil {
		return nil, fmt.Errorf("map entry count: %w", err)
	}
	if entryCount > maxArrayCount {
		return nil, fmt.Errorf("map entry count %d exceeds limit", entryCount)
	}

	entries := make([]MapEntry, entryCount)
	for i := range entries {
		k, err := readValueByType(rd, keyTree)
		if err != nil {
			return nil, fmt.Errorf("map entry %d key: %w", i, err)
		}
		v, err := readValueByType(rd, valTree)
		if err != nil {
			return nil, fmt.Errorf("map entry %d value: %w", i, err)
		}
		entries[i] = MapEntry{Key: k, Value: v}
	}

	return MapValue{Entries: entries}, nil
}

func readSetProperty(rd *reader, tree TypeTreeNode, size uint32) (Value, error) {
	if len(tree.Children) == 0 {
		return nil, fmt.Errorf("set with no element type tree")
	}
	elemTree := tree.Children[0]

	keysToRemove, err := rd.readU32()
	if err != nil {
		return nil, fmt.Errorf("set keys to remove count: %w", err)
	}
	for range keysToRemove {
		if _, err := readValueByType(rd, elemTree); err != nil {
			return nil, fmt.Errorf("set skip removed key: %w", err)
		}
	}

	count, err := rd.readU32()
	if err != nil {
		return nil, fmt.Errorf("set count: %w", err)
	}
	if count > maxArrayCount {
		return nil, fmt.Errorf("set count %d exceeds limit", count)
	}

	elements := make([]Value, count)
	for i := range elements {
		v, err := readValueByType(rd, elemTree)
		if err != nil {
			return nil, fmt.Errorf("set element %d: %w", i, err)
		}
		elements[i] = v
	}

	return ArrayValue{Elements: elements}, nil
}

// ParseBytes is a convenience wrapper that parses from a byte slice.
func ParseBytes(data []byte) (*Save, error) {
	return Parse(bytes.NewReader(data))
}

// mapKeyString extracts a string from a map key Value for lookup convenience.
func mapKeyString(v Value) string {
	switch k := v.(type) {
	case NameValue:
		return k.V
	case StrValue:
		return k.V
	case ObjectValue:
		return k.V
	case EnumValue:
		return k.V
	case ByteEnumValue:
		return k.V
	default:
		return ""
	}
}

// FindMapEntry looks up an entry in a MapValue by string key.
func FindMapEntry(entries []MapEntry, key string) *MapEntry {
	for i := range entries {
		if mapKeyString(entries[i].Key) == key {
			return &entries[i]
		}
	}
	return nil
}
