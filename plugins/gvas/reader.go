package gvas

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"unicode/utf16"
)

// reader wraps an io.Reader with position tracking and little-endian helpers.
type reader struct {
	r   io.Reader
	pos int64
	buf []byte // scratch buffer, reused
}

func newReader(r io.Reader) *reader {
	return &reader{r: r, buf: make([]byte, 16)}
}

func (r *reader) read(p []byte) error {
	n, err := io.ReadFull(r.r, p)
	r.pos += int64(n)
	if err != nil {
		return fmt.Errorf("read at offset %d: %w", r.pos-int64(n), err)
	}
	return nil
}

func (r *reader) readU8() (uint8, error) {
	if err := r.read(r.buf[:1]); err != nil {
		return 0, err
	}
	return r.buf[0], nil
}

func (r *reader) readU16() (uint16, error) {
	if err := r.read(r.buf[:2]); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint16(r.buf[:2]), nil
}

func (r *reader) readU32() (uint32, error) {
	if err := r.read(r.buf[:4]); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(r.buf[:4]), nil
}

func (r *reader) readI32() (int32, error) {
	v, err := r.readU32()
	return int32(v), err
}

func (r *reader) readU64() (uint64, error) {
	if err := r.read(r.buf[:8]); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint64(r.buf[:8]), nil
}

func (r *reader) readI64() (int64, error) {
	v, err := r.readU64()
	return int64(v), err
}

func (r *reader) readF32() (float32, error) {
	v, err := r.readU32()
	return math.Float32frombits(v), err
}

func (r *reader) readF64() (float64, error) {
	v, err := r.readU64()
	return math.Float64frombits(v), err
}

func (r *reader) readBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if err := r.read(b); err != nil {
		return nil, err
	}
	return b, nil
}

// readFString reads a UE FString: i32 length, then UTF-8 or UTF-16LE data.
func (r *reader) readFString() (string, error) {
	length, err := r.readI32()
	if err != nil {
		return "", fmt.Errorf("fstring length: %w", err)
	}
	if length == 0 {
		return "", nil
	}
	// Bound string allocation to prevent OOM from corrupt data.
	absLen := length
	if absLen < 0 {
		absLen = -absLen
	}
	if int(absLen) > maxStringBytes {
		return "", fmt.Errorf("fstring length %d exceeds limit", absLen)
	}
	if length > 0 {
		data, err := r.readBytes(int(length))
		if err != nil {
			return "", fmt.Errorf("fstring data (len=%d): %w", length, err)
		}
		// Strip null terminator.
		if len(data) > 0 && data[len(data)-1] == 0 {
			data = data[:len(data)-1]
		}
		return string(data), nil
	}
	// Negative length: UTF-16LE.
	charCount := -length
	raw, err := r.readBytes(int(charCount) * 2)
	if err != nil {
		return "", fmt.Errorf("fstring utf16 data (chars=%d): %w", charCount, err)
	}
	u16s := make([]uint16, charCount)
	for i := range u16s {
		u16s[i] = binary.LittleEndian.Uint16(raw[i*2 : i*2+2])
	}
	// Strip null terminator u16.
	if len(u16s) > 0 && u16s[len(u16s)-1] == 0 {
		u16s = u16s[:len(u16s)-1]
	}
	return string(utf16.Decode(u16s)), nil
}

// readGuid reads 16 bytes as a Guid (4x u32 LE).
func (r *reader) readGuid() (Guid, error) {
	var g Guid
	a, err := r.readU32()
	if err != nil {
		return g, err
	}
	g.A = a
	if err := r.read(g.B[:]); err != nil {
		return g, err
	}
	if err := r.read(g.C[:]); err != nil {
		return g, err
	}
	d, err := r.readU32()
	if err != nil {
		return g, err
	}
	g.D = d
	return g, nil
}
