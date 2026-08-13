//go:build windows

package sourcearchive

import "syscall"

func publishDirectoryNoReplace(source, target string) error {
	sourcePointer, err := syscall.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	targetPointer, err := syscall.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	return syscall.MoveFile(sourcePointer, targetPointer)
}
