package sav

import (
	"bytes"
	"encoding/binary"
	"errors"
	"strings"
	"testing"
)

// Synthetic body construction mirroring the verified layout in body.go.

type synthObject struct {
	isActor     bool
	classPath   string
	rootObject  string
	instance    string
	parent      string     // components only
	translation [3]float32 // actors only
}

type synthLevel struct {
	name    string // "" = persistent level
	version int32  // TOC version this level's objects are written with
	objects []synthObject
}

func writeI32(buf *bytes.Buffer, v int32)   { binary.Write(buf, binary.LittleEndian, v) }
func writeI64(buf *bytes.Buffer, v int64)   { binary.Write(buf, binary.LittleEndian, v) }
func writeU32(buf *bytes.Buffer, v uint32)  { binary.Write(buf, binary.LittleEndian, v) }
func writeF32(buf *bytes.Buffer, v float32) { binary.Write(buf, binary.LittleEndian, v) }

func writeTOC(t *testing.T, lvl synthLevel) []byte {
	t.Helper()
	toc := &bytes.Buffer{}
	writeI32(toc, int32(len(lvl.objects)))
	for _, o := range lvl.objects {
		if o.isActor {
			writeI32(toc, 1)
		} else {
			writeI32(toc, 0)
		}
		writeFString(toc, o.classPath, false)
		writeFString(toc, o.rootObject, false)
		writeFString(toc, o.instance, false)
		if lvl.version >= 49 {
			writeU32(toc, 0xC0000000) // object flags
		}
		if o.isActor {
			writeI32(toc, 1) // needTransform
			for i := range 4 {
				writeF32(toc, float32(i)) // rotation quaternion
			}
			for _, v := range o.translation {
				writeF32(toc, v)
			}
			for range 3 {
				writeF32(toc, 1) // scale
			}
			writeI32(toc, 1) // wasPlacedInLevel
		} else {
			writeFString(toc, o.parent, false)
		}
	}
	return toc.Bytes()
}

// buildBody serializes a decompressed save body. saveVersion gates which
// metadata appears; each level's TOC is written with its own version.
func buildBody(t *testing.T, saveVersion int32, levels []synthLevel) []byte {
	t.Helper()
	body := &bytes.Buffer{}

	if saveVersion >= 53 {
		writeU32(body, 1)               // version data version
		writeI32(body, 522)             // FileVersionUE4
		writeI32(body, 1012)            // FileVersionUE5
		writeI32(body, 0)               // licensee
		body.Write(make([]byte, 10))    // engine version u16*3 + u32
		writeFString(body, "++", false) // branch
		writeI32(body, 0)               // custom version count
	}

	// Validation grids: one grid with one cell.
	writeI32(body, 1)
	writeFString(body, "MainGrid", false)
	writeI32(body, 102400)
	writeU32(body, 0xDEADBEEF)
	writeU32(body, 1)
	writeFString(body, "Cell_X0_Y0", false)
	writeU32(body, 0xFEEDFACE)

	var persistent *synthLevel
	var sublevels []synthLevel
	for i := range levels {
		if levels[i].name == "" {
			persistent = &levels[i]
		} else {
			sublevels = append(sublevels, levels[i])
		}
	}
	if persistent == nil {
		t.Fatal("buildBody requires a persistent level (name == \"\")")
	}

	writeI32(body, int32(len(sublevels)))
	for _, lvl := range sublevels {
		writeFString(body, lvl.name, false)
		toc := writeTOC(t, lvl)
		writeI64(body, int64(len(toc)))
		body.Write(toc)
		data := []byte{0xDE, 0xAD, 0xBE, 0xEF, 0x00, 0x01} // skipped wholesale
		writeI64(body, int64(len(data)))
		body.Write(data)
		if saveVersion >= 51 {
			writeI32(body, lvl.version)
		}
		writeI32(body, 0) // collectables
		if saveVersion >= 53 {
			writeI32(body, 0) // no per-level version data
		}
	}

	// Persistent level: no name, then TOC/data/destroyed-actors map.
	toc := writeTOC(t, *persistent)
	writeI64(body, int64(len(toc)))
	body.Write(toc)
	writeI64(body, 0) // empty data blob
	writeI32(body, 0) // LevelToDestroyedActorsMap count

	full := &bytes.Buffer{}
	writeI64(full, int64(body.Len()))
	body.WriteTo(full)
	return full.Bytes()
}

func testHeaderFor(saveVersion int32) *Header {
	return &Header{
		HeaderVersion: 14,
		SaveVersion:   saveVersion,
		MapName:       "Persistent_Level",
		SessionName:   "Synthetic",
	}
}

func collectObjects(t *testing.T, saveVersion int32, levels []synthLevel) []ObjectHeader {
	t.Helper()
	body := buildBody(t, saveVersion, levels)
	var got []ObjectHeader
	err := WalkObjects(testHeaderFor(saveVersion), bytes.NewReader(body), func(o ObjectHeader) error {
		got = append(got, o)
		return nil
	})
	if err != nil {
		t.Fatalf("WalkObjects: %v", err)
	}
	return got
}

func standardLevels(version int32) []synthLevel {
	return []synthLevel{
		{
			name:    "Cell_X1_Y1",
			version: version,
			objects: []synthObject{
				{isActor: false, classPath: "/Script/FactoryGame.FGFactoryConnectionComponent",
					rootObject: "Persistent_Level", instance: "Persistent_Level:X1.Conn1", parent: "X1.Constructor"},
			},
		},
		{
			name:    "", // persistent
			version: version,
			objects: []synthObject{
				{isActor: true, classPath: "/Game/FactoryGame/Character/Player/Char_Player.Char_Player_C",
					rootObject: "Persistent_Level", instance: "Persistent_Level:PlayerCharacter",
					translation: [3]float32{100.5, -200, 3000}},
				{isActor: false, classPath: "/Script/FactoryGame.FGInventoryComponent",
					rootObject: "Persistent_Level", instance: "Persistent_Level:PlayerCharacter.Inventory",
					parent: "PlayerCharacter"},
			},
		},
	}
}

func TestWalkObjectsSV46(t *testing.T) {
	got := collectObjects(t, 46, standardLevels(46))

	if len(got) != 3 {
		t.Fatalf("got %d objects, want 3", len(got))
	}
	// Sublevel object first, then persistent level objects.
	if got[0].LevelName != "Cell_X1_Y1" || got[0].IsActor || got[0].ParentEntity != "X1.Constructor" {
		t.Errorf("sublevel component = %+v", got[0])
	}
	if got[1].LevelName != "Persistent_Level" || !got[1].IsActor {
		t.Errorf("player actor = %+v", got[1])
	}
	if !strings.Contains(got[1].ClassPath, "Char_Player") {
		t.Errorf("ClassPath = %q", got[1].ClassPath)
	}
	if got[1].Translation != [3]float32{100.5, -200, 3000} {
		t.Errorf("Translation = %v", got[1].Translation)
	}
	if got[2].IsActor || got[2].ParentEntity != "PlayerCharacter" {
		t.Errorf("inventory component = %+v", got[2])
	}
}

func TestWalkObjectsSV58(t *testing.T) {
	got := collectObjects(t, 58, standardLevels(58))
	if len(got) != 3 {
		t.Fatalf("got %d objects, want 3", len(got))
	}
	if got[1].Translation != [3]float32{100.5, -200, 3000} {
		t.Errorf("Translation = %v (flags misalignment?)", got[1].Translation)
	}
}

// A header-v58 save can contain stale sublevels whose TOC was written by an
// older game version without object flags. The per-level version field
// arrives AFTER the TOC blob — the walker must apply it anyway.
func TestWalkObjectsPerLevelVersionDivergence(t *testing.T) {
	levels := standardLevels(58)
	levels[0].version = 46 // stale sublevel: TOC written without flags
	got := collectObjects(t, 58, levels)

	if len(got) != 3 {
		t.Fatalf("got %d objects, want 3", len(got))
	}
	if got[0].ParentEntity != "X1.Constructor" {
		t.Errorf("stale sublevel component = %+v (flags gate misapplied?)", got[0])
	}
}

func TestWalkObjectsCallbackError(t *testing.T) {
	body := buildBody(t, 46, standardLevels(46))
	sentinel := errors.New("stop here")
	err := WalkObjects(testHeaderFor(46), bytes.NewReader(body), func(_ ObjectHeader) error {
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want sentinel", err)
	}
}

func TestWalkObjectsMalformed(t *testing.T) {
	good := buildBody(t, 46, standardLevels(46))

	cases := []struct {
		name   string
		mutate func([]byte) []byte
	}{
		{"truncated mid-TOC", func(b []byte) []byte { return b[:len(b)/2] }},
		{"empty body", func(_ []byte) []byte { return nil }},
		{
			"absurd level count",
			func(b []byte) []byte {
				// Level count sits right after the 8-byte size prefix + grids.
				// Corrupt by overwriting everything after the size prefix.
				binary.LittleEndian.PutUint32(b[8:], 0x7FFFFFFF)
				return b[:64]
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mutated := tc.mutate(append([]byte(nil), good...))
			err := WalkObjects(testHeaderFor(46), bytes.NewReader(mutated),
				func(ObjectHeader) error { return nil })
			if err == nil {
				t.Fatal("WalkObjects = nil error, want failure")
			}
		})
	}
}
