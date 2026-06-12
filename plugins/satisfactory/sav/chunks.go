package sav

import (
	"compress/zlib"
	"errors"
	"fmt"
	"io"
)

// Compressed chunk wire format, verified against 1.0/1.1/1.2 saves:
//
//	int32  magic (0x9E2A83C1, UE PACKAGE_FILE_TAG)
//	int32  archive flag (0x22222222 = v2; v1 saves predate game 1.0)
//	int64  max decompressed chunk size (always 131072)
//	byte   compressor (3 = zlib; v2 archives only)
//	int64  compressed size   ┐ stored twice with
//	int64  uncompressed size │ identical values
//	int64  compressed size   │
//	int64  uncompressed size ┘
//	[]byte zlib stream of compressed-size bytes
//
// Chunks repeat back to back until end of file.
const (
	chunkMagic     = 0x9E2A83C1
	archiveV2Flag  = 0x22222222
	maxChunkSize   = 131072
	compressorZlib = 3

	// maxCompressedChunk guards against corrupt size fields. zlib can expand
	// incompressible input slightly, so allow generous headroom over
	// maxChunkSize before declaring the header corrupt.
	maxCompressedChunk = 4 * maxChunkSize
)

// chunkReader is a streaming io.Reader over the concatenated decompressed
// chunks of a save body. At most one chunk's compressed bytes are in flight
// at a time — memory stays bounded no matter how large the save is.
type chunkReader struct {
	r       *reader
	current io.ReadCloser // zlib stream of the chunk being read; nil between chunks
	err     error         // sticky
}

func newChunkReader(r *reader) *chunkReader {
	return &chunkReader{r: r}
}

// Open parses the header and returns a streaming reader over the
// decompressed save body. The body reader is only valid while src is.
func Open(src io.Reader) (*Header, io.Reader, error) {
	r := newReader(src)
	h, err := parseHeader(r)
	if err != nil {
		return nil, nil, err
	}
	return h, newChunkReader(r), nil
}

func (c *chunkReader) Read(p []byte) (int, error) {
	if c.err != nil {
		return 0, c.err
	}
	for {
		if c.current == nil {
			if err := c.nextChunk(); err != nil {
				c.err = err
				return 0, err
			}
		}
		n, err := c.current.Read(p)
		switch {
		case errors.Is(err, io.EOF):
			// Chunk exhausted — close it and move on. Return what we have
			// if anything was read; otherwise loop into the next chunk.
			c.current.Close()
			c.current = nil
			if n > 0 {
				return n, nil
			}
		case err != nil:
			c.err = fmt.Errorf("inflate chunk: %w", err)
			return n, c.err
		default:
			return n, nil
		}
	}
}

// nextChunk reads one chunk header and primes the zlib stream for its
// payload. Returns io.EOF (unwrapped) at a clean end of stream.
func (c *chunkReader) nextChunk() error {
	start := c.r.off
	magic, err := c.r.int32("chunk magic")
	if err != nil {
		// A clean EOF exactly at a chunk boundary is the end of the body.
		if errors.Is(err, io.ErrUnexpectedEOF) && c.r.off == start {
			return io.EOF
		}
		return err
	}
	if uint32(magic) != chunkMagic {
		return fmt.Errorf("chunk at offset %d: bad magic 0x%08X, want 0x%08X", start, uint32(magic), uint32(chunkMagic))
	}

	flag, err := c.r.int32("archive flag")
	if err != nil {
		return err
	}
	if uint32(flag) != archiveV2Flag {
		return fmt.Errorf("chunk at offset %d: unknown archive flag 0x%08X (pre-1.0 save?)", start, uint32(flag))
	}

	if _, sizeErr := c.r.int64("max chunk size"); sizeErr != nil {
		return sizeErr
	}

	compressor, err := c.r.byte("compressor")
	if err != nil {
		return err
	}
	if compressor != compressorZlib {
		return fmt.Errorf("chunk at offset %d: unsupported compressor %d, want %d (zlib)",
			start, compressor, compressorZlib)
	}

	compSize, uncompSize, err := c.readSizes()
	if err != nil {
		return err
	}
	if compSize <= 0 || compSize > maxCompressedChunk || uncompSize <= 0 || uncompSize > maxChunkSize {
		return fmt.Errorf("chunk at offset %d: implausible sizes (compressed %d, uncompressed %d)",
			start, compSize, uncompSize)
	}

	zr, err := zlib.NewReader(io.LimitReader(c.r.r, compSize))
	if err != nil {
		return fmt.Errorf("chunk at offset %d: open zlib stream: %w", start, err)
	}
	c.r.off += compSize
	c.current = zr
	return nil
}

// readSizes reads the duplicated (compressed, uncompressed) size pairs and
// verifies both copies agree.
func (c *chunkReader) readSizes() (compSize, uncompSize int64, err error) {
	if compSize, err = c.r.int64("compressed size"); err != nil {
		return 0, 0, err
	}
	if uncompSize, err = c.r.int64("uncompressed size"); err != nil {
		return 0, 0, err
	}
	compSize2, err := c.r.int64("compressed size copy")
	if err != nil {
		return 0, 0, err
	}
	uncompSize2, err := c.r.int64("uncompressed size copy")
	if err != nil {
		return 0, 0, err
	}
	if compSize != compSize2 || uncompSize != uncompSize2 {
		return 0, 0, fmt.Errorf(
			"chunk size copies disagree: compressed %d/%d, uncompressed %d/%d",
			compSize, compSize2, uncompSize, uncompSize2,
		)
	}
	return compSize, uncompSize, nil
}
