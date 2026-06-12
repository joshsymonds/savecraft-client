// Package sav parses Satisfactory .sav save files.
//
// Format reference: https://github.com/moritz-h/satisfactory-3d-map/blob/master/docs/SATISFACTORY_SAVE.md
// Field order and version gates cross-checked against FGSaveManagerInterface.h
// via etothepii4/satisfactory-file-parser (MIT).
package sav

import (
	"fmt"
	"io"
	"time"
)

// Version support floor: game release 1.0.
//
// SaveHeaderVersion (FGSaveManagerInterface.h): 13 = Update 8 + 1.0, 14 = 1.1+.
// SaveVersion (FGSaveCustomVersion.h): 46 = "Version1", the 1.0 release marker.
const (
	MinHeaderVersion = 13
	MinSaveVersion   = 46

	// headerVersionAddedSaveName: header v14 (game 1.1) inserted SaveName
	// between BuildVersion and MapName.
	headerVersionAddedSaveName = 14
)

// ueEpochTicks is the Unix epoch expressed in UE FDateTime ticks
// (100ns intervals since 0001-01-01 00:00:00 UTC).
const ueEpochTicks = 621355968000000000

// ticksPerMillisecond converts UE FDateTime ticks (100ns) to milliseconds.
const ticksPerMillisecond = 10000

// UnsupportedVersionError reports a save older than game version 1.0.
type UnsupportedVersionError struct {
	HeaderVersion int32
	SaveVersion   int32
}

func (e *UnsupportedVersionError) Error() string {
	return fmt.Sprintf(
		"unsupported save version: header version %d, save version %d "+
			"(need header >= %d and save >= %d, i.e. a save from game version 1.0 or later)",
		e.HeaderVersion, e.SaveVersion, MinHeaderVersion, MinSaveVersion,
	)
}

// Header is the uncompressed .sav file header.
type Header struct {
	HeaderVersion    int32
	SaveVersion      int32
	BuildVersion     int32
	SaveName         string // header version >= 14 (game 1.1+) only
	MapName          string
	MapOptions       string
	SessionName      string
	PlayDuration     time.Duration
	SaveTime         time.Time
	Visibility       byte
	EditorVersion    int32
	ModMetadata      string // JSON blob; empty for unmodded saves
	Modded           bool
	SaveIdentifier   string
	PartitionedWorld bool
	CreativeMode     bool
}

// ParseHeader reads the uncompressed header from the start of a .sav stream.
// It consumes exactly the header bytes, leaving the reader positioned at the
// first compressed body chunk.
//
// Returns *UnsupportedVersionError for pre-1.0 saves. Header versions newer
// than 14 parse on a best-effort basis: fields are only ever appended, so
// the known prefix remains valid (body parsing has its own version gates).
func ParseHeader(src io.Reader) (*Header, error) {
	r := newReader(src)
	return parseHeader(r)
}

func parseHeader(r *reader) (*Header, error) {
	h := &Header{}

	var err error
	if h.HeaderVersion, err = r.int32("header version"); err != nil {
		return nil, err
	}
	if h.SaveVersion, err = r.int32("save version"); err != nil {
		return nil, err
	}
	if h.HeaderVersion < MinHeaderVersion || h.SaveVersion < MinSaveVersion {
		return nil, &UnsupportedVersionError{HeaderVersion: h.HeaderVersion, SaveVersion: h.SaveVersion}
	}
	if h.BuildVersion, err = r.int32("build version"); err != nil {
		return nil, err
	}

	if h.HeaderVersion >= headerVersionAddedSaveName {
		if h.SaveName, err = r.fstring("save name"); err != nil {
			return nil, err
		}
	}
	if h.MapName, err = r.fstring("map name"); err != nil {
		return nil, err
	}
	if h.MapOptions, err = r.fstring("map options"); err != nil {
		return nil, err
	}
	if h.SessionName, err = r.fstring("session name"); err != nil {
		return nil, err
	}

	playSeconds, err := r.int32("play duration")
	if err != nil {
		return nil, err
	}
	h.PlayDuration = time.Duration(playSeconds) * time.Second

	ticks, err := r.int64("save timestamp")
	if err != nil {
		return nil, err
	}
	h.SaveTime = time.UnixMilli((ticks - ueEpochTicks) / ticksPerMillisecond).UTC()

	if h.Visibility, err = r.byte("session visibility"); err != nil {
		return nil, err
	}
	if h.EditorVersion, err = r.int32("editor object version"); err != nil {
		return nil, err
	}
	if h.ModMetadata, err = r.fstring("mod metadata"); err != nil {
		return nil, err
	}

	modded, err := r.int32("modded flag")
	if err != nil {
		return nil, err
	}
	h.Modded = modded != 0

	if h.SaveIdentifier, err = r.fstring("save identifier"); err != nil {
		return nil, err
	}

	partitioned, err := r.int32("partitioned world flag")
	if err != nil {
		return nil, err
	}
	h.PartitionedWorld = partitioned == 1

	// FMD5Hash: validity int32, then 16 hash bytes only when valid.
	md5Valid, err := r.int32("md5 validity")
	if err != nil {
		return nil, err
	}
	if md5Valid == 1 {
		if discardErr := r.discard(16, "md5 hash"); discardErr != nil {
			return nil, discardErr
		}
	}

	creative, err := r.int32("creative mode flag")
	if err != nil {
		return nil, err
	}
	h.CreativeMode = creative == 1

	return h, nil
}
