package sav

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"io"
	"strings"
	"testing"
)

// buildChunk serializes one compressed chunk in the on-disk wire format
// (verified against real 1.0/1.2 fixtures — see chunks.go).
func buildChunk(t *testing.T, payload []byte) []byte {
	t.Helper()
	var compressed bytes.Buffer
	zw := zlib.NewWriter(&compressed)
	if _, err := zw.Write(payload); err != nil {
		t.Fatalf("compress: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close compressor: %v", err)
	}

	buf := &bytes.Buffer{}
	le := binary.LittleEndian
	binary.Write(buf, le, uint32(chunkMagic))
	binary.Write(buf, le, uint32(archiveV2Flag))
	binary.Write(buf, le, int64(maxChunkSize))
	buf.WriteByte(compressorZlib)
	for range 2 {
		binary.Write(buf, le, int64(compressed.Len()))
		binary.Write(buf, le, int64(len(payload)))
	}
	buf.Write(compressed.Bytes())
	return buf.Bytes()
}

func TestChunkReaderMultiChunkRoundTrip(t *testing.T) {
	// Three chunks with distinct, odd-sized payloads so a read that crosses
	// chunk boundaries proves reassembly is seamless.
	payloads := [][]byte{
		bytes.Repeat([]byte{0xAA, 0xBB}, 1000),
		[]byte("short middle chunk"),
		bytes.Repeat([]byte{0x01, 0x02, 0x03}, 4444),
	}
	stream := &bytes.Buffer{}
	want := &bytes.Buffer{}
	for _, p := range payloads {
		stream.Write(buildChunk(t, p))
		want.Write(p)
	}

	got, err := io.ReadAll(newChunkReader(newReader(stream)))
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, want.Bytes()) {
		t.Fatalf("round trip mismatch: got %d bytes, want %d", len(got), want.Len())
	}
}

func TestChunkReaderEmptyStream(t *testing.T) {
	got, err := io.ReadAll(newChunkReader(newReader(bytes.NewReader(nil))))
	if err != nil {
		t.Fatalf("ReadAll on empty stream: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %d bytes from empty stream", len(got))
	}
}

func TestChunkReaderCorruptCases(t *testing.T) {
	good := func() []byte { return buildChunk(t, []byte("good chunk payload")) }

	cases := []struct {
		name    string
		mutate  func([]byte) []byte
		wantErr string
	}{
		{
			name: "wrong magic",
			mutate: func(b []byte) []byte {
				b[0] = 0x00
				return b
			},
			wantErr: "magic",
		},
		{
			name: "unknown archive flag",
			mutate: func(b []byte) []byte {
				b[4] = 0x11
				return b
			},
			wantErr: "archive",
		},
		{
			name: "unknown compressor",
			mutate: func(b []byte) []byte {
				b[16] = 7 // after magic(4)+flag(4)+maxsize(8)
				return b
			},
			wantErr: "compressor",
		},
		{
			name: "size copies disagree",
			mutate: func(b []byte) []byte {
				b[17]++ // first byte of compressed size copy 1
				return b
			},
			wantErr: "size",
		},
		{
			name: "truncated payload",
			mutate: func(b []byte) []byte {
				return b[:len(b)-5]
			},
			wantErr: "",
		},
		{
			name: "garbage compressed bytes",
			mutate: func(b []byte) []byte {
				for i := 49; i < len(b); i++ { // past the 49-byte chunk header
					b[i] = 0xFF
				}
				return b
			},
			wantErr: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stream := tc.mutate(good())
			_, err := io.ReadAll(newChunkReader(newReader(bytes.NewReader(stream))))
			if err == nil {
				t.Fatal("ReadAll = nil error, want failure")
			}
			if tc.wantErr != "" && !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q does not mention %q", err, tc.wantErr)
			}
		})
	}
}

func TestChunkReaderRejectsAbsurdSizes(t *testing.T) {
	buf := &bytes.Buffer{}
	le := binary.LittleEndian
	binary.Write(buf, le, uint32(chunkMagic))
	binary.Write(buf, le, uint32(archiveV2Flag))
	binary.Write(buf, le, int64(maxChunkSize))
	buf.WriteByte(compressorZlib)
	for range 2 {
		binary.Write(buf, le, int64(1<<40)) // absurd compressed size
		binary.Write(buf, le, int64(maxChunkSize))
	}

	_, err := io.ReadAll(newChunkReader(newReader(buf)))
	if err == nil {
		t.Fatal("ReadAll = nil error, want size-limit failure")
	}
}

func TestOpenReturnsHeaderAndBody(t *testing.T) {
	spec := defaultSpec()
	file := &bytes.Buffer{}
	file.Write(buildHeader(spec))
	file.Write(buildChunk(t, []byte("body bytes after header")))

	h, body, err := Open(file)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if h.SessionName != "MyFactory" {
		t.Errorf("SessionName = %q, want MyFactory", h.SessionName)
	}
	got, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(got) != "body bytes after header" {
		t.Errorf("body = %q", got)
	}
}
