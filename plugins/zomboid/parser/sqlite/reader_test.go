package sqlite

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"reflect"
	"testing"
)

func TestFixtureLocalPlayers(t *testing.T) {
	b, err := os.ReadFile("../testdata/tutorial-42.20.2/players.db")
	if err != nil {
		t.Fatal(err)
	}
	db, err := Open(b)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := db.Rows("localPlayers")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || len(rows[0].Values) != 10 {
		t.Fatalf("got %d rows", len(rows))
	}
	if rows[0].Values[0].Int64() != 1 || rows[0].Values[1].Text() != "Jane Doe" || rows[0].Values[2].Int64() != 22 {
		t.Fatalf("unexpected row: %#v", rows[0])
	}
	if len(rows[0].Values[8].Blob()) != 4312 {
		t.Fatalf("blob length %d", len(rows[0].Values[8].Blob()))
	}
	if !reflect.DeepEqual(rows[0].Columns, []string{"id", "name", "wx", "wy", "x", "y", "z", "worldversion", "data", "isDead"}) {
		t.Fatalf("columns %v", rows[0].Columns)
	}
	if rows[0].Values[3].Int64() != 18 || rows[0].Values[4].Float64() != 178.56251525878906 || rows[0].Values[5].Float64() != 147.0225372314453 || rows[0].Values[6].Float64() != 0 || rows[0].Values[7].Int64() != 249 || rows[0].Values[9].Int64() != 0 {
		t.Fatalf("values %#v", rows[0].Values)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(rows[0].Values[8].Blob())); got != "de011b2d120dab1a945ddf860a1bdc42537c3a1f377279d3c9f78a78557ccc70" {
		t.Fatal(got)
	}
	if r, e := db.Rows("networkPlayers"); e != nil || len(r) != 0 {
		t.Fatalf("network %v %v", len(r), e)
	}
	if !reflect.DeepEqual(db.Tables(), []string{"localPlayers", "networkPlayers"}) {
		t.Fatalf("tables %v", db.Tables())
	}
}

func TestMultipage(t *testing.T) {
	b, err := os.ReadFile("testdata/multipage.db")
	if err != nil {
		t.Fatal(err)
	}
	db, err := Open(b)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := db.Rows("t")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 320 {
		t.Fatalf("unexpected multipage rows: %d", len(rows))
	}
	for i, row := range rows {
		if row.Values[0].Kind() != Int64 || row.Values[0].Int64() != int64(i) {
			t.Fatalf("row %d id: %#v", i, row.Values[0])
		}
		wantName := "row" + fmt.Sprint(i)
		if i%10 == 0 {
			wantName = ""
		}
		if row.Values[1].Kind() != Text || row.Values[1].Text() != wantName {
			t.Fatalf("row %d name: %#v", i, row.Values[1])
		}
		wantBlob := []byte{byte(i)}
		if i%11 == 0 {
			wantBlob = []byte{}
		}
		if i == 100 {
			wantBlob = bytes.Repeat([]byte("x"), 3000)
		}
		if i == 200 {
			wantBlob = bytes.Repeat([]byte("y"), 20000)
		}
		if row.Values[2].Kind() != Blob || !bytes.Equal(row.Values[2].Blob(), wantBlob) {
			t.Fatalf("row %d blob length %d", i, len(row.Values[2].Blob()))
		}
		if i%7 == 0 {
			if row.Values[3].Kind() != Null {
				t.Fatalf("row %d f: %#v", i, row.Values[3])
			}
		} else if i%3 == 0 {
			if row.Values[3].Kind() != Int64 || row.Values[3].Int64() != int64(i/3) {
				t.Fatalf("row %d f: %#v", i, row.Values[3])
			}
		} else if row.Values[3].Kind() != Float64 || row.Values[3].Float64() != float64(i)/3 {
			t.Fatalf("row %d f: %#v", i, row.Values[3])
		}
		if i%5 == 0 {
			if row.Values[4].Kind() != Null {
				t.Fatalf("row %d n: %#v", i, row.Values[4])
			}
		} else {
			wantInt := -int64(i)
			if i == 301 {
				wantInt = 1 << 40
			}
			if i == 302 {
				wantInt = 1 << 55
			}
			if row.Values[4].Kind() != Int64 || row.Values[4].Int64() != wantInt {
				t.Fatalf("row %d n: %#v", i, row.Values[4])
			}
		}
	}
}

func TestMalformedAndDeterministic(t *testing.T) {
	b, err := os.ReadFile("../testdata/tutorial-42.20.2/players.db")
	if err != nil {
		t.Fatal(err)
	}
	d1, e := Open(b)
	if e != nil {
		t.Fatal(e)
	}
	d2, e := Open(b)
	if e != nil {
		t.Fatal(e)
	}
	a, e := d1.Rows("localPlayers")
	if e != nil {
		t.Fatal(e)
	}
	c, e := d2.Rows("localPlayers")
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(a, c) {
		t.Fatal("nondeterministic")
	}
	if _, e := d1.Rows("missing"); !errors.Is(e, ErrNoSuchTable) {
		t.Fatal(e)
	}
	for _, m := range []struct {
		n int
		v byte
	}{{0, 'X'}, {16, 3}, {59, 0}} {
		x := append([]byte{}, b...)
		x[m.n] = m.v
		if _, e := Open(x); !errors.Is(e, ErrCorrupt) {
			t.Errorf("case %d: %v", m.n, e)
		}
	}
	for n := 4096; n < len(b); n += 4096 {
		if _, e := Open(b[:n]); e == nil {
			t.Errorf("truncation %d accepted", n)
		}
	}
	if _, e := Open(b[:2048]); !errors.Is(e, ErrCorrupt) {
		t.Fatalf("half-page truncation: %v", e)
	}
	pageSize := int(binary.BigEndian.Uint16(b[16:18]))
	if pageSize == 1 {
		pageSize = 65536
	}
	pages := len(b) / pageSize
	x := append([]byte(nil), b...)
	binary.BigEndian.PutUint32(x[28:32], uint32(pages-1))
	if _, e := Open(x); !errors.Is(e, ErrCorrupt) {
		t.Fatalf("valid page count smaller than file: %v", e)
	}
	binary.BigEndian.PutUint32(x[92:96], 0)
	if _, e := Open(x); e != nil {
		t.Fatalf("stale page count should be ignored: %v", e)
	}
	x = append([]byte(nil), b...)
	x[18], x[19] = 2, 2
	if _, e := Open(x); e != nil {
		t.Fatalf("WAL format versions are valid: %v", e)
	}
}

func TestCorruptCellsAndOverflow(t *testing.T) {
	b, err := os.ReadFile("testdata/multipage.db")
	if err != nil {
		t.Fatal(err)
	}
	db, err := Open(b)
	if err != nil {
		t.Fatal(err)
	}
	cell, payload, err := tableCell(db, "t", 200)
	if err != nil {
		t.Fatal(err)
	}
	_, nv, err := readVarint(b[cell:])
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = readVarint(b[cell+nv:])
	if err != nil {
		t.Fatal(err)
	}
	full, _, err := readVarint(b[cell:])
	if err != nil {
		t.Fatal(err)
	}
	local := localPayload(db.usable, int(full))
	overflow := payload + local
	first := binary.BigEndian.Uint32(b[overflow:])
	last := lastOverflowPage(t, db, first, int(full)-local)

	for _, tc := range []struct {
		name  string
		patch func([]byte)
	}{
		{"overflow page zero", func(x []byte) { binary.BigEndian.PutUint32(x[overflow:], 0) }},
		{"overflow page beyond file", func(x []byte) { binary.BigEndian.PutUint32(x[overflow:], uint32(db.pages+1)) }},
		{"overflow page one", func(x []byte) { binary.BigEndian.PutUint32(x[overflow:], 1) }},
		{"unterminated overflow chain", func(x []byte) {
			binary.BigEndian.PutUint32(x[(int(last)-1)*db.ps:], first)
		}},
		{"cyclic overflow", func(x []byte) { binary.BigEndian.PutUint32(x[(int(first)-1)*db.ps:], first) }},
		{"unknown serial type", func(x []byte) { x[payload+1] = 10 }},
		{"oversized payload varint", func(x []byte) {
			for i := 0; i < 9; i++ {
				x[cell+i] = 0xff
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			x := append([]byte(nil), b...)
			tc.patch(x)
			d, err := Open(x)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = d.Rows("t"); !errors.Is(err, ErrCorrupt) {
				t.Fatalf("error = %v", err)
			}
		})
	}

	x := append([]byte(nil), b...)
	root := db.names["t"].root
	ptr := (int(root)-1)*db.ps + 12
	binary.BigEndian.PutUint16(x[ptr:], uint16(db.ps-1))
	for i := 0; i < 8; i++ {
		x[(int(root)-1)*db.ps+db.ps-8+i] = 0x80
	}
	d, err := Open(x)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = d.Rows("t"); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("malformed varint: %v", err)
	}
}

func TestInvalidCellPointers(t *testing.T) {
	b, err := os.ReadFile("testdata/multipage.db")
	if err != nil {
		t.Fatal(err)
	}
	db, err := Open(b)
	if err != nil {
		t.Fatal(err)
	}
	root := db.names["t"].root
	for _, pointer := range []uint16{0, uint16(db.ps)} {
		x := append([]byte(nil), b...)
		binary.BigEndian.PutUint16(x[(int(root)-1)*db.ps+12:], pointer)
		d, err := Open(x)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = d.Rows("t"); !errors.Is(err, ErrCorrupt) {
			t.Fatalf("pointer %d: %v", pointer, err)
		}
	}
}

func TestOverflowRejectsPageOne(t *testing.T) {
	b, err := os.ReadFile("testdata/multipage.db")
	if err != nil {
		t.Fatal(err)
	}
	db, err := Open(b)
	if err != nil {
		t.Fatal(err)
	}
	// Page 1's slice stops 100 bytes short because of the database header, so
	// a full content read from it silently spills into page 2 rather than
	// failing on the slice bound.
	if _, err := db.overflow(1, db.usable-4); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("page 1 as overflow target: %v", err)
	}
}

func TestPageOneInteriorCellPointer(t *testing.T) {
	b, err := os.ReadFile("testdata/multipage.db")
	if err != nil {
		t.Fatal(err)
	}
	x := append([]byte(nil), b...)
	// Page 1's cell pointers are shifted down by the 100-byte database header,
	// so a pointer below 100 computes a negative page offset. Only an interior
	// page can root at page 1 and still reach the interior cell loop, so the
	// page type is flipped to 5 as well.
	x[100] = 5
	binary.BigEndian.PutUint16(x[112:], 0)
	if _, err := Open(x); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("interior cell pointer under the header: %v", err)
	}
}

func TestRecordRejectsOversizedAndTrailingPayload(t *testing.T) {
	t.Run("oversized serial type", func(t *testing.T) {
		// A nine-byte serial-type varint of all 0xff decodes to 2^64-1: a text
		// value of 2^63-7 bytes, whose length added to the read position would
		// wrap around int and slip past a bounds check.
		p := append([]byte{10}, bytes.Repeat([]byte{0xff}, 9)...)
		if _, err := record(p); !errors.Is(err, ErrCorrupt) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("trailing payload byte", func(t *testing.T) {
		// A one-byte integer whose record declares a payload byte it never spends.
		if _, err := record([]byte{2, 1, 5, 0}); !errors.Is(err, ErrCorrupt) {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestUsableSizeMinimum(t *testing.T) {
	if _, err := Open(emptyDB(512, 33)); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("usable size 479: %v", err)
	}
	if _, err := Open(emptyDB(512, 32)); err != nil {
		t.Fatalf("usable size 480: %v", err)
	}
}

func TestRowColumnsAreIndependent(t *testing.T) {
	b, err := os.ReadFile("testdata/multipage.db")
	if err != nil {
		t.Fatal(err)
	}
	db, err := Open(b)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := db.Rows("t")
	if err != nil {
		t.Fatal(err)
	}
	rows[0].Columns[0] = "clobbered"
	if rows[1].Columns[0] != "id" {
		t.Fatalf("sibling row columns %v", rows[1].Columns)
	}
	again, err := db.Rows("t")
	if err != nil {
		t.Fatal(err)
	}
	if again[0].Columns[0] != "id" {
		t.Fatalf("later call columns %v", again[0].Columns)
	}
}

// emptyDB builds a one-page database whose sqlite_master b-tree holds no cells,
// so that Open's header checks are the only thing standing between the given
// page size and reserved-byte count and a successfully opened DB.
func emptyDB(ps, reserved int) []byte {
	b := make([]byte, ps)
	copy(b, "SQLite format 3\x00")
	binary.BigEndian.PutUint16(b[16:18], uint16(ps))
	b[18], b[19] = 1, 1
	b[20] = byte(reserved)
	binary.BigEndian.PutUint32(b[24:28], 1)
	binary.BigEndian.PutUint32(b[28:32], 1)
	binary.BigEndian.PutUint32(b[56:60], 1)
	binary.BigEndian.PutUint32(b[92:96], 1)
	b[100] = 13
	binary.BigEndian.PutUint16(b[105:107], uint16(ps-reserved))
	return b
}

// lastOverflowPage follows the overflow chain that starts at page first for the
// given number of spilled bytes and returns the page that ends it.
func lastOverflowPage(t *testing.T, db *DB, first uint32, want int) uint32 {
	t.Helper()
	for n := first; ; {
		if take := db.usable - 4; take < want {
			want -= take
		} else {
			return n
		}
		p, err := db.page(n)
		if err != nil {
			t.Fatal(err)
		}
		n = binary.BigEndian.Uint32(p)
	}
}

func tableCell(db *DB, name string, target int64) (int, int, error) {
	n := db.names[name].root
	for {
		p, err := db.page(n)
		if err != nil {
			return 0, 0, err
		}
		base, ptrStart := 0, 8
		if n == 1 {
			base = 100
		}
		if p[0] == 5 {
			ptrStart = 12
			count := int(binary.BigEndian.Uint16(p[3:5]))
			for i := 0; i < count; i++ {
				o := int(binary.BigEndian.Uint16(p[ptrStart+2*i:])) - base
				key, _, err := readVarint(p[o+4:])
				if err != nil {
					return 0, 0, err
				}
				if target <= int64(key) {
					n = binary.BigEndian.Uint32(p[o:])
					goto next
				}
			}
			n = binary.BigEndian.Uint32(p[8:12])
			goto next
		}
		for i := 0; i < int(binary.BigEndian.Uint16(p[3:5])); i++ {
			o := int(binary.BigEndian.Uint16(p[ptrStart+2*i:])) - base
			_, nv, err := readVarint(p[o:])
			if err != nil {
				return 0, 0, err
			}
			id, nr, err := readVarint(p[o+nv:])
			if err != nil {
				return 0, 0, err
			}
			if int64(id) == target {
				cell := (int(n)-1)*db.ps + o + base
				return cell, cell + nv + nr, nil
			}
		}
		return 0, 0, fmt.Errorf("row %d not found", target)
	next:
	}
}

func localPayload(usable, total int) int {
	if total <= usable-35 {
		return total
	}
	min := ((usable-12)*32)/255 - 23
	local := min + (total-min)%(usable-4)
	if local > usable-35 {
		return min
	}
	return local
}
