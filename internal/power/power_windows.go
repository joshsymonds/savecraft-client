//go:build windows

// Package power provides platform-specific power event monitoring.
// On Windows, it creates a hidden message-only window to receive
// WM_POWERBROADCAST events and signals on resume from sleep/hibernate.
package power

import (
	"context"
	"runtime"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	wmPowerBroadcast      = 0x0218
	wmClose               = 0x0010
	pbtAPMResumeAutomatic = 0x0012

	csHRedraw = 0x0002
	csVRedraw = 0x0001
)

var (
	user32             = windows.NewLazySystemDLL("user32.dll")
	procRegisterClassW = user32.NewProc("RegisterClassExW")
	procCreateWindowEx = user32.NewProc("CreateWindowExW")
	procDestroyWindow  = user32.NewProc("DestroyWindow")
	procGetMessage     = user32.NewProc("GetMessageW")
	procDefWindowProc  = user32.NewProc("DefWindowProcW")
	procPostMessage    = user32.NewProc("PostMessageW")
	procPostQuitMsg    = user32.NewProc("PostQuitMessage")
)

// hwndMessage is the HWND_MESSAGE constant for message-only windows.
const hwndMessage = ^uintptr(2) // (HWND)(-3) in Win32

type wndClassExW struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   windows.Handle
	Icon       windows.Handle
	Cursor     windows.Handle
	Background windows.Handle
	MenuName   *uint16
	ClassName  *uint16
	IconSm     windows.Handle
}

type msg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      struct{ X, Y int32 }
}

// windowChannels maps window handles to their resume channels.
// wndProc is a bare function (Windows callback) and cannot capture state
// via closures, so we use a synchronized map to route events to the
// correct channel.
var (
	windowChMu sync.RWMutex                    //nolint:gochecknoglobals // required by wndProc callback
	windowChs  = map[uintptr]chan<- struct{}{} //nolint:gochecknoglobals // required by wndProc callback
)

// Monitor starts listening for Windows power events in a background goroutine.
// It returns a channel that signals on resume from sleep/hibernate, and a stop
// function that tears down the hidden window and message pump.
//
// The channel is buffered (size 1) so a signal is never lost but rapid
// resume events don't pile up.
func Monitor(ctx context.Context) (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)

	ready := make(chan uintptr, 1)
	done := make(chan struct{})

	go func() {
		// The message pump must run on the same OS thread that created the window.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		hwnd := createMessageWindow()
		if hwnd == 0 {
			ready <- 0
			close(done)
			return
		}

		// Register the channel before signaling ready.
		windowChMu.Lock()
		windowChs[hwnd] = ch
		windowChMu.Unlock()

		ready <- hwnd

		go func() {
			select {
			case <-ctx.Done():
			case <-done:
				return
			}
			// PostMessageW targets the window handle and is thread-safe.
			// The wndProc handles WM_CLOSE by calling PostQuitMessage on
			// the pump thread, which correctly terminates GetMessage.
			procPostMessage.Call(hwnd, wmClose, 0, 0) //nolint:errcheck // fire and forget
		}()

		messagePump()

		windowChMu.Lock()
		delete(windowChs, hwnd)
		windowChMu.Unlock()

		procDestroyWindow.Call(hwnd) //nolint:errcheck // best effort
		close(done)
	}()

	hwnd := <-ready

	stop := func() {
		if hwnd != 0 {
			// PostMessageW is thread-safe — sends WM_CLOSE to the pump thread.
			procPostMessage.Call(hwnd, wmClose, 0, 0) //nolint:errcheck // fire and forget
		}
		<-done
	}

	return ch, stop
}

func createMessageWindow() uintptr {
	className, _ := windows.UTF16PtrFromString("SavecraftPower")

	wc := wndClassExW{
		Style:     csHRedraw | csVRedraw,
		WndProc:   windows.NewCallback(wndProc),
		ClassName: className,
	}
	wc.Size = uint32(unsafe.Sizeof(wc))

	procRegisterClassW.Call(uintptr(unsafe.Pointer(&wc))) //nolint:errcheck // if it fails, CreateWindowEx will too

	hwnd, _, _ := procCreateWindowEx.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		0, // no window title
		0, // no style (message-only)
		0, 0, 0, 0,
		hwndMessage, // parent = HWND_MESSAGE
		0, 0, 0,
	)

	return hwnd
}

func wndProc(hwnd uintptr, umsg uint32, wparam, lparam uintptr) uintptr {
	switch {
	case umsg == wmPowerBroadcast && wparam == pbtAPMResumeAutomatic:
		windowChMu.RLock()
		ch := windowChs[hwnd]
		windowChMu.RUnlock()

		if ch != nil {
			select {
			case ch <- struct{}{}:
			default:
			}
		}
	case umsg == wmClose:
		// WM_CLOSE is delivered on the pump thread via PostMessageW.
		// Call PostQuitMessage here so it targets the correct thread's
		// message queue, which causes GetMessage to return 0.
		procPostQuitMsg.Call(0) //nolint:errcheck // terminates message pump
		return 0
	}

	ret, _, _ := procDefWindowProc.Call(hwnd, uintptr(umsg), wparam, lparam)

	return ret
}

func messagePump() {
	var m msg
	for {
		ret, _, _ := procGetMessage.Call(
			uintptr(unsafe.Pointer(&m)),
			0, 0, 0,
		)
		// GetMessage returns 0 for WM_QUIT, -1 for error.
		if int32(ret) <= 0 {
			return
		}
	}
}
