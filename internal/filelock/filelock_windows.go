//go:build windows

package filelock

import (
	"errors"
	"os"
	"syscall"
	"unsafe"
)

const (
	lockfileExclusiveLock   = 0x00000002
	lockfileFailImmediately = 0x00000001
	errorLockViolation      = syscall.Errno(33)
	errorSharingViolation   = syscall.Errno(32)
)

var (
	lockFileEx   = syscall.NewLazyDLL("kernel32.dll").NewProc("LockFileEx")
	unlockFileEx = syscall.NewLazyDLL("kernel32.dll").NewProc("UnlockFileEx")
)

func openCreateLock(path string) (*os.File, error) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := syscall.CreateFile(name, syscall.GENERIC_READ|syscall.GENERIC_WRITE,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE, nil,
		syscall.OPEN_ALWAYS, syscall.FILE_ATTRIBUTE_NORMAL|syscall.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(handle), path), nil
}

func tryLock(file *os.File) (bool, error) {
	var overlapped syscall.Overlapped
	result, _, callErr := lockFileEx.Call(
		file.Fd(),
		lockfileExclusiveLock|lockfileFailImmediately,
		0,
		1,
		0,
		uintptr(unsafe.Pointer(&overlapped)),
	)
	if result != 0 {
		return true, nil
	}
	if errors.Is(callErr, errorLockViolation) || errors.Is(callErr, errorSharingViolation) {
		return false, nil
	}
	return false, callErr
}

func unlock(file *os.File) error {
	var overlapped syscall.Overlapped
	result, _, callErr := unlockFileEx.Call(
		file.Fd(),
		0,
		1,
		0,
		uintptr(unsafe.Pointer(&overlapped)),
	)
	if result == 0 {
		return callErr
	}
	return nil
}

func trySharedLock(file *os.File) (bool, error) {
	var overlapped syscall.Overlapped
	result, _, callErr := lockFileEx.Call(file.Fd(), lockfileFailImmediately, 0, 1, 0, uintptr(unsafe.Pointer(&overlapped)))
	if result != 0 {
		return true, nil
	}
	if errors.Is(callErr, errorLockViolation) || errors.Is(callErr, errorSharingViolation) {
		return false, nil
	}
	return false, callErr
}

func openExistingLock(path string, shared bool) (*os.File, error) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	access := uint32(syscall.GENERIC_READ)
	if !shared {
		access |= syscall.GENERIC_WRITE
	}
	handle, err := syscall.CreateFile(name, access, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE, nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL|syscall.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(handle), path), nil
}
