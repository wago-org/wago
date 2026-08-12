//go:build windows

package atomicfile

import (
	"errors"
	"syscall"
	"time"
	"unsafe"
)

const (
	moveFileReplaceExisting      = 0x1
	moveFileWriteThrough         = 0x8
	windowsErrorSharingViolation = syscall.Errno(32)
	windowsErrorLockViolation    = syscall.Errno(33)
	windowsReplaceRetryDelay     = 10 * time.Millisecond
	windowsReplaceRetryTimeout   = 2 * time.Second
)

var moveFileEx = syscall.NewLazyDLL("kernel32.dll").NewProc("MoveFileExW")

func replaceExisting(source, destination string) error {
	sourcePointer, err := syscall.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	destinationPointer, err := syscall.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(windowsReplaceRetryTimeout)
	for {
		result, _, callErr := moveFileEx.Call(
			uintptr(unsafe.Pointer(sourcePointer)),
			uintptr(unsafe.Pointer(destinationPointer)),
			moveFileReplaceExisting|moveFileWriteThrough,
		)
		if result != 0 {
			return nil
		}
		if !retryableWindowsReplaceError(callErr) || !time.Now().Before(deadline) {
			return callErr
		}
		time.Sleep(windowsReplaceRetryDelay)
	}
}

func retryableWindowsReplaceError(err error) bool {
	return errors.Is(err, syscall.ERROR_ACCESS_DENIED) ||
		errors.Is(err, windowsErrorSharingViolation) ||
		errors.Is(err, windowsErrorLockViolation)
}
