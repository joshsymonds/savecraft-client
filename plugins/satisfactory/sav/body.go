package sav

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
)

// Decompressed body layout for SaveVersion >= 46, verified empirically
// against 1.0 (sv46), 1.1 (sv52, 400k objects), and 1.2 (sv58) saves.
// Spec: satisfactory-3d-map SATISFACTORY_SAVE.md; field widths and gate
// versions cross-checked against etothepii4/satisfactory-file-parser.
//
//	int64  body size (excluding this field)
//	sv>=53: FSaveObjectVersionData (package/engine/custom versions)
//	validation grids: int32 count {FString name, int32 cellSize,
//	    uint32 gridHash, uint32 cellCount {FString cell, uint32 hash}}
//	int32  sublevel count
//	per sublevel:
//	    FString name
//	    int64  TOC blob size, TOC bytes
//	    int64  data blob size, data bytes
//	    sv>=51: int32 level TOC version   <- arrives AFTER the TOC it governs
//	    collectables: int32 count {FString, FString}
//	    sv>=53: int32 flag, if >=1: FSaveObjectVersionData
//	persistent level (no name; uses the header's SaveVersion):
//	    int64 TOC blob size, TOC bytes
//	    int64 data blob size, data bytes
//	    destroyed actors: int32 count {FString level, int32 n {FString, FString}}
//	trailing unresolved-reference data (ignored)
//
// TOC blob: int32 object count, then per object:
//
//	int32 type (1 = actor, 0 = component)
//	FString classPath, FString rootObject, FString instanceName
//	levelVersion>=49: uint32 objectFlags
//	actor:     int32 needTransform, float32[4] rotation,
//	           float32[3] translation, float32[3] scale, int32 wasPlacedInLevel
//	component: FString parentEntityName
//
// Any bytes left in the blob after the last object (a duplicate collectables
// list) are skipped. Per-level TOC versions genuinely diverge in real saves
// (a sv52 megafactory carried 803 levels at v51) and the version field sits
// AFTER its blob — so sublevel TOCs are buffered and parsed once the version
// is known. The persistent level always uses the header version and is
// parsed in-stream, since megafactory persistent TOCs exceed any reasonable
// buffer budget.
const (
	saveVersionObjectFlags     = 49 // SerializeObjectFlags
	saveVersionPerLevelTOC     = 51 // SerializePerStreamableLevelTOCVersion
	saveVersionPackageVersions = 53 // SerializeDataPackageVersionAndCustomVersions

	objectTypeComponent = 0
	objectTypeActor     = 1

	// maxSublevelTOC bounds the buffer for one streaming level's TOC.
	// Real sublevels are world-partition cells a few KB to a few hundred KB
	// in size; the giant TOCs live in the persistent level, which streams.
	maxSublevelTOC = 64 << 20

	// maxObjectsPerLevel and maxSublevels guard corrupt counts before any
	// allocation happens. Largest observed real save: 3088 sublevels,
	// 400k objects total.
	maxObjectsPerLevel = 50_000_000
	maxSublevels       = 1_000_000
)

// ObjectHeader is one TOC entry: an actor or component that exists in the
// world, identified by class and instance path. Object property data lives
// in the data blob and is not decoded by WalkObjects.
type ObjectHeader struct {
	LevelName    string
	ClassPath    string
	RootObject   string
	InstanceName string
	IsActor      bool
	ParentEntity string     // components: instance path of the owning actor
	Translation  [3]float32 // actors: world position (cm)
}

// WalkObjects streams every object header in the save body, level by level,
// invoking fn for each. Data blobs are skipped wholesale; memory stays
// bounded by the largest single sublevel TOC. Returning an error from fn
// aborts the walk and returns that error.
func WalkObjects(h *Header, body io.Reader, fn func(ObjectHeader) error) error {
	r := newReader(body)

	if _, err := r.int64("body size"); err != nil {
		return err
	}
	if h.SaveVersion >= saveVersionPackageVersions {
		if err := skipVersionData(r); err != nil {
			return fmt.Errorf("version data: %w", err)
		}
	}
	if err := skipValidationGrids(r); err != nil {
		return fmt.Errorf("validation grids: %w", err)
	}

	sublevels, err := r.int32("sublevel count")
	if err != nil {
		return err
	}
	if sublevels < 0 || sublevels > maxSublevels {
		return fmt.Errorf("implausible sublevel count %d", sublevels)
	}

	for i := range sublevels {
		if err := walkSublevel(r, h, fn); err != nil {
			return fmt.Errorf("sublevel %d/%d: %w", i+1, sublevels, err)
		}
	}
	if err := walkPersistentLevel(r, h, fn); err != nil {
		return fmt.Errorf("persistent level: %w", err)
	}
	return nil
}

// walkSublevel handles one streaming level. The TOC blob is buffered because
// its format depends on the per-level version serialized after it.
func walkSublevel(r *reader, h *Header, fn func(ObjectHeader) error) error {
	name, err := r.fstring("level name")
	if err != nil {
		return err
	}

	tocSize, err := r.int64("TOC blob size")
	if err != nil {
		return fmt.Errorf("level %q: %w", name, err)
	}
	if tocSize < 0 || tocSize > maxSublevelTOC {
		return fmt.Errorf("level %q: implausible TOC size %d", name, tocSize)
	}
	tocBytes, err := r.bytes(int(tocSize), "TOC blob")
	if err != nil {
		return fmt.Errorf("level %q: %w", name, err)
	}

	if err := skipDataBlob(r); err != nil {
		return fmt.Errorf("level %q: %w", name, err)
	}

	levelVersion := h.SaveVersion
	if h.SaveVersion >= saveVersionPerLevelTOC {
		if levelVersion, err = r.int32("level TOC version"); err != nil {
			return fmt.Errorf("level %q: %w", name, err)
		}
	}
	if err := skipObjectReferences(r); err != nil {
		return fmt.Errorf("level %q collectables: %w", name, err)
	}
	if h.SaveVersion >= saveVersionPackageVersions {
		hasVersionData, vdErr := r.int32("level version data flag")
		if vdErr != nil {
			return fmt.Errorf("level %q: %w", name, vdErr)
		}
		if hasVersionData >= 1 {
			if vdErr := skipVersionData(r); vdErr != nil {
				return fmt.Errorf("level %q version data: %w", name, vdErr)
			}
		}
	}

	// Version known — now parse the buffered TOC.
	if err := walkTOC(newReader(bytes.NewReader(tocBytes)), name, levelVersion, fn); err != nil {
		return fmt.Errorf("level %q TOC: %w", name, err)
	}
	return nil
}

// walkPersistentLevel handles the final, unnamed level. It has no per-level
// version (the header version governs), so its TOC parses in-stream.
func walkPersistentLevel(r *reader, h *Header, fn func(ObjectHeader) error) error {
	tocSize, err := r.int64("TOC blob size")
	if err != nil {
		return err
	}
	if tocSize < 0 {
		return fmt.Errorf("implausible TOC size %d", tocSize)
	}

	// Bound all TOC reads to the blob, then drain whatever the object
	// headers didn't consume (a duplicate destroyed-actors list).
	limited := io.LimitReader(r.r, tocSize)
	if err := walkTOC(newReader(limited), h.MapName, h.SaveVersion, fn); err != nil {
		return fmt.Errorf("TOC: %w", err)
	}
	if _, err := io.Copy(io.Discard, limited); err != nil {
		return fmt.Errorf("drain TOC blob: %w", err)
	}
	r.off += tocSize

	if err := skipDataBlob(r); err != nil {
		return err
	}
	// LevelToDestroyedActorsMap and trailing unresolved data follow; nothing
	// in them is needed, and the stream ends here.
	return nil
}

// walkTOC parses object headers from one level's TOC blob.
func walkTOC(r *reader, levelName string, levelVersion int32, fn func(ObjectHeader) error) error {
	count, err := r.int32("object count")
	if err != nil {
		return err
	}
	if count < 0 || count > maxObjectsPerLevel {
		return fmt.Errorf("implausible object count %d", count)
	}

	for i := range count {
		obj, err := readObjectHeader(r, levelName, levelVersion)
		if err != nil {
			return fmt.Errorf("object %d/%d: %w", i+1, count, err)
		}
		if err := fn(obj); err != nil {
			return err
		}
	}
	return nil
}

func readObjectHeader(r *reader, levelName string, levelVersion int32) (ObjectHeader, error) {
	obj := ObjectHeader{LevelName: levelName}

	objType, err := r.int32("object type")
	if err != nil {
		return obj, err
	}
	if objType != objectTypeActor && objType != objectTypeComponent {
		return obj, fmt.Errorf("unknown object type %d", objType)
	}
	obj.IsActor = objType == objectTypeActor

	if obj.ClassPath, err = r.fstring("class path"); err != nil {
		return obj, err
	}
	if obj.RootObject, err = r.fstring("root object"); err != nil {
		return obj, err
	}
	if obj.InstanceName, err = r.fstring("instance name"); err != nil {
		return obj, err
	}
	if levelVersion >= saveVersionObjectFlags {
		if _, err = r.int32("object flags"); err != nil {
			return obj, err
		}
	}

	if obj.IsActor {
		// needTransform int32, rotation float32[4], translation float32[3],
		// scale float32[3], wasPlacedInLevel int32.
		if _, err := r.int32("needTransform"); err != nil {
			return obj, err
		}
		if err := r.discard(16, "rotation"); err != nil {
			return obj, err
		}
		buf, err := r.bytes(12, "translation")
		if err != nil {
			return obj, err
		}
		obj.Translation = [3]float32{f32(buf[0:]), f32(buf[4:]), f32(buf[8:])}
		if err := r.discard(12, "scale"); err != nil {
			return obj, err
		}
		if _, err := r.int32("wasPlacedInLevel"); err != nil {
			return obj, err
		}
		return obj, nil
	}

	if obj.ParentEntity, err = r.fstring("parent entity"); err != nil {
		return obj, err
	}
	return obj, nil
}

// skipDataBlob discards a size-prefixed data blob without inflating any of
// it into memory.
func skipDataBlob(r *reader) error {
	size, err := r.int64("data blob size")
	if err != nil {
		return err
	}
	if size < 0 {
		return fmt.Errorf("implausible data blob size %d", size)
	}
	return r.discard(size, "data blob")
}

// skipObjectReferences discards an int32-counted list of
// FObjectReferenceDisc (level name + path name string pairs).
func skipObjectReferences(r *reader) error {
	count, err := r.int32("reference count")
	if err != nil {
		return err
	}
	if count < 0 || count > maxObjectsPerLevel {
		return fmt.Errorf("implausible reference count %d", count)
	}
	for range count {
		if _, err := r.fstring("reference level"); err != nil {
			return err
		}
		if _, err := r.fstring("reference path"); err != nil {
			return err
		}
	}
	return nil
}

// skipValidationGrids discards the world-partition validation data: a map
// of grid name -> (cell size, grid hash, map of cell name -> cell hash).
func skipValidationGrids(r *reader) error {
	grids, err := r.int32("grid count")
	if err != nil {
		return err
	}
	if grids < 0 || grids > 1000 {
		return fmt.Errorf("implausible grid count %d", grids)
	}
	for range grids {
		if _, err := r.fstring("grid name"); err != nil {
			return err
		}
		if err := r.discard(8, "grid cell size + hash"); err != nil {
			return err
		}
		cells, err := r.int32("grid cell count")
		if err != nil {
			return err
		}
		if cells < 0 || cells > maxSublevels {
			return fmt.Errorf("implausible cell count %d", cells)
		}
		for range cells {
			if _, err := r.fstring("cell name"); err != nil {
				return err
			}
			if err := r.discard(4, "cell hash"); err != nil {
				return err
			}
		}
	}
	return nil
}

func f32(b []byte) float32 {
	return math.Float32frombits(binary.LittleEndian.Uint32(b))
}

// skipVersionData discards an FSaveObjectVersionData block: version numbers,
// engine version, and the custom version GUID container.
func skipVersionData(r *reader) error {
	// data version u32, FileVersionUE4 i32, FileVersionUE5 i32, licensee i32.
	if err := r.discard(16, "package versions"); err != nil {
		return err
	}
	// FEngineVersion: u16 major/minor/patch, u32 changelist, FString branch.
	if err := r.discard(10, "engine version"); err != nil {
		return err
	}
	if _, err := r.fstring("engine branch"); err != nil {
		return err
	}
	count, err := r.int32("custom version count")
	if err != nil {
		return err
	}
	if count < 0 || count > 100_000 {
		return fmt.Errorf("implausible custom version count %d", count)
	}
	// Each: 16-byte GUID + int32 version.
	return r.discard(int64(count)*20, "custom versions")
}
