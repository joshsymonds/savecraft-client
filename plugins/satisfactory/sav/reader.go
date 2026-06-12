package sav

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf16"
)

// maxFStringBytes guards against corrupt length prefixes. Real save strings
// (session names, object paths, property names) are well under this.
const maxFStringBytes = 1 << 24 // 16MB

// reader wraps an io.Reader with little-endian primitive reads and offset
// tracking for error reporting. All reads are sequential — the underlying
// stream is consumed exactly once, which keeps whole-save parsing streamable.
type reader struct {
	r   *bufio.Reader
	off int64
}

func newReader(r io.Reader) *reader {
	return &reader{r: bufio.NewReaderSize(r, 64*1024)}
}

// errAt wraps err with the stream offset where the failed read started.
func (r *reader) errAt(start int64, what string, err error) error {
	if errors.Is(err, io.EOF) {
		err = io.ErrUnexpectedEOF
	}
	return fmt.Errorf("read %s at offset %d: %w", what, start, err)
}

func (r *reader) bytes(n int, what string) ([]byte, error) {
	start := r.off
	buf := make([]byte, n)
	if _, err := io.ReadFull(r.r, buf); err != nil {
		return nil, r.errAt(start, what, err)
	}
	r.off += int64(n)
	return buf, nil
}

// discard skips n bytes without buffering them.
func (r *reader) discard(n int64, what string) error {
	start := r.off
	skipped, err := r.r.Discard(int(n))
	r.off += int64(skipped)
	if err != nil {
		return r.errAt(start, what, err)
	}
	return nil
}

func (r *reader) int32(what string) (int32, error) {
	buf, err := r.bytes(4, what)
	if err != nil {
		return 0, err
	}
	return int32(binary.LittleEndian.Uint32(buf)), nil
}

func (r *reader) int64(what string) (int64, error) {
	buf, err := r.bytes(8, what)
	if err != nil {
		return 0, err
	}
	return int64(binary.LittleEndian.Uint64(buf)), nil
}

func (r *reader) byte(what string) (byte, error) {
	buf, err := r.bytes(1, what)
	if err != nil {
		return 0, err
	}
	return buf[0], nil
}

// fstring reads a UE FString: int32 length prefix, then either UTF-8 bytes
// (length > 0) or UTF-16LE code units (length < 0), each null-terminated.
// Length 0 is the empty string with no data bytes.
func (r *reader) fstring(what string) (string, error) {
	start := r.off
	length, err := r.int32(what + " length")
	if err != nil {
		return "", err
	}

	switch {
	case length == 0:
		return "", nil

	case length > 0:
		if length > maxFStringBytes {
			return "", r.errAt(start, what,
				fmt.Errorf("string length %d exceeds %d byte limit", length, maxFStringBytes))
		}
		buf, readErr := r.bytes(int(length), what)
		if readErr != nil {
			return "", readErr
		}
		return strings.TrimRight(string(buf), "\x00"), nil

	default: // length < 0: UTF-16
		n := -int64(length) * 2
		if n > maxFStringBytes {
			return "", r.errAt(start, what,
				fmt.Errorf("string length %d exceeds %d byte limit", n, maxFStringBytes))
		}
		buf, readErr := r.bytes(int(n), what)
		if readErr != nil {
			return "", readErr
		}
		units := make([]uint16, len(buf)/2)
		for i := range units {
			units[i] = binary.LittleEndian.Uint16(buf[i*2:])
		}
		return strings.TrimRight(string(utf16.Decode(units)), "\x00"), nil
	}
}
