//go:build windows

package osfs

import (
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// openShared opens path read-only with a share mode that includes
// FILE_SHARE_DELETE. Go's os.Open uses only FILE_SHARE_READ |
// FILE_SHARE_WRITE (golang/go#32088), so for as long as such a handle is
// open no other process can rename-replace or delete the file — exactly
// how games commit saves (write a temp file, then MoveFileEx over the old
// one). The daemon reads live save files while the game process is
// running; an unshared read window makes the game's own save fail with a
// sharing violation (observed as Palworld's "could not save" whenever the
// daemon read a save member mid-autosave).
func openShared(path string) (*os.File, error) {
	pathp, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	h, err := windows.CreateFile(
		pathp,
		windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(h), path), nil
}

// ReadFileShared reads the file at path without blocking a concurrent
// rename-replace or delete of it by another process (see openShared).
func ReadFileShared(path string) ([]byte, error) {
	f, err := openShared(filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var size int64
	if info, statErr := f.Stat(); statErr == nil {
		size = info.Size()
	}
	buf := make([]byte, 0, size+1)
	for {
		n, readErr := f.Read(buf[len(buf):cap(buf)])
		buf = buf[:len(buf)+n]
		if readErr != nil {
			if readErr == io.EOF {
				return buf, nil
			}
			return nil, &os.PathError{Op: "read", Path: path, Err: readErr}
		}
		if len(buf) == cap(buf) {
			buf = append(buf, 0)[:len(buf)]
		}
	}
}
