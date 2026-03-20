// Package gvas parses Unreal Engine 5.4 GVAS binary save files into
// a structured Go representation.
package gvas

import (
	"fmt"
	"strings"
)

// Save represents a parsed GVAS save file.
type Save struct {
	Header       Header
	SaveGameType string
	Properties   Properties
	Extra        []byte
}

// Header holds GVAS file header fields.
type Header struct {
	Magic              uint32
	SaveGameVersion    uint32
	PackageVersionUE4  uint32
	PackageVersionUE5  uint32
	EngineVersionMajor uint16
	EngineVersionMinor uint16
	EngineVersionPatch uint16
	EngineVersionBuild uint32
	EngineVersionStr   string
	CustomVersions     []CustomVersion
}

// CustomVersion is a GUID+version pair from the header.
type CustomVersion struct {
	GUID  Guid
	Value int32
}

// Guid is a 128-bit identifier stored as four little-endian uint32s.
type Guid struct {
	A uint32
	B [4]byte
	C [4]byte
	D uint32
}

// String formats a Guid in the uesave style:
// {a:08x}-{b[3]:02x}{b[2]:02x}-{b[1]:02x}{b[0]:02x}-{c[3]:02x}{c[2]:02x}-{c[1]:02x}{c[0]:02x}{d:08x}
func (g Guid) String() string {
	return fmt.Sprintf("%08x-%02x%02x-%02x%02x-%02x%02x-%02x%02x%08x",
		g.A,
		g.B[3], g.B[2],
		g.B[1], g.B[0],
		g.C[3], g.C[2],
		g.C[1], g.C[0],
		g.D,
	)
}

// TypeTreeNode represents the recursive type tree attached to each
// property tag in UE 5.4 format.
type TypeTreeNode struct {
	Name     string
	Children []TypeTreeNode
}

// Property is a named value from a GVAS property list.
type Property struct {
	Name       string
	TypeTree   TypeTreeNode
	ArrayIndex uint32
	PropGUID   *Guid
	Value      Value
}

// Properties is an ordered slice of Property with lookup helpers.
type Properties []Property

// Get returns the first property whose name matches (case-sensitive).
// It returns nil if not found.
func (ps Properties) Get(name string) *Property {
	for i := range ps {
		if ps[i].Name == name {
			return &ps[i]
		}
	}
	return nil
}

// GetPrefix returns the first property whose name starts with prefix.
// Useful for looking up suffixed names inside structs
// (e.g., prefix "CurrentLevel" matches "CurrentLevel_49_...").
func (ps Properties) GetPrefix(prefix string) *Property {
	for i := range ps {
		if strings.HasPrefix(ps[i].Name, prefix) {
			return &ps[i]
		}
	}
	return nil
}

// GetInt returns the int32 value of the named property, or 0 if not found or wrong type.
func (ps Properties) GetInt(name string) int32 {
	p := ps.Get(name)
	if p == nil {
		return 0
	}
	if v, ok := p.Value.(IntValue); ok {
		return v.V
	}
	return 0
}

// GetIntPrefix returns the int32 value of a property whose name starts with prefix.
func (ps Properties) GetIntPrefix(prefix string) int32 {
	p := ps.GetPrefix(prefix)
	if p == nil {
		return 0
	}
	if v, ok := p.Value.(IntValue); ok {
		return v.V
	}
	return 0
}

// GetString returns the string value of the named property (StrProperty or NameProperty).
func (ps Properties) GetString(name string) string {
	p := ps.Get(name)
	if p == nil {
		return ""
	}
	switch v := p.Value.(type) {
	case StrValue:
		return v.V
	case NameValue:
		return v.V
	}
	return ""
}

// GetStringPrefix returns the string value of a property whose name starts with prefix.
func (ps Properties) GetStringPrefix(prefix string) string {
	p := ps.GetPrefix(prefix)
	if p == nil {
		return ""
	}
	switch v := p.Value.(type) {
	case StrValue:
		return v.V
	case NameValue:
		return v.V
	}
	return ""
}

// GetFloat64 returns the float64 value of the named DoubleProperty.
func (ps Properties) GetFloat64(name string) float64 {
	p := ps.Get(name)
	if p == nil {
		return 0
	}
	if v, ok := p.Value.(Float64Value); ok {
		return v.V
	}
	return 0
}

// GetFloat64Prefix returns the float64 value of a property whose name starts with prefix.
func (ps Properties) GetFloat64Prefix(prefix string) float64 {
	p := ps.GetPrefix(prefix)
	if p == nil {
		return 0
	}
	if v, ok := p.Value.(Float64Value); ok {
		return v.V
	}
	return 0
}

// GetBool returns the bool value of the named BoolProperty.
func (ps Properties) GetBool(name string) bool {
	p := ps.Get(name)
	if p == nil {
		return false
	}
	if v, ok := p.Value.(BoolValue); ok {
		return v.V
	}
	return false
}

// GetBoolPrefix returns the bool value of a property whose name starts with prefix.
func (ps Properties) GetBoolPrefix(prefix string) bool {
	p := ps.GetPrefix(prefix)
	if p == nil {
		return false
	}
	if v, ok := p.Value.(BoolValue); ok {
		return v.V
	}
	return false
}

// GetStruct returns the Properties inside a named StructProperty.
func (ps Properties) GetStruct(name string) Properties {
	p := ps.Get(name)
	if p == nil {
		return nil
	}
	if v, ok := p.Value.(StructValue); ok {
		return v.Properties
	}
	return nil
}

// GetStructPrefix returns the Properties inside a property whose name starts with prefix.
func (ps Properties) GetStructPrefix(prefix string) Properties {
	p := ps.GetPrefix(prefix)
	if p == nil {
		return nil
	}
	if v, ok := p.Value.(StructValue); ok {
		return v.Properties
	}
	return nil
}

// GetArray returns the []Value inside a named ArrayProperty.
func (ps Properties) GetArray(name string) []Value {
	p := ps.Get(name)
	if p == nil {
		return nil
	}
	if v, ok := p.Value.(ArrayValue); ok {
		return v.Elements
	}
	return nil
}

// GetArrayPrefix returns the []Value inside a property whose name starts with prefix.
func (ps Properties) GetArrayPrefix(prefix string) []Value {
	p := ps.GetPrefix(prefix)
	if p == nil {
		return nil
	}
	if v, ok := p.Value.(ArrayValue); ok {
		return v.Elements
	}
	return nil
}

// GetMap returns the []MapEntry inside a named MapProperty.
func (ps Properties) GetMap(name string) []MapEntry {
	p := ps.Get(name)
	if p == nil {
		return nil
	}
	if v, ok := p.Value.(MapValue); ok {
		return v.Entries
	}
	return nil
}

// GetMapPrefix returns the []MapEntry inside a property whose name starts with prefix.
func (ps Properties) GetMapPrefix(prefix string) []MapEntry {
	p := ps.GetPrefix(prefix)
	if p == nil {
		return nil
	}
	if v, ok := p.Value.(MapValue); ok {
		return v.Entries
	}
	return nil
}

// GetByteEnum returns the string value of a named ByteProperty with enum inner type.
func (ps Properties) GetByteEnum(name string) string {
	p := ps.Get(name)
	if p == nil {
		return ""
	}
	if v, ok := p.Value.(ByteEnumValue); ok {
		return v.V
	}
	return ""
}

// GetByteEnumPrefix returns the ByteEnum string value of a property whose name starts with prefix.
func (ps Properties) GetByteEnumPrefix(prefix string) string {
	p := ps.GetPrefix(prefix)
	if p == nil {
		return ""
	}
	if v, ok := p.Value.(ByteEnumValue); ok {
		return v.V
	}
	return ""
}

// Value is the interface satisfied by all property value types.
type Value interface {
	isValue()
}

// IntValue holds an IntProperty (i32).
type IntValue struct{ V int32 }

// Int64Value holds an Int64Property (i64).
type Int64Value struct{ V int64 }

// UInt32Value holds a UInt32Property (u32).
type UInt32Value struct{ V uint32 }

// UInt64Value holds a UInt64Property (u64).
type UInt64Value struct{ V uint64 }

// FloatValue holds a FloatProperty (f32).
type FloatValue struct{ V float32 }

// Float64Value holds a DoubleProperty (f64).
type Float64Value struct{ V float64 }

// BoolValue holds a BoolProperty.
type BoolValue struct{ V bool }

// StrValue holds a StrProperty.
type StrValue struct{ V string }

// NameValue holds a NameProperty.
type NameValue struct{ V string }

// ObjectValue holds an ObjectProperty.
type ObjectValue struct{ V string }

// EnumValue holds an EnumProperty.
type EnumValue struct{ V string }

// ByteEnumValue holds a ByteProperty with an enum inner type (reads as FString).
type ByteEnumValue struct{ V string }

// ByteValue holds a raw ByteProperty (u8).
type ByteValue struct{ V uint8 }

// SoftObjectValue holds a SoftObjectProperty (3 FStrings in UE5 >= 1007).
type SoftObjectValue struct {
	AssetPathName string
	PackageName   string
	AssetName     string
}

// StructValue holds a generic StructProperty (nested properties).
type StructValue struct {
	StructType string
	Properties Properties
}

// GuidValue holds a Guid struct.
type GuidValue struct{ V Guid }

// DateTimeValue holds a DateTime struct (u64 ticks).
type DateTimeValue struct{ V uint64 }

// VectorValue holds a Vector struct (3x f64 in UE5).
type VectorValue struct{ X, Y, Z float64 }

// QuatValue holds a Quat struct (4x f64).
type QuatValue struct{ X, Y, Z, W float64 }

// RotatorValue holds a Rotator struct (3x f64).
type RotatorValue struct{ Pitch, Yaw, Roll float64 }

// LinearColorValue holds a LinearColor struct (4x f32).
type LinearColorValue struct{ R, G, B, A float32 }

// GameplayTagContainerValue holds a GameplayTagContainer.
type GameplayTagContainerValue struct{ Tags []string }

// Vector2DValue holds a Vector2D struct (2x f64).
type Vector2DValue struct{ X, Y float64 }

// IntPointValue holds an IntPoint struct (2x i32).
type IntPointValue struct{ X, Y int32 }

// RawStructValue holds struct data that was read as raw bytes
// (HasBinaryOrNativeSerialize flag set).
type RawStructValue struct {
	StructType string
	Data       []byte
}

// ArrayValue holds an ArrayProperty.
type ArrayValue struct {
	Elements []Value
}

// MapEntry is a single key-value pair in a MapProperty.
type MapEntry struct {
	Key   Value
	Value Value
}

// MapValue holds a MapProperty.
type MapValue struct {
	Entries []MapEntry
}

// RawValue holds property data that could not be parsed.
type RawValue struct {
	TypeName string
	Data     []byte
}

// TextValue holds a TextProperty.
type TextValue struct {
	Data []byte
}

// Marker method implementations for the Value interface.
func (IntValue) isValue()                  {}
func (Int64Value) isValue()                {}
func (UInt32Value) isValue()               {}
func (UInt64Value) isValue()               {}
func (FloatValue) isValue()                {}
func (Float64Value) isValue()              {}
func (BoolValue) isValue()                 {}
func (StrValue) isValue()                  {}
func (NameValue) isValue()                 {}
func (ObjectValue) isValue()               {}
func (EnumValue) isValue()                 {}
func (ByteEnumValue) isValue()             {}
func (ByteValue) isValue()                 {}
func (SoftObjectValue) isValue()           {}
func (StructValue) isValue()               {}
func (GuidValue) isValue()                 {}
func (DateTimeValue) isValue()             {}
func (VectorValue) isValue()               {}
func (QuatValue) isValue()                 {}
func (RotatorValue) isValue()              {}
func (LinearColorValue) isValue()          {}
func (GameplayTagContainerValue) isValue() {}
func (Vector2DValue) isValue()             {}
func (IntPointValue) isValue()             {}
func (RawStructValue) isValue()            {}
func (ArrayValue) isValue()                {}
func (MapValue) isValue()                  {}
func (RawValue) isValue()                  {}
func (TextValue) isValue()                 {}
