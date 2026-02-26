package d2s

import (
	"fmt"
	"io"
)

// bitReader reads individual bits from a byte stream.
// D2 stores bits LSB-first within each byte.
type bitReader struct {
	r        io.ByteReader
	buf      uint64
	bits     uint // bits available in buf
	totalRead uint // total bits consumed
}

func newBitReader(r io.ByteReader) *bitReader {
	return &bitReader{r: r}
}

// ReadBits reads n bits (up to 64) and returns them as a uint64.
func (br *bitReader) ReadBits(n uint) (uint64, error) {
	for br.bits < n {
		b, err := br.r.ReadByte()
		if err != nil {
			return 0, fmt.Errorf("read bits: %w", err)
		}
		br.buf |= uint64(reverseByte(b)) << br.bits
		br.bits += 8
	}

	val := br.buf & ((1 << n) - 1)
	br.buf >>= n
	br.bits -= n
	br.totalRead += n
	return reverseBits(val, n), nil
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

// reverseByte reverses the bit order of a single byte.
func reverseByte(b byte) byte {
	b = (b&0xF0)>>4 | (b&0x0F)<<4
	b = (b&0xCC)>>2 | (b&0x33)<<2
	b = (b&0xAA)>>1 | (b&0x55)<<1
	return b
}

// reverseBits reverses the bit order of n bits within a uint64.
func reverseBits(v uint64, n uint) uint64 {
	var r uint64
	for i := uint(0); i < n; i++ {
		r = (r << 1) | (v & 1)
		v >>= 1
	}
	return r
}
