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

func walkFixture(t *testing.T, name string) map[string]int {
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
	classes := map[string]int{}
	err = sav.WalkObjects(h, body, func(o sav.ObjectHeader) error {
		classes[o.ClassPath]++
		return nil
	})
	if err != nil {
		t.Fatalf("WalkObjects(%s): %v", name, err)
	}
	return classes
}

func total(classes map[string]int) int {
	n := 0
	for _, c := range classes {
		n += c
	}
	return n
}

func findClass(classes map[string]int, fragment string) (string, int) {
	for cls, n := range classes {
		if strings.Contains(cls, fragment) {
			return cls, n
		}
	}
	return "", 0
}

// Expected totals were established independently with a python walker over
// the raw bytes (see task notes); these tests pin the Go walker to them.
func TestGoldenWalkEarlyGame(t *testing.T) {
	classes := walkFixture(t, "early_game.sav")

	if got := total(classes); got != 6853 {
		t.Errorf("total objects = %d, want 6853", got)
	}
	if cls, n := findClass(classes, "Char_Player"); n != 1 {
		t.Errorf("player class %q count = %d, want 1", cls, n)
	}
	if _, n := findClass(classes, "BP_ResourceNode."); n != 459 {
		t.Errorf("resource nodes = %d, want 459", n)
	}
}

func TestGoldenWalkCurrent12(t *testing.T) {
	classes := walkFixture(t, "current_1_2.sav")

	if got := total(classes); got != 1066 {
		t.Errorf("total objects = %d, want 1066", got)
	}
	if _, n := findClass(classes, "Char_Player"); n != 1 {
		t.Errorf("player count = %d, want 1", n)
	}
}

func TestGoldenWalkMegafactoryBoundedMemory(t *testing.T) {
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

	var count int
	var maxHeap uint64
	err = sav.WalkObjects(h, body, func(_ sav.ObjectHeader) error {
		count++
		if count%50000 == 0 {
			var ms runtime.MemStats
			runtime.ReadMemStats(&ms)
			if ms.HeapAlloc > maxHeap {
				maxHeap = ms.HeapAlloc
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WalkObjects: %v", err)
	}

	if count != 400307 {
		t.Errorf("total objects = %d, want 400307", count)
	}
	const heapLimit = 32 << 20
	if maxHeap > heapLimit {
		t.Errorf("HeapAlloc high-water = %dMB walking %d objects, want < %dMB",
			maxHeap>>20, count, heapLimit>>20)
	}
	t.Logf("walked %d objects, HeapAlloc high-water %dMB", count, maxHeap>>20)
}
