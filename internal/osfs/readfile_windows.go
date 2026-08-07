//go:build windows

package osfs

import (
	"os"
	"path/filepath"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows makes "read a file the game is about to replace" genuinely hard:
//
//   - Go's os.ReadFile opens with only FILE_SHARE_READ | FILE_SHARE_WRITE
//     (golang/go#32088), so the game's save commit — write Level.sav.tmp,
//     rename it over Level.sav — fails with a sharing violation for as
//     long as the read handle is open. Observed live as Palworld's "could
//     not save" whenever the daemon read a save member mid-autosave.
//   - Adding FILE_SHARE_DELETE is necessary but NOT sufficient: it lets
//     the game delete the file under us, but a rename-OVER a file with
//     any open handle still fails (ACCESS_DENIED) — proven by this
//     package's own Windows CI tests.
//
// The full fix is the mechanism Windows Search and AV scanners use: take
// a Read-Handle (RH) oplock (FSCTL_REQUEST_OPLOCK with CACHE_READ |
// CACHE_HANDLE) on the read handle. When the game's rename/delete
// collides, the filesystem breaks the oplock and HOLDS the game's
// operation until our handle closes — and our reads are short, so the
// game waits a few milliseconds instead of erroring. If the oplock can't
// be granted (non-NTFS, exotic filters), we degrade to the plain
// share-everything handle, which is still strictly better than stdlib.
const (
	fsctlRequestOplock            = 0x00090240
	oplockLevelCacheRead          = 0x00000001
	oplockLevelCacheHandle        = 0x00000004
	requestOplockInputFlagRequest = 0x00000001
)

type requestOplockInputBuffer struct {
	StructureVersion     uint16
	StructureLength      uint16
	RequestedOplockLevel uint32
	Flags                uint32
}

type requestOplockOutputBuffer struct {
	StructureVersion    uint16
	StructureLength     uint16
	OriginalOplockLevel uint32
	NewOplockLevel      uint32
	Flags               uint32
	AccessMode          uint32
	ShareMode           uint16
	pad                 uint16
}

// sharedReader is an open read handle that yields to writers: full share
// mode plus a best-effort RH oplock. Split from ReadFileShared so the
// Windows CI tests can hold a read open while asserting a concurrent
// rename-replace succeeds.
type sharedReader struct {
	handle windows.Handle
	// Oplock plumbing. The kernel writes into oplockOut and oplockOv when
	// the oplock breaks, so both must stay alive until the handle closes.
	oplockEvent windows.Handle
	oplockIn    *requestOplockInputBuffer
	oplockOut   *requestOplockOutputBuffer
	oplockOv    *windows.Overlapped
}

func openShared(path string) (*sharedReader, error) {
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
		windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OVERLAPPED,
		0,
	)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	r := &sharedReader{handle: h}
	r.requestOplock()
	return r, nil
}

// requestOplock takes a best-effort RH oplock on the handle. On success
// the FSCTL completes asynchronously (ERROR_IO_PENDING) and stays pending
// until the oplock breaks; we never need to wait on it — closing the
// handle is our break acknowledgement, and every reader closes within
// milliseconds. Any other outcome (unsupported filesystem, oplock not
// granted) leaves the reader working without one.
func (r *sharedReader) requestOplock() {
	event, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return
	}
	in := &requestOplockInputBuffer{
		StructureVersion:     1,
		StructureLength:      uint16(unsafe.Sizeof(requestOplockInputBuffer{})),
		RequestedOplockLevel: oplockLevelCacheRead | oplockLevelCacheHandle,
		Flags:                requestOplockInputFlagRequest,
	}
	out := &requestOplockOutputBuffer{}
	ov := &windows.Overlapped{HEvent: event}
	err = windows.DeviceIoControl(
		r.handle,
		fsctlRequestOplock,
		(*byte)(unsafe.Pointer(in)),
		uint32(unsafe.Sizeof(*in)),
		(*byte)(unsafe.Pointer(out)),
		uint32(unsafe.Sizeof(*out)),
		nil,
		ov,
	)
	if err != windows.ERROR_IO_PENDING {
		// Immediate completion (or refusal) means no oplock is held.
		windows.CloseHandle(event)
		return
	}
	r.oplockEvent = event
	r.oplockIn = in
	r.oplockOut = out
	r.oplockOv = ov
}

// ReadAll reads the whole file via explicit overlapped reads (the handle
// is FILE_FLAG_OVERLAPPED, so plain synchronous ReadFile calls are not an
// option).
func (r *sharedReader) ReadAll(path string) ([]byte, error) {
	var size int64
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(r.handle, &info); err == nil {
		size = int64(info.FileSizeHigh)<<32 | int64(info.FileSizeLow)
	}
	event, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return nil, &os.PathError{Op: "read", Path: path, Err: err}
	}
	defer windows.CloseHandle(event)

	buf := make([]byte, 0, size+1)
	chunk := make([]byte, 1<<20)
	var offset int64
	for {
		ov := windows.Overlapped{
			Offset:     uint32(offset & 0xFFFFFFFF),
			OffsetHigh: uint32(offset >> 32),
			HEvent:     event,
		}
		var done uint32
		err := windows.ReadFile(r.handle, chunk, &done, &ov)
		if err == windows.ERROR_IO_PENDING {
			err = windows.GetOverlappedResult(r.handle, &ov, &done, true)
		}
		if err != nil {
			if err == windows.ERROR_HANDLE_EOF {
				return buf, nil
			}
			return nil, &os.PathError{Op: "read", Path: path, Err: err}
		}
		buf = append(buf, chunk[:done]...)
		offset += int64(done)
		if done == 0 {
			return buf, nil
		}
	}
}

// Close releases the handle (which also acknowledges any in-progress
// oplock break, letting a waiting writer proceed) and then the oplock
// plumbing that had to outlive it.
func (r *sharedReader) Close() error {
	err := windows.CloseHandle(r.handle)
	if r.oplockEvent != 0 {
		windows.CloseHandle(r.oplockEvent)
	}
	// The kernel may write into these until the handle is gone.
	runtime.KeepAlive(r.oplockIn)
	runtime.KeepAlive(r.oplockOut)
	runtime.KeepAlive(r.oplockOv)
	return err
}

// ReadFileShared reads the file at path without making a concurrent
// rename-replace or delete by another process fail (see the package
// comment above: full share mode + RH oplock; writers wait out our
// milliseconds-long read instead of erroring).
func ReadFileShared(path string) ([]byte, error) {
	path = filepath.Clean(path)
	r, err := openShared(path)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return r.ReadAll(path)
}
