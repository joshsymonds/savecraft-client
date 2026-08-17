// Package sqlite reads SQLite 3 database files from memory. It is a read-only,
// stdlib-only table scanner used by the zomboid parser: the save's .db files are
// handed over as a byte slice and every table is walked directly out of the
// b-tree pages. There is no SQL, no query planner, and no cgo, which is what
// lets the parser run as a wasip1/wasm module under wazero.
//
// Only what the zomboid saves actually use is supported: UTF-8 text encoding,
// table b-trees (interior type 5 and leaf type 13), overflow pages, and the
// standard record serial types. Indexes, WAL frames, and freelist reuse are
// ignored. Offsets and formulas below cite the SQLite file format spec
// (https://sqlite.org/fileformat.html).
package sqlite

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
)

// ErrCorrupt is returned, wrapped with a description of the specific check that
// failed, whenever the file does not conform to the SQLite file format.
var ErrCorrupt = errors.New("sqlite corrupt")

// ErrNoSuchTable is returned, wrapped with the requested name, when Rows is
// asked for a table that is not in the database schema.
var ErrNoSuchTable = errors.New("sqlite table not found")

// Kind is the storage class of a Value, mirroring SQLite's five datatypes.
type Kind uint8

// The storage classes a Value can hold.
const (
	Null Kind = iota
	Int64
	Float64
	Text
	Blob
)

// Value is a single decoded column value. Exactly one accessor is meaningful,
// selected by Kind; the others return their zero value.
type Value struct {
	kind Kind
	i    int64
	f    float64
	s    string
	b    []byte
}

// Kind reports which storage class the value holds.
func (v Value) Kind() Kind { return v.kind }

// Int64 returns the integer value, or 0 if Kind is not Int64.
func (v Value) Int64() int64 { return v.i }

// Float64 returns the floating-point value, or 0 if Kind is not Float64.
func (v Value) Float64() float64 { return v.f }

// Text returns the string value, or "" if Kind is not Text.
func (v Value) Text() string { return v.s }

// Blob returns the byte value, or nil if Kind is not Blob. The slice is a copy
// and does not alias the underlying database bytes.
func (v Value) Blob() []byte { return v.b }

// Row is one table row: Columns holds the column names parsed from the table's
// CREATE statement, and Values holds the decoded values in the same order. Each
// row owns both slices, so a caller may modify them freely.
type Row struct {
	Columns []string
	Values  []Value
}

// DB is an opened, read-only SQLite database backed entirely by an in-memory
// byte slice. It is safe for concurrent reads and never writes to the file.
type DB struct {
	data              []byte
	ps, pages, usable int
	names             map[string]table
}

// table is a schema entry from sqlite_master: the root page of its b-tree, its
// column names, and the index of its INTEGER PRIMARY KEY column (-1 if none).
type table struct {
	root uint32
	cols []string
	pk   int
}

// Tables returns the names of every table in the database, sorted, so that
// callers iterate in a deterministic order.
func (d *DB) Tables() []string {
	out := make([]string, 0, len(d.names))
	for n := range d.names {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// bad wraps ErrCorrupt with the specific integrity check that failed.
func bad(s string) error { return fmt.Errorf("%w: %s", ErrCorrupt, s) }

// Open validates the 100-byte database header, then reads the schema so that
// Tables and Rows can be served. The data slice is retained and must not be
// modified for the lifetime of the returned DB. It returns an error wrapping
// ErrCorrupt if the header fails validation or the schema cannot be parsed.
func Open(data []byte) (*DB, error) {
	if len(data) < 100 || !bytes.Equal(data[:16], []byte("SQLite format 3\x00")) {
		return nil, bad("bad magic or truncated header")
	}
	ps := int(binary.BigEndian.Uint16(data[16:18]))
	// Header offset 16 stores a 64KiB page size as 1, the one value that does
	// not fit the u16 (file format, "The Database Header").
	if ps == 1 {
		ps = 65536
	}
	if ps < 512 || ps > 65536 || ps&(ps-1) != 0 {
		return nil, bad("invalid page size")
	}
	if data[18] < 1 || data[18] > 2 || data[19] < 1 || data[19] > 2 {
		return nil, bad("unsupported file format")
	}
	reserved := int(data[20])
	// The usable size is the page size less the reserved space, and SQLite
	// requires it to be at least 480 bytes; the local-payload formula below
	// assumes as much (file format, "The Database Header").
	if ps-reserved < 480 {
		return nil, bad("usable page size below 480 bytes")
	}
	if binary.BigEndian.Uint32(data[56:60]) != 1 {
		return nil, bad("text encoding is not UTF-8")
	}
	if len(data)%ps != 0 {
		return nil, bad("file length is not a whole number of pages")
	}
	pages := len(data) / ps
	claimed := int(binary.BigEndian.Uint32(data[28:32]))
	// The in-header database size at offset 28 is only authoritative when the
	// version-valid-for number (offset 92) equals the file change counter
	// (offset 24) and the size is non-zero; otherwise it is stale and the file
	// length wins (file format, "The Database Header").
	if binary.BigEndian.Uint32(data[92:96]) == binary.BigEndian.Uint32(data[24:28]) && claimed != 0 {
		if claimed != pages {
			return nil, bad("page count does not match file length")
		}
		pages = claimed
	}
	if pages <= 0 {
		return nil, bad("truncated page")
	}
	d := &DB{data: data, ps: ps, pages: pages, usable: ps - reserved, names: map[string]table{}}
	if err := d.loadMaster(); err != nil {
		return nil, err
	}
	return d, nil
}

// page returns the bytes of page n, which is 1-based. Page 1 shares its space
// with the 100-byte database header, so its content starts after that header
// (file format, "The Database Header").
func (d *DB) page(n uint32) ([]byte, error) {
	if n < 1 || int(n) > d.pages {
		return nil, bad("page number out of range")
	}
	start := (int(n) - 1) * d.ps
	if n == 1 {
		start = 100
	}
	return d.data[start : int(n)*d.ps], nil
}

// readVarint decodes a big-endian base-128 varint, returning its value and the
// number of bytes consumed. The ninth byte, if reached, contributes all 8 of
// its bits (file format, "A variable-length integer").
func readVarint(b []byte) (uint64, int, error) {
	var v uint64
	for i := 0; i < 9; i++ {
		if i >= len(b) {
			return 0, 0, bad("malformed varint")
		}
		c := b[i]
		if i == 8 {
			return (v << 8) | uint64(c), 9, nil
		}
		v = (v << 7) | uint64(c&127)
		if c < 128 {
			return v, i + 1, nil
		}
	}
	return 0, 0, bad("malformed varint")
}

// loadMaster walks the sqlite_master b-tree, which always roots at page 1, and
// records each table's root page and column names.
func (d *DB) loadMaster() error {
	return d.walk(1, func(rowid int64, payload []byte) error {
		vals, e := record(payload)
		if e != nil {
			return e
		}
		if len(vals) < 5 || vals[0].Text() != "table" {
			return nil
		}
		root := uint32(vals[3].Int64())
		sql := vals[4].Text()
		open := strings.Index(sql, "(")
		close := strings.LastIndex(sql, ")")
		if open < 0 || close < open {
			return bad("malformed schema")
		}
		var cols []string
		pk := -1
		for _, part := range splitCols(sql[open+1 : close]) {
			f := strings.Fields(part)
			if len(f) == 0 || strings.EqualFold(f[0], "constraint") {
				continue
			}
			cols = append(cols, strings.Trim(f[0], "`\"[]"))
			// An INTEGER PRIMARY KEY column is an alias for the rowid and is
			// stored as NULL in the record, so remember which column Rows must
			// substitute ("ROWIDs and the INTEGER PRIMARY KEY").
			if strings.Contains(strings.ToUpper(part), "INTEGER PRIMARY KEY") {
				pk = len(cols) - 1
			}
		}
		d.names[vals[1].Text()] = table{root, cols, pk}
		return nil
	})
}

// splitCols splits a CREATE TABLE column list on top-level commas, ignoring
// commas nested inside parentheses such as those in a type's size arguments.
func splitCols(s string) []string {
	var out []string
	start, depth := 0, 0
	for i, r := range s {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}

// Rows decodes every row of the named table in b-tree order. It returns an
// error wrapping ErrNoSuchTable if the table is not in the schema, or one
// wrapping ErrCorrupt if a page or record fails to decode.
func (d *DB) Rows(name string) ([]Row, error) {
	t, ok := d.names[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNoSuchTable, name)
	}
	var out []Row
	err := d.walk(t.root, func(id int64, p []byte) error {
		v, e := record(p)
		if e != nil {
			return e
		}
		// An INTEGER PRIMARY KEY is stored as NULL in the record; its real
		// value is the cell's rowid ("ROWIDs and the INTEGER PRIMARY KEY").
		if t.pk >= 0 && t.pk < len(v) && v[t.pk].kind == Null {
			v[t.pk] = Value{kind: Int64, i: id}
		}
		out = append(out, Row{append([]string(nil), t.cols...), v})
		return nil
	})
	return out, err
}

// walk visits every cell payload in the table b-tree rooted at the given page,
// in key order, passing each rowid and its reassembled payload to visit.
func (d *DB) walk(root uint32, visit func(int64, []byte) error) error {
	return d.walkSeen(root, visit, map[uint32]bool{})
}

// walkSeen is walk's recursive worker; seen tracks visited pages so that a
// corrupt file with a cyclic b-tree is rejected instead of looping forever.
func (d *DB) walkSeen(n uint32, visit func(int64, []byte) error, seen map[uint32]bool) error {
	if seen[n] {
		return bad("cyclic b-tree")
	}
	seen[n] = true
	p, e := d.page(n)
	if e != nil {
		return e
	}
	if len(p) < 12 {
		return bad("truncated b-tree header")
	}
	// Cell pointers are offsets from the start of the page, but page 1's slice
	// begins after the 100-byte database header, so shift them by that much.
	base := 0
	if n == 1 {
		base = 100
	}
	typ := p[0]
	cnt := int(binary.BigEndian.Uint16(p[3:5]))
	// Interior pages (type 5) carry an extra 4-byte right-most pointer before
	// the cell pointer array (file format, "B-tree Pages").
	ptrStart := 8
	if typ == 5 {
		ptrStart = 12
	}
	if int(binary.BigEndian.Uint16(p[5:7]))-base > len(p) || ptrStart+2*cnt > len(p) {
		return bad(fmt.Sprintf("invalid cell pointers on page %d", n))
	}
	if typ == 5 {
		for i := 0; i < cnt; i++ {
			// A pointer into the page header — or, on page 1, below the database
			// header, which makes the shifted offset negative — is corrupt.
			o := int(binary.BigEndian.Uint16(p[ptrStart+2*i:])) - base
			if o < ptrStart || o+4 > len(p) {
				return bad("cell pointer beyond page")
			}
			if e = d.walkSeen(binary.BigEndian.Uint32(p[o:]), visit, seen); e != nil {
				return e
			}
		}
		return d.walkSeen(binary.BigEndian.Uint32(p[8:12]), visit, seen)
	}
	if typ != 13 {
		return bad("unsupported b-tree page")
	}
	for i := 0; i < cnt; i++ {
		o := int(binary.BigEndian.Uint16(p[ptrStart+2*i:])) - base
		if o < ptrStart || o >= len(p) {
			return bad("cell pointer beyond page")
		}
		sz, nv, e := readVarint(p[o:])
		if e != nil {
			return e
		}
		rid, nr, e := readVarint(p[o+nv:])
		if e != nil {
			return e
		}
		payload := p[o+nv+nr:]
		if sz > uint64(len(d.data)) {
			return bad("payload length exceeds file size")
		}
		full := int(sz)
		local := full
		// Spilled payloads keep a computed prefix on the leaf: with usable size
		// U and payload P, minimum local M = ((U-12)*32/255)-23 and local
		// K = M+((P-M)%(U-4)), falling back to M when K would still exceed the
		// U-35 leaf maximum (file format, "Cell Payload Overflow Pages").
		if local > d.usable-35 {
			m := ((d.usable - 12) * 32 / 255) - 23
			local = m + ((full - m) % (d.usable - 4))
			if local > d.usable-35 {
				local = m
			}
		}
		if local > len(payload) {
			return bad("truncated cell")
		}
		buf := append([]byte{}, payload[:local]...)
		if local < full {
			if local+4 > len(payload) {
				return bad("truncated overflow pointer")
			}
			q := binary.BigEndian.Uint32(payload[local:])
			x, e := d.overflow(q, full-local)
			if e != nil {
				return e
			}
			buf = append(buf, x...)
		}
		if e = visit(int64(rid), buf); e != nil {
			return e
		}
	}
	return nil
}

// overflow follows the overflow page chain starting at page n and returns the
// remaining want bytes of a spilled payload. Each overflow page begins with a
// 4-byte next-page pointer, leaving usable-4 content bytes (file format, "Cell
// Payload Overflow Pages").
func (d *DB) overflow(n uint32, want int) ([]byte, error) {
	seen := map[uint32]bool{}
	out := make([]byte, 0, want)
	for want > 0 {
		// Page 1 is the header page and can never hold overflow content; its
		// slice is short by those 100 bytes, so reading it as an overflow page
		// would run off the end of the page.
		if seen[n] || n <= 1 {
			return nil, bad("cyclic or invalid overflow page")
		}
		seen[n] = true
		p, e := d.page(n)
		if e != nil {
			return nil, e
		}
		take := d.usable - 4
		if take > want {
			take = want
		}
		if 4+take > len(p) {
			return nil, bad("truncated overflow page")
		}
		out = append(out, p[4:4+take]...)
		want -= take
		next := binary.BigEndian.Uint32(p)
		if want == 0 {
			// The page holding the last of the payload ends the chain, so its
			// next-page pointer must be zero.
			if next != 0 {
				return nil, bad("overflow chain outlives its payload")
			}
			break
		}
		n = next
	}
	return out, nil
}

// record decodes one payload in the SQLite record format: a header of serial
// type varints followed by the values themselves (file format, "Record
// Format").
func record(p []byte) ([]Value, error) {
	hs, n, e := readVarint(p)
	if e != nil || hs < uint64(n) || hs > uint64(len(p)) {
		return nil, bad("invalid record header")
	}
	types := []uint64{}
	at := n
	for at < int(hs) {
		x, k, e := readVarint(p[at:])
		if e != nil {
			return nil, e
		}
		types = append(types, x)
		at += k
	}
	if at != int(hs) {
		return nil, bad("invalid record header")
	}
	pos := int(hs)
	out := make([]Value, 0, len(types))
	for _, x := range types {
		v := Value{}
		switch x {
		case 0:
			v.kind = Null
		case 1, 2, 3, 4, 5, 6:
			v.kind = Int64
			// Serial types 1-6 are big-endian twos-complement integers of 1, 2,
			// 3, 4, 6, and 8 bytes respectively.
			nb := []int{0, 1, 2, 3, 4, 6, 8}[int(x)]
			if pos+nb > len(p) {
				return nil, bad("truncated integer")
			}
			for _, b := range p[pos : pos+nb] {
				v.i = (v.i << 8) | int64(b)
			}
			if nb > 0 && p[pos]&128 != 0 {
				v.i -= 1 << uint(nb*8)
			}
			pos += nb
		case 7:
			v.kind = Float64
			if pos+8 > len(p) {
				return nil, bad("truncated float")
			}
			v.f = math.Float64frombits(binary.BigEndian.Uint64(p[pos:]))
			pos += 8
		case 8, 9:
			// Serial types 8 and 9 are the constants 0 and 1; they occupy no
			// bytes in the value area.
			v.kind = Int64
			v.i = int64(x - 8)
		default:
			// Even serial types >= 12 are blobs of (x-12)/2 bytes; odd types
			// >= 13 are text of (x-13)/2 bytes.
			var n int
			if x >= 12 && x%2 == 0 {
				v.kind = Blob
				n = int((x - 12) / 2)
			} else if x >= 13 {
				v.kind = Text
				n = int((x - 13) / 2)
			} else {
				return nil, bad("unknown serial type")
			}
			// A nine-byte serial type can encode a length near the top of the
			// uint64 range, so compare against the bytes remaining instead of
			// adding an attacker-chosen length to the read position.
			if n > len(p)-pos {
				return nil, bad("truncated value")
			}
			if v.kind == Blob {
				v.b = append([]byte{}, p[pos:pos+n]...)
			} else {
				v.s = string(p[pos : pos+n])
			}
			pos += n
		}
		out = append(out, v)
	}
	// The values a record declares must account for its whole payload; leftover
	// bytes mean the header and the body disagree.
	if pos != len(p) {
		return nil, bad("record does not fill its payload")
	}
	return out, nil
}
