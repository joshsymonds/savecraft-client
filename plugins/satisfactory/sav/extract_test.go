package sav

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

func extractObjects(t *testing.T, saveVersion int32, levels []synthLevel, want func(ObjectHeader) bool) []Object {
	t.Helper()
	body := buildBody(t, saveVersion, levels)
	var got []Object
	err := Extract(testHeaderFor(saveVersion), bytes.NewReader(body), want, func(o Object) error {
		got = append(got, o)
		return nil
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return got
}

func TestExtractFilteredSV46(t *testing.T) {
	want := func(o ObjectHeader) bool { return strings.Contains(o.ClassPath, "FGInventoryComponent") }
	got := extractObjects(t, 46, standardLevels(46), want)

	if len(got) != 1 {
		t.Fatalf("got %d objects, want 1", len(got))
	}
	obj := got[0]
	if !strings.Contains(obj.ClassPath, "FGInventoryComponent") {
		t.Errorf("ClassPath = %q", obj.ClassPath)
	}
	if string(obj.Data) != obj.InstanceName {
		t.Errorf("Data = %q, want instance name %q", obj.Data, obj.InstanceName)
	}
	if obj.SaveVersion != 46 {
		t.Errorf("SaveVersion = %d, want 46", obj.SaveVersion)
	}
}

// sv58: per-object version-data blocks trail some objects' payloads. The
// synthetic builder attaches one to the FIRST object of each level, so
// extracting a LATER object proves the framing was consumed correctly.
func TestExtractAfterVersionDataBlockSV58(t *testing.T) {
	want := func(o ObjectHeader) bool { return strings.Contains(o.ClassPath, "FGInventoryComponent") }
	got := extractObjects(t, 58, standardLevels(58), want)

	if len(got) != 1 {
		t.Fatalf("got %d objects, want 1", len(got))
	}
	if string(got[0].Data) != got[0].InstanceName {
		t.Errorf("Data = %q, want %q (version-data block misconsumed?)", got[0].Data, got[0].InstanceName)
	}
}

func TestExtractAll(t *testing.T) {
	got := extractObjects(t, 58, standardLevels(58), func(ObjectHeader) bool { return true })
	if len(got) != 3 {
		t.Fatalf("got %d objects, want 3", len(got))
	}
	for _, o := range got {
		if string(o.Data) != o.InstanceName {
			t.Errorf("object %q data = %q", o.InstanceName, o.Data)
		}
	}
}

func TestExtractNothingMatches(t *testing.T) {
	got := extractObjects(t, 46, standardLevels(46), func(ObjectHeader) bool { return false })
	if len(got) != 0 {
		t.Fatalf("got %d objects, want 0", len(got))
	}
}

func TestExtractCountMismatch(t *testing.T) {
	body := buildBody(t, 46, standardLevels(46))

	// Corrupt the sublevel's data-blob object count. The instance string
	// appears twice — in the TOC (as an FString) and as our synthetic data
	// payload — so take the LAST occurrence, then walk back over the
	// size/migrate/version fields (4 bytes each) to the count that precedes
	// the first object's framing.
	payload := []byte("Persistent_Level:X1.Conn1")
	idx := bytes.LastIndex(body, payload)
	if idx < 0 {
		t.Fatal("payload not found in synthetic body")
	}
	countOffset := idx - 4 - 4 - 4 - 4 // size, migrate, version, count
	binary.LittleEndian.PutUint32(body[countOffset:], 999)

	err := Extract(testHeaderFor(46), bytes.NewReader(body),
		func(ObjectHeader) bool { return true },
		func(Object) error { return nil })
	if err == nil {
		t.Fatal("Extract = nil error, want count mismatch failure")
	}
}

// Extraction parses sublevel TOCs eagerly (before the per-level version
// arrives) with a fallback across the flags gate. A stale no-flags sublevel
// inside a v58 save must still extract correctly via the fallback path.
func TestExtractPerLevelVersionDivergence(t *testing.T) {
	levels := standardLevels(58)
	levels[0].version = 46 // stale sublevel TOC: no object flags
	want := func(o ObjectHeader) bool { return strings.Contains(o.ClassPath, "FGFactoryConnectionComponent") }
	got := extractObjects(t, 58, levels, want)

	if len(got) != 1 {
		t.Fatalf("got %d objects, want 1", len(got))
	}
	if string(got[0].Data) != got[0].InstanceName {
		t.Errorf("Data = %q, want %q", got[0].Data, got[0].InstanceName)
	}
}

func TestWalkObjectsStillSkipsData(t *testing.T) {
	// WalkObjects must keep working against bodies with real data blobs.
	got := collectObjects(t, 58, standardLevels(58))
	if len(got) != 3 {
		t.Fatalf("got %d objects, want 3", len(got))
	}
}
