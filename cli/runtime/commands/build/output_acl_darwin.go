//go:build darwin

package build

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	initialDarwinACLBuffer    = 512
	maxDarwinAccessACLSize    = 64 << 10
	darwinFileSecuritySize    = 44
	darwinAccessACLEntrySize  = 24
	darwinMaxAccessACLEntries = 128
	darwinFileSecurityMagic   = 0x012cc16d
	darwinFileSecurityNoACL   = ^uint32(0)
	darwinAccessACLXattrName  = "com.apple.system.Security"
)

func preservedBuildOutputXattr(name []byte) bool { return string(name) == darwinAccessACLXattrName }

func captureBuildAccessMetadata(path string, expected buildFileIdentity) ([]byte, bool, []buildOutputSecurityLabel, os.FileInfo, error) {
	// O_EVTONLY permits stable, descriptor-bound stat and attrlist inspection of
	// mode-000 files. openDarwinBuildOutput keeps that identity-pinning handle
	// live while validateBuildOutputPublication performs the separate O_WRONLY
	// authorization required by the former os.WriteFile path. macOS rejects ACL
	// xattr reads on both kinds of descriptor, so capture uses the public
	// descriptor-bound attrlist interface below.
	metadataFile, info, err := openDarwinBuildOutput(path, unix.O_EVTONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, expected)
	if err != nil {
		return nil, false, nil, nil, fmt.Errorf("open output access ACL %s: %w", path, err)
	}
	acl, present, aclErr := readDarwinAccessACL(int(metadataFile.Fd()))
	closeErr := metadataFile.Close()
	if aclErr != nil || closeErr != nil {
		return nil, false, nil, nil, fmt.Errorf("inspect output access ACL %s: %w", path, errors.Join(aclErr, closeErr))
	}
	return acl, present, nil, info, nil
}

func captureNewBuildAccessMetadata(file *os.File) ([]byte, bool, []buildOutputSecurityLabel, error) {
	acl, present, err := readDarwinAccessACL(int(file.Fd()))
	return acl, present, nil, err
}

func openDarwinBuildOutput(path string, flags int, expected buildFileIdentity) (*os.File, os.FileInfo, error) {
	fd, err := unix.Open(path, flags, 0)
	if err != nil {
		return nil, nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, nil, errors.New("invalid file descriptor")
	}
	info, err := file.Stat()
	if err == nil {
		var identity buildFileIdentity
		identity, err = captureBuildFileIdentity(path, false, info)
		if err == nil && !sameBuildFileIdentity(expected, identity) {
			err = fmt.Errorf("output %s changed during build", path)
		}
		if err == nil {
			err = validateBuildOutputPublication(path, info, identity)
		}
	}
	if err != nil {
		return nil, nil, errors.Join(err, file.Close())
	}
	return file, info, nil
}

func readDarwinAccessACL(fd int) ([]byte, bool, error) {
	attributes := unix.Attrlist{
		Bitmapcount: unix.ATTR_BIT_MAP_COUNT,
		Commonattr:  unix.ATTR_CMN_RETURNED_ATTRS | unix.ATTR_CMN_EXTENDED_SECURITY,
	}
	buffer := make([]byte, initialDarwinACLBuffer)
	for attempts := 0; attempts < 4; attempts++ {
		_, _, errno := unix.Syscall6(unix.SYS_FGETATTRLIST,
			uintptr(fd), uintptr(unsafe.Pointer(&attributes)), uintptr(unsafe.Pointer(&buffer[0])),
			uintptr(len(buffer)), uintptr(unix.FSOPT_REPORT_FULLSIZE), 0)
		runtime.KeepAlive(&attributes)
		runtime.KeepAlive(buffer)
		if errors.Is(errno, unix.ENOTSUP) || errors.Is(errno, unix.EOPNOTSUPP) {
			return nil, false, nil
		}
		if errno != 0 {
			return nil, false, errno
		}
		total := int(binary.NativeEndian.Uint32(buffer[:4]))
		if total > maxDarwinAccessACLSize+32 {
			return nil, false, fmt.Errorf("access ACL is too large: %d bytes", total)
		}
		if total > len(buffer) {
			buffer = make([]byte, total)
			continue
		}
		return parseDarwinAccessACL(buffer[:total])
	}
	return nil, false, errors.New("access ACL size changed during inspection")
}

func parseDarwinAccessACL(buffer []byte) ([]byte, bool, error) {
	const (
		returnedAttributesOffset = 4
		returnedAttributesSize   = 20
		referenceOffset          = returnedAttributesOffset + returnedAttributesSize
	)
	if len(buffer) < referenceOffset {
		return nil, false, errors.New("short access ACL attribute result")
	}
	returnedCommon := binary.NativeEndian.Uint32(buffer[returnedAttributesOffset : returnedAttributesOffset+4])
	if returnedCommon&unix.ATTR_CMN_EXTENDED_SECURITY == 0 {
		return nil, false, nil
	}
	if len(buffer) < referenceOffset+8 {
		return nil, false, errors.New("short access ACL attribute reference")
	}
	dataOffset := int(int32(binary.NativeEndian.Uint32(buffer[referenceOffset : referenceOffset+4])))
	dataLength := int(binary.NativeEndian.Uint32(buffer[referenceOffset+4 : referenceOffset+8]))
	dataStart := referenceOffset + dataOffset
	if dataLength == 0 {
		return nil, false, nil
	}
	if dataOffset%4 != 0 || dataStart < referenceOffset+8 || dataLength > maxDarwinAccessACLSize ||
		dataStart > len(buffer) || dataLength > len(buffer)-dataStart {
		return nil, false, errors.New("invalid access ACL attribute reference")
	}
	data := buffer[dataStart : dataStart+dataLength]
	if len(data) < darwinFileSecuritySize || binary.NativeEndian.Uint32(data[:4]) != darwinFileSecurityMagic {
		return nil, false, errors.New("invalid access ACL file-security record")
	}
	entryCount := binary.NativeEndian.Uint32(data[36:40])
	expectedSize := darwinFileSecuritySize
	if entryCount == darwinFileSecurityNoACL {
		if len(data) != expectedSize {
			return nil, false, errors.New("invalid no-ACL file-security record")
		}
		return nil, false, nil
	}
	if entryCount > darwinMaxAccessACLEntries {
		return nil, false, fmt.Errorf("access ACL has too many entries: %d", entryCount)
	}
	expectedSize += int(entryCount) * darwinAccessACLEntrySize
	if len(data) != expectedSize {
		return nil, false, fmt.Errorf("invalid access ACL size: got %d, want %d", len(data), expectedSize)
	}
	// Preserve the descriptor-returned host-format kauth_filesec bytes alone.
	// fsetattrlist packing is reconstructed below rather than mixing this public
	// attrlist representation with the privileged raw xattr representation.
	return append([]byte(nil), data...), true, nil
}

func applyBuildAccessMetadata(file *os.File, acl []byte, present bool, _ []buildOutputSecurityLabel) error {
	hadACL := present
	if !present {
		// A staged file can inherit an ACL from its parent even when the replaced
		// output had none. The documented NOACL sentinel removes it without using
		// the privileged com.apple.system.Security xattr transport.
		acl = make([]byte, darwinFileSecuritySize)
		binary.NativeEndian.PutUint32(acl[:4], darwinFileSecurityMagic)
		binary.NativeEndian.PutUint32(acl[36:40], darwinFileSecurityNoACL)
	}
	packed := make([]byte, 8+len(acl))
	binary.NativeEndian.PutUint32(packed[:4], 8)
	binary.NativeEndian.PutUint32(packed[4:8], uint32(len(acl)))
	copy(packed[8:], acl)
	attributes := unix.Attrlist{
		Bitmapcount: unix.ATTR_BIT_MAP_COUNT,
		Commonattr:  unix.ATTR_CMN_EXTENDED_SECURITY,
	}
	_, _, errno := unix.Syscall6(unix.SYS_FSETATTRLIST,
		uintptr(file.Fd()), uintptr(unsafe.Pointer(&attributes)), uintptr(unsafe.Pointer(&packed[0])),
		uintptr(len(packed)), 0, 0)
	runtime.KeepAlive(&attributes)
	runtime.KeepAlive(packed)
	if !hadACL && (errors.Is(errno, unix.ENOTSUP) || errors.Is(errno, unix.EOPNOTSUPP)) {
		// A filesystem that could not return an ACL also cannot have inherited one.
		// Treat unsupported absence as faithful preservation, as the old path did.
		return nil
	}
	if errno != 0 {
		return errno
	}
	return nil
}
