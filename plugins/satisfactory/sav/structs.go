package sav

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
)

func leUint64(b []byte) uint64 { return binary.LittleEndian.Uint64(b) }

// Struct value layouts, from etothepii4 StructProperty.ts / FInventoryItem.ts
// / FGDynamicStruct.ts. Binary structs have fixed wire layouts; everything
// not listed parses as a generic nested property list (the engine's
// DynamicStructPropertyValue fallback), which covers FG game structs like
// InventoryStack, ItemAmount, ResearchData, etc.
const (
	// SwitchTo64BitSaveArchive: vectors/quats switch from float32 to
	// float64 components. Predates 1.0, so every supported save uses
	// doubles, but the gate is kept for correctness.
	saveVersion64BitVectors = 41

	// RefactoredInventoryItemState: FInventoryItem's state becomes a
	// size-prefixed FGDynamicStruct instead of a legacy actor reference.
	saveVersionInventoryItemState = 43

	// maxItemStatePayload guards the skip of an item state blob.
	maxItemStatePayload = 16 << 20
)

// InventoryItem is an FInventoryItem: an item class with optional opaque
// state (the state payload is skipped, not decoded).
type InventoryItem struct {
	ItemClass string
	HasState  bool
}

// undecodableStructs have binary wire layouts this parser does not decode.
// They cannot fall through to the generic property-list path (they are not
// property lists), so they must be skipped by the declared property size.
var undecodableStructs = map[string]bool{
	"ClientIdentityInfo":          true,
	"UniqueNetIdRepl":             true,
	"PlayerInfoHandle":            true,
	"FICFrameRange":               true,
	"LBBalancerIndexing":          true,
	"FINNetworkTrace":             true,
	"FIRTrace":                    true,
	"FINGPUT1BufferPixel":         true,
	"FINLuaProcessorStateStorage": true,
	"FINLuaRuntimePersistenceState": true,
}

func isUndecodableStruct(name string) bool { return undecodableStructs[name] }

// parseStructValue decodes one struct value by its type name. Unknown names
// parse as a generic nested property list.
func parseStructValue(r *reader, structName string, ctx parseCtx) (any, error) {
	wide := ctx.saveVersion >= saveVersion64BitVectors
	switch structName {
	case "Vector", "Rotator":
		return readVecN3(r, wide)
	case "Quat", "Vector4", "Vector4D":
		v, err := readFloats(r, 4, wide)
		if err != nil {
			return nil, err
		}
		return [4]float64{v[0], v[1], v[2], v[3]}, nil
	case "Vector2D":
		v, err := readFloats(r, 2, wide)
		if err != nil {
			return nil, err
		}
		return [2]float64{v[0], v[1]}, nil
	case "LinearColor":
		v, err := readFloats(r, 4, false)
		if err != nil {
			return nil, err
		}
		return [4]float64{v[0], v[1], v[2], v[3]}, nil
	case "Color":
		buf, err := r.bytes(4, "color")
		if err != nil {
			return nil, err
		}
		return [4]int64{int64(buf[0]), int64(buf[1]), int64(buf[2]), int64(buf[3])}, nil
	case "Guid":
		buf, err := r.bytes(16, "guid")
		if err != nil {
			return nil, err
		}
		return hex.EncodeToString(buf), nil
	case "IntPoint", "DateTime":
		return r.int64(structName)
	case "TimerHandle", "SlateBrush":
		return r.fstring(structName)
	case "FluidBox":
		buf, err := r.bytes(4, "fluid box")
		if err != nil {
			return nil, err
		}
		return float64(f32(buf)), nil
	case "Box":
		boxMin, err := readVecN3(r, wide)
		if err != nil {
			return nil, err
		}
		boxMax, err := readVecN3(r, wide)
		if err != nil {
			return nil, err
		}
		valid, err := r.byte("box valid")
		if err != nil {
			return nil, err
		}
		return map[string]any{"min": boxMin, "max": boxMax, "isValid": valid >= 1}, nil
	case "RailroadTrackPosition":
		root, err := r.fstring("track root")
		if err != nil {
			return nil, err
		}
		instance, err := r.fstring("track instance")
		if err != nil {
			return nil, err
		}
		buf, err := r.bytes(8, "track offset+forward")
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"root": root, "instanceName": instance,
			"offset": float64(f32(buf[0:])), "forward": float64(f32(buf[4:])),
		}, nil
	case "InventoryItem":
		return parseInventoryItem(r, ctx)
	default:
		return parseGenericStruct(r, ctx)
	}
}

func readVecN3(r *reader, wide bool) ([3]float64, error) {
	v, err := readFloats(r, 3, wide)
	if err != nil {
		return [3]float64{}, err
	}
	return [3]float64{v[0], v[1], v[2]}, nil
}

// readFloats reads n components as float64 (wide) or float32.
func readFloats(r *reader, n int, wide bool) ([]float64, error) {
	width := 4
	if wide {
		width = 8
	}
	buf, err := r.bytes(n*width, "vector components")
	if err != nil {
		return nil, err
	}
	out := make([]float64, n)
	for i := range n {
		if wide {
			out[i] = math64(int64(leUint64(buf[i*8:])))
		} else {
			out[i] = float64(f32(buf[i*4:]))
		}
	}
	return out, nil
}

// parseInventoryItem reads an FInventoryItem, skipping any item state
// payload (size-prefixed at sv >= 43).
func parseInventoryItem(r *reader, ctx parseCtx) (InventoryItem, error) {
	ref, err := readObjectRef(r)
	if err != nil {
		return InventoryItem{}, fmt.Errorf("item reference: %w", err)
	}
	item := InventoryItem{ItemClass: ref.Path}

	if ctx.saveVersion >= saveVersionInventoryItemState {
		hasState, err := r.int32("item state flag")
		if err != nil {
			return item, err
		}
		if hasState >= 1 {
			if _, err := readObjectRef(r); err != nil {
				return item, fmt.Errorf("item state struct ref: %w", err)
			}
			payload, err := r.int32("item state payload size")
			if err != nil {
				return item, err
			}
			if payload < 0 || payload > maxItemStatePayload {
				return item, fmt.Errorf("implausible item state payload %d", payload)
			}
			if err := r.discard(int64(payload), "item state payload"); err != nil {
				return item, err
			}
			item.HasState = true
		}
		return item, nil
	}

	// Legacy: an actor reference instead of a dynamic struct.
	if _, err := readObjectRef(r); err != nil {
		return item, fmt.Errorf("legacy item state actor: %w", err)
	}
	return item, nil
}

// parseGenericStruct reads a nested tagged property list (the engine's
// dynamic struct fallback). Nested undecodable properties are dropped —
// the top-level property's Skipped record is the discovery surface.
func parseGenericStruct(r *reader, ctx parseCtx) (map[string]any, error) {
	props := map[string]any{}
	for {
		name, value, skippedType, done, err := parseProperty(r, ctx)
		if err != nil {
			return nil, err
		}
		if done {
			return props, nil
		}
		if skippedType != "" {
			continue
		}
		if existing, dup := props[name]; dup {
			if slice, ok := existing.([]any); ok {
				props[name] = append(slice, value)
			} else {
				props[name] = []any{existing, value}
			}
		} else {
			props[name] = value
		}
	}
}
