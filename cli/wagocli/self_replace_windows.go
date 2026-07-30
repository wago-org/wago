//go:build windows

package wagocli

import (
	"os"
	"syscall"
	"unsafe"
)

const (
	moveFileReplaceExisting  = 0x1
	moveFileDelayUntilReboot = 0x4
)

var moveFileEx = syscall.NewLazyDLL("kernel32.dll").NewProc("MoveFileExW")

func replaceSelfExecutable(executable, staged string) (bool, error) {
	if err := os.Rename(staged, executable); err == nil {
		return false, nil
	}
	return true, scheduleMove(staged, executable, moveFileReplaceExisting|moveFileDelayUntilReboot)
}

func removeSelfExecutable(executable string) (bool, error) {
	if err := os.Remove(executable); err == nil || os.IsNotExist(err) {
		return false, nil
	}
	return true, scheduleMove(executable, "", moveFileDelayUntilReboot)
}

func scheduleMove(source, destination string, flags uintptr) error {
	sourcePtr, err := syscall.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	var destinationPtr *uint16
	if destination != "" {
		destinationPtr, err = syscall.UTF16PtrFromString(destination)
		if err != nil {
			return err
		}
	}
	result, _, callErr := moveFileEx.Call(
		uintptr(unsafe.Pointer(sourcePtr)),
		uintptr(unsafe.Pointer(destinationPtr)),
		flags,
	)
	if result == 0 {
		return callErr
	}
	return nil
}
