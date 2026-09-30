//go:build windows

package atomicfile

import (
	"errors"
	"path/filepath"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	windowsErrorSharingViolation = syscall.Errno(32)
	windowsErrorLockViolation    = syscall.Errno(33)
	windowsReplaceRetryDelay     = 10 * time.Millisecond
	windowsReplaceRetryTimeout   = 2 * time.Second
)

type fileRenameInfo struct {
	flags          uint32
	rootDirectory  windows.Handle
	fileNameLength uint32
	fileName       [1]uint16
}

func replaceExisting(source, destination string) error {
	sourcePointer, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	destination, err = filepath.Abs(destination)
	if err != nil {
		return err
	}
	const nameOffset = int(unsafe.Offsetof(fileRenameInfo{}.fileName)) / 2
	buffer := make([]uint16, nameOffset, nameOffset+len(destination)+1)
	for _, character := range destination {
		if character == 0 {
			return syscall.EINVAL
		}
		buffer = utf16.AppendRune(buffer, character)
	}
	buffer = append(buffer, 0)
	info := (*fileRenameInfo)(unsafe.Pointer(&buffer[0]))
	info.flags = windows.FILE_RENAME_REPLACE_IF_EXISTS | windows.FILE_RENAME_POSIX_SEMANTICS
	info.fileNameLength = uint32((len(buffer) - nameOffset - 1) * 2)
	class := uint32(windows.FileRenameInfoEx)
	deadline := time.Now().Add(windowsReplaceRetryTimeout)
	for {
		handle, err := windows.CreateFile(sourcePointer, windows.DELETE,
			windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
			windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
		if err == nil {
			err = windows.SetFileInformationByHandle(handle, class, (*byte)(unsafe.Pointer(info)), uint32(len(buffer)*2))
			if closeErr := windows.CloseHandle(handle); closeErr != nil {
				return errors.Join(err, closeErr)
			}
			if class == windows.FileRenameInfoEx && (errors.Is(err, windows.ERROR_INVALID_PARAMETER) || errors.Is(err, windows.ERROR_NOT_SUPPORTED) || errors.Is(err, windows.ERROR_INVALID_FUNCTION)) {
				// Older file systems support replacement without POSIX handle semantics.
				class = windows.FileRenameInfo
				info.flags = windows.FILE_RENAME_REPLACE_IF_EXISTS
				continue
			}
		}
		if err == nil || !retryableWindowsReplaceError(err) || !time.Now().Before(deadline) {
			return err
		}
		time.Sleep(windowsReplaceRetryDelay)
	}
}

func retryableWindowsReplaceError(err error) bool {
	return errors.Is(err, syscall.ERROR_ACCESS_DENIED) ||
		errors.Is(err, windowsErrorSharingViolation) ||
		errors.Is(err, windowsErrorLockViolation)
}
