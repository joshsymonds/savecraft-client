package d2s

import (
	"encoding/binary"
	"testing"
)

func TestSharedStashRealm(t *testing.T) {
	tests := []struct {
		name          string
		want          byte
		kind, version uint32
	}{
		{"legacy hardcore", RealmClassic, 0, 0x60},
		{"lod hardcore", RealmLoD, 0, 0x61},
		{"lod softcore upper boundary", RealmLoD, 1, 0x68},
		{"modern hardcore", RealmRotW, 0, 0x69},
		{"legacy softcore", RealmClassic, 1, 0x60},
		{"modern softcore", RealmRotW, 1, 0x69},
		{"rotw kind", RealmRotW, 2, 0x60},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := make([]byte, stashHeaderSize)
			binary.LittleEndian.PutUint32(data[0:4], stashMagic)
			binary.LittleEndian.PutUint32(data[4:8], tt.kind)
			binary.LittleEndian.PutUint32(data[8:12], tt.version)
			binary.LittleEndian.PutUint32(data[16:20], stashHeaderSize)
			stash, err := ParseStash(data)
			if err != nil {
				t.Fatal(err)
			}
			if got := stash.Realm(); got != tt.want {
				t.Errorf("Realm() = %d, want %d", got, tt.want)
			}
		})
	}
}
