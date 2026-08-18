package blob

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"unicode/utf8"
)

// Sentinel errors returned by Decode.
var (
	ErrShortRead          = errors.New("zomboid blob: short read")
	ErrTrailingBytes      = errors.New("zomboid blob: trailing bytes")
	ErrUnsupportedVersion = errors.New("zomboid blob: unsupported world version")
)

// Widths in bytes of the fixed-size primitives the format uses.
const (
	sizeU16 = 2
	sizeU32 = 4
	sizeU64 = 8
)

// reader is a bounds-checked big-endian cursor over the BLOB; the whole
// format is big-endian (spec meta.endian: be).
//
// Once a read fails the reader latches the error and every later read is a
// no-op returning the zero value, so the decode functions read as
// straight-line ports of the .ksy types and the caller inspects err once,
// after the last field.
type reader struct {
	data []byte
	pos  int
	err  error
}

func (r *reader) fail(err error) {
	if r.err == nil {
		r.err = err
	}
}

func (r *reader) bytes(n int) []byte {
	if r.err != nil {
		return nil
	}
	if n < 0 || n > len(r.data)-r.pos {
		r.fail(fmt.Errorf("%w at offset %d requesting %d bytes", ErrShortRead, r.pos, n))
		return nil
	}
	raw := r.data[r.pos : r.pos+n]
	r.pos += n
	return raw
}

// skipTo consumes input up to end, which must not be behind the cursor.
func (r *reader) skipTo(end int) {
	r.bytes(end - r.pos)
}

func (r *reader) u8() uint8 {
	raw := r.bytes(1)
	if raw == nil {
		return 0
	}
	return raw[0]
}

// boolean reads a u1 the game writes as a Java boolean; any other value means
// the cursor has drifted out of alignment with the spec.
func (r *reader) boolean() bool {
	v := r.u8()
	if r.err == nil && v > 1 {
		r.fail(fmt.Errorf("zomboid blob: invalid boolean %d at offset %d", v, r.pos-1))
	}
	return v == 1
}

// i8 reads the format's s1: a two's-complement byte whose low seven bits carry
// the magnitude and whose top bit contributes int8's minimum.
func (r *reader) i8() int8 {
	raw := r.bytes(1)
	if raw == nil {
		return 0
	}
	value := int8(raw[0] & math.MaxInt8)
	if raw[0] > math.MaxInt8 {
		value += math.MinInt8
	}
	return value
}

func (r *reader) u16() uint16 {
	raw := r.bytes(sizeU16)
	if raw == nil {
		return 0
	}
	return binary.BigEndian.Uint16(raw)
}

// i16 and i32 accumulate their big-endian bytes into the signed type the spec
// declares, so the sign comes out of the bytes themselves.
func (r *reader) i16() int16 {
	raw := r.bytes(sizeU16)
	if raw == nil {
		return 0
	}
	return int16(raw[0])<<8 | int16(raw[1])
}

func (r *reader) u32() uint32 {
	raw := r.bytes(sizeU32)
	if raw == nil {
		return 0
	}
	return binary.BigEndian.Uint32(raw)
}

func (r *reader) i32() int32 {
	raw := r.bytes(sizeU32)
	if raw == nil {
		return 0
	}
	var value int32
	for _, b := range raw {
		value = value<<8 | int32(b)
	}
	return value
}

// i64 consumes the format's s8. No field the character sheet carries is one, so
// only its width and the bounds check matter and the value is dropped.
func (r *reader) i64() { r.bytes(sizeU64) }

func (r *reader) f32() float32 {
	return math.Float32frombits(r.u32())
}

func (r *reader) f64() float64 {
	raw := r.bytes(sizeU64)
	if raw == nil {
		return 0
	}
	return math.Float64frombits(binary.BigEndian.Uint64(raw))
}

// stringUTF reads common::string_utf: a u2 byte length and UTF-8 bytes.
func (r *reader) stringUTF() string {
	size := int(r.u16())
	raw := r.bytes(size)
	if raw == nil {
		return ""
	}
	if !utf8.Valid(raw) {
		r.fail(fmt.Errorf("zomboid blob: invalid UTF-8 string at offset %d", r.pos-size))
		return ""
	}
	return string(raw)
}

// count16 reads a 2-byte element count, bounded like count.
func (r *reader) count16() int {
	size := r.i16()
	if r.err != nil {
		return 0
	}
	if size < 0 || int(size) > len(r.data)-r.pos {
		r.fail(fmt.Errorf("%w: count %d at offset %d exceeds %d remaining bytes",
			ErrShortRead, size, r.pos-sizeU16, len(r.data)-r.pos))
		return 0
	}
	return int(size)
}

// byteLen reads a u4 byte length, bounded like count. sized_blob.len_data is
// the format's one unsigned length, and reading it through count would wrap a
// length above 2^31 to a negative size and report it as one.
func (r *reader) byteLen() int {
	size := r.u32()
	if r.err != nil {
		return 0
	}
	remaining := len(r.data) - r.pos
	if size > math.MaxInt32 || int(size) > remaining {
		r.fail(fmt.Errorf("%w: length %d at offset %d exceeds %d remaining bytes",
			ErrShortRead, size, r.pos-sizeU32, remaining))
		return 0
	}
	return int(size)
}

// count reads a 4-byte element count or byte length. Every repeated element in
// the format occupies at least one byte, so a count past the end of the input
// is corruption, not a large save; rejecting it here also bounds the slices
// the decoder preallocates.
func (r *reader) count() int {
	size := r.i32()
	if r.err != nil {
		return 0
	}
	if size < 0 || int(size) > len(r.data)-r.pos {
		r.fail(fmt.Errorf("%w: count %d at offset %d exceeds %d remaining bytes",
			ErrShortRead, size, r.pos-sizeU32, len(r.data)-r.pos))
		return 0
	}
	return int(size)
}
