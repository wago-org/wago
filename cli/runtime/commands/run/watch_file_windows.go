//go:build windows && !wago_lean

package run

import (
	"encoding/binary"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

type watchedWindowsFileBasicInfo struct {
	creationTime, lastAccessTime, lastWriteTime, changeTime int64
	attributes                                              uint32
	_                                                       uint32
}

type watchedWindowsFileIDInfo struct {
	volume uint64
	id     [16]byte
}

func openWatchedFile(path string) (*os.File, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(
		name,
		windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(handle), path), nil
}

func metadataForWatchedFile(file *os.File, info os.FileInfo) (watchedFileMetadata, error) {
	handle := windows.Handle(file.Fd())
	basic := watchedWindowsFileBasicInfo{}
	if err := windows.GetFileInformationByHandleEx(handle, windows.FileBasicInfo, (*byte)(unsafe.Pointer(&basic)), uint32(unsafe.Sizeof(basic))); err != nil {
		return watchedFileMetadata{}, err
	}
	identity := watchedWindowsFileIDInfo{}
	if err := windows.GetFileInformationByHandleEx(handle, windows.FileIdInfo, (*byte)(unsafe.Pointer(&identity)), uint32(unsafe.Sizeof(identity))); err != nil {
		return watchedFileMetadata{}, err
	}
	return watchedFileMetadata{
		size:     info.Size(),
		modified: info.ModTime().UnixNano(),
		change: [4]uint64{
			uint64(basic.changeTime),
			identity.volume,
			binary.LittleEndian.Uint64(identity.id[:8]),
			binary.LittleEndian.Uint64(identity.id[8:]),
		},
	}, nil
}
