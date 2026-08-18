package blob

import (
	"errors"
	"math"
	"testing"
)

// TestReaderSignedPrimitives pins the two's-complement reading of the format's
// s1, s2 and s4 across the sign boundary. The fixture carries no negative
// integer of any width — the perk multiplier table it would come from is empty
// — so these widths are only covered here.
func TestReaderSignedPrimitives(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []byte
		want int8
	}{
		{"zero", []byte{0x00}, 0},
		{"one", []byte{0x01}, 1},
		{"max", []byte{0x7f}, math.MaxInt8},
		{"min", []byte{0x80}, math.MinInt8},
		{"minus one", []byte{0xff}, -1},
	} {
		t.Run("i8 "+tc.name, func(t *testing.T) {
			r := &reader{data: tc.data}
			if got := r.i8(); got != tc.want {
				t.Fatalf("i8(% x) = %d, want %d", tc.data, got, tc.want)
			}
			if r.err != nil || r.pos != len(tc.data) {
				t.Fatalf("consumed %d of %d bytes: %v", r.pos, len(tc.data), r.err)
			}
		})
	}
	for _, tc := range []struct {
		name string
		data []byte
		want int16
	}{
		{"zero", []byte{0x00, 0x00}, 0},
		{"positive", []byte{0x12, 0x34}, 0x1234},
		{"max", []byte{0x7f, 0xff}, math.MaxInt16},
		{"min", []byte{0x80, 0x00}, math.MinInt16},
		{"minus one", []byte{0xff, 0xff}, -1},
		{"low byte set below zero", []byte{0xff, 0xab}, -85},
	} {
		t.Run("i16 "+tc.name, func(t *testing.T) {
			r := &reader{data: tc.data}
			if got := r.i16(); got != tc.want {
				t.Fatalf("i16(% x) = %d, want %d", tc.data, got, tc.want)
			}
			if r.err != nil || r.pos != len(tc.data) {
				t.Fatalf("consumed %d of %d bytes: %v", r.pos, len(tc.data), r.err)
			}
		})
	}
	for _, tc := range []struct {
		name string
		data []byte
		want int32
	}{
		{"zero", []byte{0x00, 0x00, 0x00, 0x00}, 0},
		{"positive", []byte{0x12, 0x34, 0x56, 0x78}, 0x12345678},
		{"max", []byte{0x7f, 0xff, 0xff, 0xff}, math.MaxInt32},
		{"min", []byte{0x80, 0x00, 0x00, 0x00}, math.MinInt32},
		{"minus one", []byte{0xff, 0xff, 0xff, 0xff}, -1},
		{"minus two hundred", []byte{0xff, 0xff, 0xff, 0x38}, -200},
	} {
		t.Run("i32 "+tc.name, func(t *testing.T) {
			r := &reader{data: tc.data}
			if got := r.i32(); got != tc.want {
				t.Fatalf("i32(% x) = %d, want %d", tc.data, got, tc.want)
			}
			if r.err != nil || r.pos != len(tc.data) {
				t.Fatalf("consumed %d of %d bytes: %v", r.pos, len(tc.data), r.err)
			}
		})
	}
}

// TestReaderFloats pins that the float widths read their bits straight out of
// the big-endian bytes, including the values the JSON result cannot carry.
func TestReaderFloats(t *testing.T) {
	r := &reader{data: []byte{0x40, 0x49, 0x0f, 0xdb, 0x7f, 0xf0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}}
	if got := r.f32(); got != math.Float32frombits(0x40490fdb) {
		t.Fatalf("f32 = %v", got)
	}
	if got := r.f64(); !math.IsInf(got, 1) {
		t.Fatalf("f64 = %v, want +Inf", got)
	}
	if r.err != nil || r.pos != 12 {
		t.Fatalf("consumed %d of 12 bytes: %v", r.pos, r.err)
	}
}

// TestReaderRejectsShortSignedReads pins that every fixed-width read is still
// bounds-checked, and that a failed read leaves the cursor latched.
func TestReaderRejectsShortSignedReads(t *testing.T) {
	for name, read := range map[string]func(*reader){
		"i8":  func(r *reader) { r.i8() },
		"i16": func(r *reader) { r.i16() },
		"i32": func(r *reader) { r.i32() },
		"i64": func(r *reader) { r.i64() },
		"f64": func(r *reader) { r.f64() },
	} {
		t.Run(name, func(t *testing.T) {
			r := &reader{data: []byte{}}
			read(r)
			if !errors.Is(r.err, ErrShortRead) {
				t.Fatalf("error = %v, want ErrShortRead", r.err)
			}
			if r.pos != 0 {
				t.Fatalf("a failed read moved the cursor to %d", r.pos)
			}
		})
	}
}

// TestByteLenRejectsLengthsAboveInt32 pins the bound sized_blob.len_data is
// read under: a u4 length is checked on its true magnitude, never as a size
// that wrapped negative.
func TestByteLenRejectsLengthsAboveInt32(t *testing.T) {
	data := make([]byte, 8)
	data[0], data[1], data[2], data[3] = 0xff, 0xff, 0xff, 0xff
	r := &reader{data: data}
	if got := r.byteLen(); got != 0 {
		t.Fatalf("byteLen = %d, want 0", got)
	}
	if !errors.Is(r.err, ErrShortRead) {
		t.Fatalf("error = %v, want ErrShortRead", r.err)
	}
}
