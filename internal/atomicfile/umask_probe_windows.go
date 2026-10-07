//go:build windows

package atomicfile

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"unsafe"

	"github.com/wago-org/wago/internal/windowsfilepath"
	"golang.org/x/sys/windows"
)

func probeUmaskMode(destination string, requested fs.FileMode, requireExistingParent bool) (fs.FileMode, error) {
	directory := filepath.Dir(destination)
	if !requireExistingParent {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return 0, err
		}
	}
	for attempts := 0; attempts < 100; attempts++ {
		var random [16]byte
		if _, err := rand.Read(random[:]); err != nil {
			return 0, err
		}
		path := filepath.Join(directory, ".wago-umask-"+hex.EncodeToString(random[:]))
		pathPointer, err := windowsfilepath.UTF16PtrFromString(path)
		if err != nil {
			return 0, err
		}
		attributes := uint32(windows.FILE_ATTRIBUTE_NORMAL)
		if requested.Perm()&0o200 == 0 {
			attributes = windows.FILE_ATTRIBUTE_READONLY
		}
		// Deny sharing and mark the exact handle delete-pending before Stat. A peer
		// cannot rename the empty probe or make cleanup target a replacement path.
		handle, err := windows.CreateFile(pathPointer,
			windows.GENERIC_READ|windows.GENERIC_WRITE|windows.DELETE,
			0, nil, windows.CREATE_NEW, attributes|windows.FILE_FLAG_DELETE_ON_CLOSE, 0)
		if errors.Is(err, windows.ERROR_FILE_EXISTS) || errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
			continue
		}
		if err != nil {
			return 0, err
		}
		deleteErr := markWindowsUmaskProbeDelete(handle)
		probe := os.NewFile(uintptr(handle), path)
		if probe == nil {
			return 0, errors.Join(errors.New("wrap atomic umask probe"), deleteErr, windows.CloseHandle(handle))
		}
		info, statErr := probe.Stat()
		closeErr := probe.Close()
		if deleteErr != nil || statErr != nil || closeErr != nil {
			return 0, errors.Join(deleteErr, statErr, closeErr)
		}
		return info.Mode().Perm(), nil
	}
	return 0, errors.New("create atomic umask probe: too many name collisions")
}

func markWindowsUmaskProbeDelete(handle windows.Handle) error {
	flags := uint32(windows.FILE_DISPOSITION_DELETE | windows.FILE_DISPOSITION_POSIX_SEMANTICS |
		windows.FILE_DISPOSITION_IGNORE_READONLY_ATTRIBUTE)
	err := windows.SetFileInformationByHandle(handle, windows.FileDispositionInfoEx,
		(*byte)(unsafe.Pointer(&flags)), uint32(unsafe.Sizeof(flags)))
	if err == nil {
		return nil
	}
	if !errors.Is(err, windows.ERROR_INVALID_PARAMETER) && !errors.Is(err, windows.ERROR_NOT_SUPPORTED) &&
		!errors.Is(err, windows.ERROR_INVALID_FUNCTION) {
		return fmt.Errorf("mark atomic umask probe delete-pending: %w", err)
	}
	deleteFile := byte(1)
	if err := windows.SetFileInformationByHandle(handle, windows.FileDispositionInfo,
		&deleteFile, uint32(unsafe.Sizeof(deleteFile))); err != nil {
		return fmt.Errorf("mark atomic umask probe delete-pending: %w", err)
	}
	return nil
}
