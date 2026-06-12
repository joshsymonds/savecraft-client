package main

import (
	"errors"
	"io/fs"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/joshsymonds/savecraft.gg/plugins/satisfactory/sav"
)

func extractFromFixture(t *testing.T, name string, want func(string) bool) []sav.Object {
	t.Helper()
	f, err := os.Open("testdata/" + name)
	if errors.Is(err, fs.ErrNotExist) {
		t.Skipf("%s not present (gitignored fixture — see testdata/README.md for source URL)", name)
	}
	if err != nil {
		t.Fatalf("open %s: %v", name, err)
	}
	defer f.Close()

	h, body, err := sav.Open(f)
	if err != nil {
		t.Fatalf("sav.Open(%s): %v", name, err)
	}
	var got []sav.Object
	err = sav.Extract(h, body, want, func(o sav.Object) error {
		got = append(got, o)
		return nil
	})
	if err != nil {
		t.Fatalf("Extract(%s): %v", name, err)
	}
	return got
}

func wantPlayer(cls string) bool { return strings.Contains(cls, "Char_Player.") }

// Expected data sizes were established independently with a python walker
// over the raw bytes; these tests pin the Go extractor to them.
func TestGoldenExtractPlayerEarlyGame(t *testing.T) {
	got := extractFromFixture(t, "early_game.sav", wantPlayer)

	if len(got) != 1 {
		t.Fatalf("got %d player objects, want 1", len(got))
	}
	if len(got[0].Data) != 3852 {
		t.Errorf("player data = %d bytes, want 3852", len(got[0].Data))
	}
	if got[0].SaveVersion != 46 {
		t.Errorf("player object SaveVersion = %d, want 46", got[0].SaveVersion)
	}
	if !got[0].IsActor {
		t.Error("player is not an actor")
	}
}

func TestGoldenExtractPlayerCurrent12(t *testing.T) {
	got := extractFromFixture(t, "current_1_2.sav", wantPlayer)

	if len(got) != 1 {
		t.Fatalf("got %d player objects, want 1", len(got))
	}
	if len(got[0].Data) != 4171 {
		t.Errorf("player data = %d bytes, want 4171", len(got[0].Data))
	}
	if got[0].SaveVersion != 58 {
		t.Errorf("player object SaveVersion = %d, want 58", got[0].SaveVersion)
	}
}

func TestGoldenExtractMegafactoryBoundedMemory(t *testing.T) {
	f, err := os.Open("testdata/megafactory.sav")
	if errors.Is(err, fs.ErrNotExist) {
		t.Skip("megafactory.sav not present (gitignored fixture)")
	}
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()

	h, body, err := sav.Open(f)
	if err != nil {
		t.Fatalf("sav.Open: %v", err)
	}

	// Sample the heap from inside the class filter, which fires for every
	// object — that catches transient whole-blob buffering, not just
	// retained memory.
	var players, calls int
	var maxHeap uint64
	want := func(cls string) bool {
		calls++
		if calls%50000 == 0 {
			var ms runtime.MemStats
			runtime.ReadMemStats(&ms)
			if ms.HeapAlloc > maxHeap {
				maxHeap = ms.HeapAlloc
			}
		}
		return wantPlayer(cls)
	}
	err = sav.Extract(h, body, want, func(o sav.Object) error {
		players++
		if len(o.Data) == 0 {
			t.Errorf("player object %q has empty data", o.InstanceName)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	if players < 1 {
		t.Errorf("players = %d, want >= 1", players)
	}
	const heapLimit = 32 << 20
	if maxHeap > heapLimit {
		t.Errorf("HeapAlloc high-water = %dMB during extraction, want < %dMB", maxHeap>>20, heapLimit>>20)
	}
	t.Logf("extracted %d player objects, HeapAlloc high-water %dMB", players, maxHeap>>20)
}
