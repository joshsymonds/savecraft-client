package d2s

import (
	"fmt"
	"io"
)

// bitReader reads individual bits from a byte stream.
// D2S stores bits LSB-first within each byte: bit 0 of the value
// maps to bit 0 (LSB) of the byte.
type bitReader struct {
	r         io.ByteReader
	buf       uint64
	bits      uint // bits available in buf
	totalRead uint // total bits consumed (reset per-item)
}

func newBitReader(r io.ByteReader) *bitReader {
	return &bitReader{r: r}
}

// ReadBits reads n bits (up to 64) and returns them as a uint64.
// Bits are packed LSB-first: the first bit read becomes bit 0 of the result.
func (br *bitReader) ReadBits(n uint) (uint64, error) {
	for br.bits < n {
		b, err := br.r.ReadByte()
		if err != nil {
			return 0, fmt.Errorf("read bits: %w", err)
		}
		br.buf |= uint64(b) << br.bits
		br.bits += 8
	}

	val := br.buf & ((1 << n) - 1)
	br.buf >>= n
	br.bits -= n
	br.totalRead += n
	return val, nil
}

// ReadBit reads a single bit (0 or 1).
func (br *bitReader) ReadBit() (uint64, error) {
	return br.ReadBits(1)
}

// ReadByte reads 8 bits as a byte.
func (br *bitReader) ReadByte() (byte, error) {
	v, err := br.ReadBits(8)
	if err != nil {
		return 0, err
	}
	return byte(v), nil
}

// UnreadBits pushes previously-read bits back into the buffer.
// Used for speculative reads (e.g. normal quality 12-bit field).
func (br *bitReader) UnreadBits(val uint64, n uint) {
	br.buf = (br.buf << n) | (val & ((1 << n) - 1))
	br.bits += n
	br.totalRead -= n
}

// Align advances to the next byte boundary, discarding partial bits.
func (br *bitReader) Align() {
	if remainder := br.totalRead % 8; remainder != 0 {
		skip := 8 - remainder
		if skip <= br.bits {
			br.buf >>= skip
			br.bits -= skip
			br.totalRead += skip
		}
	}
}

// BitsRead returns the total number of bits consumed.
func (br *bitReader) BitsRead() uint {
	return br.totalRead
}
