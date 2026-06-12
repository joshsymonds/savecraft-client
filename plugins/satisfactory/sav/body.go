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
// Data blob: int32 object count (must equal the TOC count), then per object:
//
//	int32 objectSaveVersion           <- self-describing, independent of level
//	int32 shouldMigrateObjectRefsToPersistent
//	int32 dataSize, dataSize bytes    <- the object's serialized properties
//	objectSaveVersion>=53: int32 flag, if ==1: FSaveObjectVersionData
//
// Any bytes left in a TOC blob after the last object (a duplicate
// collectables list) are skipped. Per-level TOC versions genuinely diverge
// in real saves (a sv52 megafactory carried 803 levels at v51) and the
// version field sits AFTER its blob — so sublevel TOCs are buffered and
// parsed once the version is known (WalkObjects), or parsed eagerly under
// the header-version interpretation with a fallback across the flags gate
// (Extract, which needs the class mask before the data blob arrives; a
// misaligned TOC parse fails fast on the string-length and object-type
// guards, and the guess is verified against the real version field once it
// streams past). Data blobs are NEVER buffered — megafactories concentrate
// tens of MB of object data in single world-partition cells, so data
// streams with only matching objects' bytes materialized. The persistent
// level always uses the header version and parses fully in-stream.
const (
	saveVersionObjectFlags     = 49 // SerializeObjectFlags
	saveVersionPerLevelTOC     = 51 // SerializePerStreamableLevelTOCVersion
	saveVersionPackageVersions = 53 // SerializeDataPackageVersionAndCustomVersions

	objectTypeComponent = 0
	objectTypeActor     = 1

	// maxSublevelTOC bounds the buffer for one streaming level's TOC blob
	// (headers only — roughly 100 bytes per object, so this allows ~500k
	// objects in a single world-partition cell).
	maxSublevelTOC = 64 << 20

	// maxObjectData bounds one object's serialized data. The largest real
	// objects (conveyor chain actors, lightweight buildable subsystems)
	// run to tens of MB in megafactories.
	maxObjectData = 256 << 20

	// maxObjectsPerLevel and maxSublevels guard corrupt counts before any
	// allocation happens. Largest observed real save: 3088 sublevels,
	// 400k objects total.
	maxObjectsPerLevel = 50_000_000
	maxSublevels       = 1_000_000
)

// ObjectHeader is one TOC entry: an actor or component that exists in the
// world, identified by class and instance path. Object property data lives
// in the data blob and is not decoded here.
type ObjectHeader struct {
	LevelName    string
	ClassPath    string
	RootObject   string
	InstanceName string
	IsActor      bool
	ParentEntity string     // components: instance path of the owning actor
	Translation  [3]float32 // actors: world position (cm)
}

// Object couples a TOC header with the object's raw serialized data bytes
// from the data blob (undecoded UE tagged properties).
type Object struct {
	ObjectHeader
	SaveVersion int32 // per-object serialization version
	Data        []byte
}

// WalkObjects streams every object header in the save body, level by level,
// invoking fn for each. Data blobs are skipped wholesale; memory stays
// bounded by the largest single sublevel TOC. Returning an error from fn
// aborts the walk and returns that error.
func WalkObjects(h *Header, body io.Reader, fn func(ObjectHeader) error) error {
	w := &walker{h: h, headerFn: fn}
	return w.walk(newReader(body))
}

// Extract streams the save body and invokes fn with header plus raw data
// bytes for every object whose class path satisfies want. Non-matching
// objects' data is discarded without buffering, so memory stays bounded by
// the largest sublevel blob plus the largest matching object.
func Extract(h *Header, body io.Reader, want func(classPath string) bool, fn func(Object) error) error {
	w := &walker{h: h, want: want, dataFn: fn}
	return w.walk(newReader(body))
}

// walker carries the two walk modes through the level structure. headerFn
// (if set) fires for every TOC entry. want+dataFn (if set) additionally
// parse data blobs and fire for matching objects.
type walker struct {
	h        *Header
	headerFn func(ObjectHeader) error
	want     func(string) bool
	dataFn   func(Object) error
}

func (w *walker) extracting() bool { return w.want != nil }

func (w *walker) walk(r *reader) error {
	if _, err := r.int64("body size"); err != nil {
		return err
	}
	if w.h.SaveVersion >= saveVersionPackageVersions {
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
		if err := w.walkSublevel(r); err != nil {
			return fmt.Errorf("sublevel %d/%d: %w", i+1, sublevels, err)
		}
	}
	if err := w.walkPersistentLevel(r); err != nil {
		return fmt.Errorf("persistent level: %w", err)
	}
	return nil
}

// walkSublevel handles one streaming level. The TOC is buffered because the
// per-level version that governs its format is serialized after the blobs.
// When extracting, the TOC is parsed eagerly (heuristically across the flags
// gate) so the data blob can stream instead of being buffered.
func (w *walker) walkSublevel(r *reader) error {
	name, err := r.fstring("level name")
	if err != nil {
		return err
	}

	tocBytes, err := readBlob(r, "TOC")
	if err != nil {
		return fmt.Errorf("level %q: %w", name, err)
	}

	var entries *tocMask
	var usedVersion int32
	if w.extracting() {
		if entries, usedVersion, err = w.parseTOCEagerly(tocBytes, name); err != nil {
			return fmt.Errorf("level %q TOC: %w", name, err)
		}
		if err := w.streamDataBlob(r, entries); err != nil {
			return fmt.Errorf("level %q data: %w", name, err)
		}
	} else if err := skipDataBlob(r); err != nil {
		return fmt.Errorf("level %q: %w", name, err)
	}

	levelVersion := w.h.SaveVersion
	if w.h.SaveVersion >= saveVersionPerLevelTOC {
		if levelVersion, err = r.int32("level TOC version"); err != nil {
			return fmt.Errorf("level %q: %w", name, err)
		}
	}

	if w.extracting() {
		// The eager parse guessed which side of the flags gate the TOC was
		// written on; the authoritative version has now streamed past.
		if (levelVersion >= saveVersionObjectFlags) != (usedVersion >= saveVersionObjectFlags) {
			return fmt.Errorf(
				"level %q: TOC parsed as version %d but level declares %d — flags interpretation mismatch",
				name, usedVersion, levelVersion)
		}
	} else {
		// Version known exactly — parse the buffered TOC now.
		if _, err := w.collectTOC(newReader(bytes.NewReader(tocBytes)), name, levelVersion); err != nil {
			return fmt.Errorf("level %q TOC: %w", name, err)
		}
	}

	if err := skipObjectReferences(r); err != nil {
		return fmt.Errorf("level %q collectables: %w", name, err)
	}
	if w.h.SaveVersion >= saveVersionPackageVersions {
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
	return nil
}

// parseTOCEagerly parses a buffered TOC before the level's version is known.
// It tries the header's version first, then the other side of the
// object-flags gate. A wrong interpretation fails fast: flags read as string
// lengths (and vice versa) trip the FString and object-type guards almost
// immediately.
func (w *walker) parseTOCEagerly(tocBytes []byte, name string) (*tocMask, int32, error) {
	primary := w.h.SaveVersion
	var fallback int32 = saveVersionObjectFlags // no-flags header => try with-flags
	if primary >= saveVersionObjectFlags {
		fallback = saveVersionObjectFlags - 1 // with-flags header => try without
	}

	entries, primaryErr := w.collectTOC(newReader(bytes.NewReader(tocBytes)), name, primary)
	if primaryErr == nil {
		return entries, primary, nil
	}
	entries, fallbackErr := w.collectTOC(newReader(bytes.NewReader(tocBytes)), name, fallback)
	if fallbackErr == nil {
		return entries, fallback, nil
	}
	return nil, 0, fmt.Errorf(
		"unparseable under either flags interpretation: as v%d: %w; as v%d: %w",
		primary, primaryErr, fallback, fallbackErr)
}

// streamDataBlob reads a size-prefixed data blob in-stream, emitting wanted
// objects and discarding the rest.
func (w *walker) streamDataBlob(r *reader, mask *tocMask) error {
	size, err := r.int64("data blob size")
	if err != nil {
		return err
	}
	if size < 0 {
		return fmt.Errorf("implausible data blob size %d", size)
	}
	limited := io.LimitReader(r.r, size)
	if err := w.emitData(newReader(limited), mask); err != nil {
		return err
	}
	if _, err := io.Copy(io.Discard, limited); err != nil {
		return fmt.Errorf("drain data blob: %w", err)
	}
	r.off += size
	return nil
}

// walkPersistentLevel handles the final, unnamed level. It has no per-level
// version (the header version governs), so both blobs parse in-stream.
func (w *walker) walkPersistentLevel(r *reader) error {
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
	headers, err := w.collectTOC(newReader(limited), w.h.MapName, w.h.SaveVersion)
	if err != nil {
		return fmt.Errorf("TOC: %w", err)
	}
	if _, err := io.Copy(io.Discard, limited); err != nil {
		return fmt.Errorf("drain TOC blob: %w", err)
	}
	r.off += tocSize

	if w.extracting() {
		if err := w.streamDataBlob(r, headers); err != nil {
			return fmt.Errorf("data: %w", err)
		}
	} else if err := skipDataBlob(r); err != nil {
		return err
	}
	// LevelToDestroyedActorsMap and trailing unresolved data follow; nothing
	// in them is needed, and the stream ends here.
	return nil
}

// tocMask records which data-blob positions are wanted. Headers are kept
// only for wanted objects, so a megafactory persistent level costs one bool
// per object (~400KB) rather than a full header struct each (~48MB).
type tocMask struct {
	wanted  []bool
	headers map[int]ObjectHeader
}

// collectTOC parses object headers from one level's TOC blob, firing
// headerFn for each, and returns the mask needed to align the data blob.
// The mask is nil when not extracting.
func (w *walker) collectTOC(r *reader, levelName string, levelVersion int32) (*tocMask, error) {
	count, err := r.int32("object count")
	if err != nil {
		return nil, err
	}
	if count < 0 || count > maxObjectsPerLevel {
		return nil, fmt.Errorf("implausible object count %d", count)
	}

	var mask *tocMask
	if w.extracting() {
		mask = &tocMask{wanted: make([]bool, count), headers: map[int]ObjectHeader{}}
	}
	for i := range count {
		obj, err := readObjectHeader(r, levelName, levelVersion)
		if err != nil {
			return nil, fmt.Errorf("object %d/%d: %w", i+1, count, err)
		}
		if w.headerFn != nil {
			if err := w.headerFn(obj); err != nil {
				return nil, err
			}
		}
		if mask != nil && w.want(obj.ClassPath) {
			mask.wanted[i] = true
			mask.headers[int(i)] = obj
		}
	}
	return mask, nil
}

// emitData parses a data blob aligned against the level's TOC mask,
// firing dataFn for wanted objects and discarding the rest.
func (w *walker) emitData(r *reader, mask *tocMask) error {
	count, err := r.int32("data object count")
	if err != nil {
		return err
	}
	if int(count) != len(mask.wanted) {
		return fmt.Errorf("data blob has %d objects, TOC has %d", count, len(mask.wanted))
	}

	for i := range mask.wanted {
		version, err := r.int32("object save version")
		if err != nil {
			return fmt.Errorf("object %d/%d: %w", i+1, count, err)
		}
		if _, err := r.int32("migrate flag"); err != nil {
			return fmt.Errorf("object %d/%d: %w", i+1, count, err)
		}
		size, err := r.int32("object data size")
		if err != nil {
			return fmt.Errorf("object %d/%d: %w", i+1, count, err)
		}
		if size < 0 || int64(size) > maxObjectData {
			return fmt.Errorf("object %d/%d: implausible data size %d", i+1, count, size)
		}

		if mask.wanted[i] {
			data, err := r.bytes(int(size), "object data")
			if err != nil {
				return fmt.Errorf("object %d/%d: %w", i+1, count, err)
			}
			obj := Object{ObjectHeader: mask.headers[i], SaveVersion: version, Data: data}
			if err := w.dataFn(obj); err != nil {
				return err
			}
		} else if err := r.discard(int64(size), "object data"); err != nil {
			return fmt.Errorf("object %d/%d: %w", i+1, count, err)
		}

		if version >= saveVersionPackageVersions {
			hasVersionData, vdErr := r.int32("object version data flag")
			if vdErr != nil {
				return fmt.Errorf("object %d/%d: %w", i+1, count, vdErr)
			}
			if hasVersionData == 1 {
				if vdErr := skipVersionData(r); vdErr != nil {
					return fmt.Errorf("object %d/%d version data: %w", i+1, count, vdErr)
				}
			}
		}
	}
	return nil
}

// readBlob reads a size-prefixed blob into memory, bounded by
// maxSublevelTOC.
func readBlob(r *reader, what string) ([]byte, error) {
	size, err := r.int64(what + " blob size")
	if err != nil {
		return nil, err
	}
	if size < 0 || size > maxSublevelTOC {
		return nil, fmt.Errorf("implausible %s blob size %d (sublevel cap %d)", what, size, maxSublevelTOC)
	}
	return r.bytes(int(size), what+" blob")
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
