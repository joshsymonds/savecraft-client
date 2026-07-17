package main

import (
	"encoding/binary"
	"errors"
	"io"
	"io/fs"
	"os"
	"runtime"
	"testing"

	"github.com/joshsymonds/savecraft-client/plugins/satisfactory/sav"
)

func openFixture(t *testing.T, name string) (io.Reader, func()) {
	t.Helper()
	f, err := os.Open("testdata/" + name)
	if errors.Is(err, fs.ErrNotExist) {
		t.Skipf("%s not present (gitignored fixture — see testdata/README.md for source URL)", name)
	}
	if err != nil {
		t.Fatalf("open %s: %v", name, err)
	}
	_, body, err := sav.Open(f)
	if err != nil {
		f.Close()
		t.Fatalf("sav.Open(%s): %v", name, err)
	}
	return body, func() { f.Close() }
}

func TestGoldenBodyInflatesEarlyGame(t *testing.T) {
	body, done := openFixture(t, "early_game.sav")
	defer done()

	// The decompressed body opens with an int64 of the remaining body size.
	var sizePrefix int64
	if err := binary.Read(body, binary.LittleEndian, &sizePrefix); err != nil {
		t.Fatalf("read body size prefix: %v", err)
	}

	rest, err := io.Copy(io.Discard, body)
	if err != nil {
		t.Fatalf("inflate body: %v", err)
	}

	// Ground truth from walking the chunk headers directly (see
	// testdata/README.md): 54 chunks totalling 7,052,456 decompressed bytes.
	const wantTotal = 7052456
	if got := rest + 8; got != wantTotal {
		t.Errorf("decompressed body = %d bytes, want %d", got, wantTotal)
	}
	if sizePrefix != wantTotal-8 {
		t.Errorf("body size prefix = %d, want %d", sizePrefix, wantTotal-8)
	}
}

// memWatchWriter samples HeapAlloc as bytes stream through it, recording the
// high-water mark so the test can assert the whole-body inflate never
// accumulates the decompressed blob in memory.
type memWatchWriter struct {
	written       int64
	nextSampleAt  int64
	maxHeapAlloc  uint64
	sampleEveryMB int64
}

func (w *memWatchWriter) Write(p []byte) (int, error) {
	w.written += int64(len(p))
	if w.written >= w.nextSampleAt {
		// Force a collection so the sample measures retained heap, not
		// GC-pacing-dependent floating garbage.
		runtime.GC()
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		if ms.HeapAlloc > w.maxHeapAlloc {
			w.maxHeapAlloc = ms.HeapAlloc
		}
		w.nextSampleAt = w.written + w.sampleEveryMB*(1<<20)
	}
	return len(p), nil
}

func TestGoldenMegafactoryStreamsWithBoundedMemory(t *testing.T) {
	body, done := openFixture(t, "megafactory.sav")
	defer done()

	watcher := &memWatchWriter{sampleEveryMB: 8}
	n, err := io.Copy(watcher, body)
	if err != nil {
		t.Fatalf("inflate body: %v", err)
	}

	if n < 100<<20 {
		t.Errorf("decompressed body = %d bytes, expected a 100MB+ megafactory", n)
	}
	const heapLimit = 32 << 20
	if watcher.maxHeapAlloc > heapLimit {
		t.Errorf("HeapAlloc high-water mark = %dMB while streaming %dMB body, want < %dMB",
			watcher.maxHeapAlloc>>20, n>>20, heapLimit>>20)
	}
	t.Logf("streamed %dMB decompressed, HeapAlloc high-water %dMB", n>>20, watcher.maxHeapAlloc>>20)
}
