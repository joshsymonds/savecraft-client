package sav

import (
	"bytes"
	"encoding/binary"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
)

// headerSpec describes a synthetic header to serialize for tests. The
// serializer mirrors FGSaveManagerInterface.h field order: saveName (v14+)
// comes BEFORE mapName.
type headerSpec struct {
	headerVersion int32
	saveVersion   int32
	buildVersion  int32
	saveName      string
	mapName       string
	mapOptions    string
	sessionName   string
	sessionUTF16  bool // serialize sessionName as UTF-16
	playSeconds   int32
	saveTicks     int64
	visibility    byte
	editorVersion int32
	modMetadata   string
	modded        int32
	saveID        string
	partitioned   int32
	md5Valid      bool
	creative      int32
}

func defaultSpec() headerSpec {
	return headerSpec{
		headerVersion: 14,
		saveVersion:   58,
		buildVersion:  423794,
		saveName:      "MyFactory",
		mapName:       "Persistent_Level",
		mapOptions:    "?startloc=Grass Fields",
		sessionName:   "MyFactory",
		playSeconds:   58723,
		// 2026-01-02 03:04:05 UTC in UE ticks (100ns since 0001-01-01).
		saveTicks:     621355968000000000 + time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC).UnixMilli()*10000,
		visibility:    1,
		editorVersion: 41,
		modMetadata:   "",
		modded:        0,
		saveID:        "8AB78F6F4A3B2E9C",
		partitioned:   1,
		md5Valid:      true,
		creative:      0,
	}
}

func writeFString(buf *bytes.Buffer, s string, asUTF16 bool) {
	if s == "" {
		binary.Write(buf, binary.LittleEndian, int32(0))
		return
	}
	if asUTF16 {
		units := utf16.Encode([]rune(s))
		units = append(units, 0)
		binary.Write(buf, binary.LittleEndian, int32(-len(units)))
		binary.Write(buf, binary.LittleEndian, units)
		return
	}
	binary.Write(buf, binary.LittleEndian, int32(len(s)+1))
	buf.WriteString(s)
	buf.WriteByte(0)
}

func buildHeader(spec headerSpec) []byte {
	buf := &bytes.Buffer{}
	le := binary.LittleEndian
	binary.Write(buf, le, spec.headerVersion)
	binary.Write(buf, le, spec.saveVersion)
	binary.Write(buf, le, spec.buildVersion)
	if spec.headerVersion >= 14 {
		writeFString(buf, spec.saveName, false)
	}
	writeFString(buf, spec.mapName, false)
	writeFString(buf, spec.mapOptions, false)
	writeFString(buf, spec.sessionName, spec.sessionUTF16)
	binary.Write(buf, le, spec.playSeconds)
	binary.Write(buf, le, spec.saveTicks)
	buf.WriteByte(spec.visibility)
	binary.Write(buf, le, spec.editorVersion)
	writeFString(buf, spec.modMetadata, false)
	binary.Write(buf, le, spec.modded)
	writeFString(buf, spec.saveID, false)
	binary.Write(buf, le, spec.partitioned)
	if spec.md5Valid {
		binary.Write(buf, le, int32(1))
		buf.Write(bytes.Repeat([]byte{0xAB}, 16))
	} else {
		binary.Write(buf, le, int32(0))
	}
	binary.Write(buf, le, spec.creative)
	return buf.Bytes()
}

func TestParseHeaderV14(t *testing.T) {
	spec := defaultSpec()
	h, err := ParseHeader(bytes.NewReader(buildHeader(spec)))
	if err != nil {
		t.Fatalf("ParseHeader: %v", err)
	}

	if h.HeaderVersion != 14 {
		t.Errorf("HeaderVersion = %d, want 14", h.HeaderVersion)
	}
	if h.SaveVersion != 58 {
		t.Errorf("SaveVersion = %d, want 58", h.SaveVersion)
	}
	if h.BuildVersion != 423794 {
		t.Errorf("BuildVersion = %d, want 423794", h.BuildVersion)
	}
	if h.SaveName != "MyFactory" {
		t.Errorf("SaveName = %q, want %q", h.SaveName, "MyFactory")
	}
	if h.MapName != "Persistent_Level" {
		t.Errorf("MapName = %q, want %q", h.MapName, "Persistent_Level")
	}
	if h.SessionName != "MyFactory" {
		t.Errorf("SessionName = %q, want %q", h.SessionName, "MyFactory")
	}
	if want := 58723 * time.Second; h.PlayDuration != want {
		t.Errorf("PlayDuration = %v, want %v", h.PlayDuration, want)
	}
	if want := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC); !h.SaveTime.Equal(want) {
		t.Errorf("SaveTime = %v, want %v", h.SaveTime, want)
	}
	if h.Modded {
		t.Error("Modded = true, want false")
	}
	if !h.PartitionedWorld {
		t.Error("PartitionedWorld = false, want true")
	}
	if h.CreativeMode {
		t.Error("CreativeMode = true, want false")
	}
	if h.SaveIdentifier != "8AB78F6F4A3B2E9C" {
		t.Errorf("SaveIdentifier = %q, want %q", h.SaveIdentifier, "8AB78F6F4A3B2E9C")
	}
}

func TestParseHeaderV13HasNoSaveName(t *testing.T) {
	spec := defaultSpec()
	spec.headerVersion = 13
	spec.saveVersion = 46 // 1.0 release
	h, err := ParseHeader(bytes.NewReader(buildHeader(spec)))
	if err != nil {
		t.Fatalf("ParseHeader: %v", err)
	}
	if h.SaveName != "" {
		t.Errorf("SaveName = %q, want empty for v13", h.SaveName)
	}
	if h.SessionName != "MyFactory" {
		t.Errorf("SessionName = %q, want %q", h.SessionName, "MyFactory")
	}
}

func TestParseHeaderUTF16SessionName(t *testing.T) {
	spec := defaultSpec()
	spec.sessionName = "Fabrik Müller™"
	spec.sessionUTF16 = true
	h, err := ParseHeader(bytes.NewReader(buildHeader(spec)))
	if err != nil {
		t.Fatalf("ParseHeader: %v", err)
	}
	if h.SessionName != "Fabrik Müller™" {
		t.Errorf("SessionName = %q, want %q", h.SessionName, "Fabrik Müller™")
	}
}

func TestParseHeaderInvalidMD5SkipsHashBytes(t *testing.T) {
	spec := defaultSpec()
	spec.md5Valid = false
	spec.creative = 1
	h, err := ParseHeader(bytes.NewReader(buildHeader(spec)))
	if err != nil {
		t.Fatalf("ParseHeader: %v", err)
	}
	if !h.CreativeMode {
		t.Error("CreativeMode = false, want true (field after MD5 misread?)")
	}
}

func TestParseHeaderOldHeaderVersion(t *testing.T) {
	spec := defaultSpec()
	spec.headerVersion = 12
	_, err := ParseHeader(bytes.NewReader(buildHeader(spec)))
	uve, ok := errors.AsType[*UnsupportedVersionError](err)
	if !ok {
		t.Fatalf("err = %v, want UnsupportedVersionError", err)
	}
	if uve.HeaderVersion != 12 {
		t.Errorf("HeaderVersion = %d, want 12", uve.HeaderVersion)
	}
	if !strings.Contains(uve.Error(), "12") || !strings.Contains(uve.Error(), "13") {
		t.Errorf("error %q should name found and supported versions", uve.Error())
	}
}

func TestParseHeaderPre10SaveVersion(t *testing.T) {
	spec := defaultSpec()
	spec.headerVersion = 13
	spec.saveVersion = 42 // Update 8, pre-1.0
	_, err := ParseHeader(bytes.NewReader(buildHeader(spec)))
	uve, ok := errors.AsType[*UnsupportedVersionError](err)
	if !ok {
		t.Fatalf("err = %v, want UnsupportedVersionError", err)
	}
	if uve.SaveVersion != 42 {
		t.Errorf("SaveVersion = %d, want 42", uve.SaveVersion)
	}
}

// A save from a future game version (unknown format) must be rejected with
// the explicit unsupported-version error naming found vs supported, not die
// later in the body with a misleading corrupt_file.
func TestParseHeaderFutureSaveVersion(t *testing.T) {
	spec := defaultSpec()
	spec.saveVersion = 99
	_, err := ParseHeader(bytes.NewReader(buildHeader(spec)))
	uve, ok := errors.AsType[*UnsupportedVersionError](err)
	if !ok {
		t.Fatalf("err = %v, want UnsupportedVersionError", err)
	}
	if uve.SaveVersion != 99 {
		t.Errorf("SaveVersion = %d, want 99", uve.SaveVersion)
	}
	if !strings.Contains(uve.Error(), "99") || !strings.Contains(uve.Error(), "60") {
		t.Errorf("error %q should name the found version and the supported ceiling", uve.Error())
	}
}

func TestParseHeaderTruncated(t *testing.T) {
	full := buildHeader(defaultSpec())
	for _, n := range []int{0, 4, 11, 40, len(full) - 1} {
		_, err := ParseHeader(bytes.NewReader(full[:n]))
		if err == nil {
			t.Errorf("ParseHeader(%d bytes) = nil error, want truncation error", n)
		}
		if _, ok := errors.AsType[*UnsupportedVersionError](err); ok {
			t.Errorf("ParseHeader(%d bytes) = UnsupportedVersionError, want corrupt-input error", n)
		}
	}
}

func TestParseHeaderGarbageStringLength(t *testing.T) {
	buf := &bytes.Buffer{}
	le := binary.LittleEndian
	binary.Write(buf, le, int32(14))
	binary.Write(buf, le, int32(58))
	binary.Write(buf, le, int32(423794))
	binary.Write(buf, le, int32(1<<30)) // absurd saveName length
	_, err := ParseHeader(bytes.NewReader(buf.Bytes()))
	if err == nil {
		t.Fatal("ParseHeader = nil error, want string-length error")
	}
}
