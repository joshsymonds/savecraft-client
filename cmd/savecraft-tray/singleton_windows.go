//go:build windows

package main

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	kernel32        = syscall.NewLazyDLL("kernel32.dll")
	createMutexW    = kernel32.NewProc("CreateMutexW")
	errAlreadyExist = syscall.Errno(183) // ERROR_ALREADY_EXISTS
)

// acquireSingleton attempts to acquire a system-wide named mutex.
// Returns a release function on success, or an error if another instance
// already holds the mutex.
func acquireSingleton() (release func(), err error) {
	name, nameErr := syscall.UTF16PtrFromString("Global\\Savecraft_Tray")
	if nameErr != nil {
		return nil, fmt.Errorf("encode mutex name: %w", nameErr)
	}

	handle, _, callErr := createMutexW.Call(0, 0, uintptr(unsafe.Pointer(name)))
	if handle == 0 {
		return nil, fmt.Errorf("CreateMutexW: %w", callErr)
	}

	if callErr == errAlreadyExist {
		syscall.CloseHandle(syscall.Handle(handle))

		return nil, fmt.Errorf("another savecraft-tray instance is already running")
	}

	return func() {
		syscall.CloseHandle(syscall.Handle(handle))
	}, nil
}
