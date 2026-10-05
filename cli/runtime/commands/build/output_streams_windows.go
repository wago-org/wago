//go:build windows

package build

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

const maxWindowsBuildOutputStreamInfo = 64 << 10

func validateWindowsBuildOutputStreams(handle windows.Handle, path string) error {
	for _, size := range []int{4 << 10, maxWindowsBuildOutputStreamInfo} {
		buffer := make([]byte, size)
		err := windows.GetFileInformationByHandleEx(handle, windows.FileStreamInfo,
			&buffer[0], uint32(len(buffer)))
		if errors.Is(err, windows.ERROR_INVALID_FUNCTION) || errors.Is(err, windows.ERROR_NOT_SUPPORTED) {
			// Filesystems without named-stream support cannot carry an alternate
			// stream that inode replacement would discard (notably FAT and Wine's
			// Unix-backed compatibility filesystem).
			return nil
		}
		if errors.Is(err, windows.ERROR_MORE_DATA) || errors.Is(err, windows.ERROR_INSUFFICIENT_BUFFER) {
			continue
		}
		if err != nil {
			return fmt.Errorf("enumerate output streams: %w", err)
		}
		name, named, err := windowsBuildOutputNamedStream(buffer)
		if err != nil {
			return err
		}
		if named {
			// Replacing an inode drops NTFS alternate streams such as provenance or
			// download-zone metadata. Reject rather than silently discarding them.
			return fmt.Errorf("output %s has alternate data stream %q", path, name)
		}
		return nil
	}
	return fmt.Errorf("output stream metadata exceeds %d bytes", maxWindowsBuildOutputStreamInfo)
}

func windowsBuildOutputNamedStream(buffer []byte) (string, bool, error) {
	for offset := uint64(0); ; {
		if offset+24 > uint64(len(buffer)) {
			return "", false, errors.New("malformed output stream metadata")
		}
		next := binary.LittleEndian.Uint32(buffer[offset:])
		nameLength := binary.LittleEndian.Uint32(buffer[offset+4:])
		if nameLength%2 != 0 || offset+24+uint64(nameLength) > uint64(len(buffer)) {
			return "", false, errors.New("malformed output stream name")
		}
		units := make([]uint16, nameLength/2)
		for index := range units {
			units[index] = binary.LittleEndian.Uint16(buffer[offset+24+uint64(index*2):])
		}
		name := string(utf16.Decode(units))
		if !strings.EqualFold(name, "::$DATA") {
			return name, true, nil
		}
		if next == 0 {
			return "", false, nil
		}
		if next < 24 || offset+uint64(next) >= uint64(len(buffer)) {
			return "", false, errors.New("malformed output stream offset")
		}
		offset += uint64(next)
	}
}
