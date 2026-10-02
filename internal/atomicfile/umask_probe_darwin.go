//go:build darwin

package atomicfile

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

type darwinPrivateDirectory struct {
	parentPath string
	name       string
	parent     *os.File
	parentStat unix.Stat_t
	pin        *os.File
	file       *os.File
	stat       unix.Stat_t
}

// x/sys does not currently expose Darwin's O_SEARCH. Unlike O_EVTONLY,
// O_SEARCH does not request implicit read access, so a caller that may create a
// write-only output can still pin and operate relative to its searchable parent.
// Keep this value synchronized with <sys/fcntl.h> (O_EXEC | O_DIRECTORY).
const darwinOSearch = 0x40000000 | unix.O_DIRECTORY

func probeUmaskMode(destination string, requested fs.FileMode, requireExistingParent bool) (_ fs.FileMode, resultErr error) {
	directory := filepath.Dir(destination)
	if !requireExistingParent {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return 0, err
		}
	}
	private, err := createDarwinPrivateDirectory(directory, ".wago-umask-")
	if err != nil {
		return 0, err
	}
	defer func() {
		removeErr := removeDarwinPrivateDirectory(private)
		closeErr := private.file.Close()
		closePinErr := private.pin.Close()
		closeParentErr := private.parent.Close()
		resultErr = errors.Join(resultErr, closeErr, closePinErr, closeParentErr, removeErr)
	}()

	// The former os.WriteFile path requested write-only access. On Darwin a
	// restrictive umask can make the new inode mode 000 soon enough that an O_RDWR
	// create is denied even though O_WRONLY creation is authorized.
	fd, err := unix.Openat(int(private.file.Fd()), "mode", unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		uint32(requested.Perm()))
	if err != nil {
		return 0, fmt.Errorf("create private umask mode probe: %w", err)
	}
	probe := os.NewFile(uintptr(fd), filepath.Join(private.parentPath, private.name, "mode"))
	if probe == nil {
		return 0, errors.Join(errors.New("wrap atomic umask probe"), unix.Close(fd))
	}
	// The pinned private directory cannot be traversed by another identity. Unlink
	// relative to that directory descriptor before closing the probe, so cleanup
	// never follows a parent pathname that could have been exchanged.
	unlinkErr := unix.Unlinkat(int(private.file.Fd()), "mode", 0)
	info, statErr := probe.Stat()
	closeErr := probe.Close()
	if unlinkErr != nil || statErr != nil || closeErr != nil {
		return 0, errors.Join(unlinkErr, statErr, closeErr)
	}
	return info.Mode().Perm(), nil
}

func createDarwinPrivateDirectory(parent, prefix string) (darwinPrivateDirectory, error) {
	parentFD, err := unix.Open(parent, darwinOSearch|unix.O_CLOEXEC, 0)
	if err != nil {
		return darwinPrivateDirectory{}, fmt.Errorf("open private atomic parent directory: %w", err)
	}
	parentFile := os.NewFile(uintptr(parentFD), parent)
	if parentFile == nil {
		_ = unix.Close(parentFD)
		return darwinPrivateDirectory{}, errors.New("wrap private atomic parent directory")
	}
	var parentStat unix.Stat_t
	if err := unix.Fstat(parentFD, &parentStat); err != nil {
		return darwinPrivateDirectory{}, errors.Join(err, parentFile.Close())
	}
	for attempts := 0; attempts < 100; attempts++ {
		var random [16]byte
		if _, err := rand.Read(random[:]); err != nil {
			return darwinPrivateDirectory{}, errors.Join(err, parentFile.Close())
		}
		name := prefix + hex.EncodeToString(random[:])
		path := filepath.Join(parent, name)
		if err := unix.Mkdirat(parentFD, name, 0o700); errors.Is(err, unix.EEXIST) {
			continue
		} else if err != nil {
			return darwinPrivateDirectory{}, errors.Join(
				fmt.Errorf("create private atomic directory: %w", err), parentFile.Close())
		}
		var initial unix.Stat_t
		if err := unix.Fstatat(parentFD, name, &initial, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			return darwinPrivateDirectory{}, errors.Join(
				fmt.Errorf("inspect new private atomic directory: %w", err), parentFile.Close())
		}
		if initial.Mode&unix.S_IFMT != unix.S_IFDIR || initial.Uid != uint32(os.Geteuid()) {
			return darwinPrivateDirectory{}, errors.Join(
				errors.New("new private atomic directory has unexpected type or owner"), parentFile.Close())
		}
		fd, err := unix.Openat(parentFD, name, unix.O_EVTONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if errors.Is(err, unix.EACCES) {
			// mkdir applies the process umask, so a fully restrictive umask can
			// produce mode 000 before there is a descriptor to normalize. The leaf
			// is a freshly-created, verified directory owned by this effective user.
			// Normalize it only when the parent excludes every different identity,
			// then revalidate its inode immediately; same-identity processes remain
			// outside the filesystem permission boundary used throughout staging.
			err = prepareDarwinPrivateDirectoryOpen(parentFD, name, initial, parentStat)
			if err == nil {
				fd, err = unix.Openat(parentFD, name,
					unix.O_EVTONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
			}
		}
		if err != nil {
			// Do not path-remove after a failed open: a directory writer could have
			// exchanged the just-created name. Leaving an empty private directory is
			// safer than deleting an unverified replacement.
			return darwinPrivateDirectory{}, errors.Join(
				fmt.Errorf("open new private atomic directory: %w", err), parentFile.Close())
		}
		pin := os.NewFile(uintptr(fd), path)
		if pin == nil {
			_ = unix.Close(fd)
			return darwinPrivateDirectory{}, errors.Join(
				errors.New("wrap private atomic directory"), parentFile.Close())
		}
		var pinned, named unix.Stat_t
		statErr := unix.Fstat(fd, &pinned)
		nameErr := unix.Fstatat(parentFD, name, &named, unix.AT_SYMLINK_NOFOLLOW)
		identityMatches := statErr == nil && nameErr == nil && initial.Dev == pinned.Dev && initial.Ino == pinned.Ino &&
			initial.Dev == named.Dev && initial.Ino == named.Ino
		if statErr != nil || nameErr != nil || !identityMatches || pinned.Mode&unix.S_IFMT != unix.S_IFDIR ||
			pinned.Uid != uint32(os.Geteuid()) {
			closeErr := pin.Close()
			closeParentErr := parentFile.Close()
			return darwinPrivateDirectory{}, fmt.Errorf("validate private atomic directory: %w",
				errors.Join(statErr, nameErr, closeErr, closeParentErr))
		}
		cleanup := func(result error) error {
			return errors.Join(result, removeDarwinPrivateDirectoryName(parentFD, name, initial))
		}
		normalizeErr := normalizeDarwinPrivateDirectory(fd)
		var opened, normalizedName unix.Stat_t
		normalizedStatErr := unix.Fstat(fd, &opened)
		normalizedNameErr := unix.Fstatat(parentFD, name, &normalizedName, unix.AT_SYMLINK_NOFOLLOW)
		aclPresent, _, aclErr := inspectDarwinDirectoryACL(fd)
		if normalizeErr != nil || normalizedStatErr != nil || normalizedNameErr != nil || aclErr != nil ||
			opened.Dev != initial.Dev || opened.Ino != initial.Ino || normalizedName.Dev != initial.Dev ||
			normalizedName.Ino != initial.Ino || opened.Mode&0o777 != 0o700 || aclPresent {
			cleanupErr := cleanup(errors.Join(normalizeErr, normalizedStatErr, normalizedNameErr, aclErr))
			closeErr := pin.Close()
			closeParentErr := parentFile.Close()
			return darwinPrivateDirectory{}, fmt.Errorf("normalize private atomic directory: %w",
				errors.Join(cleanupErr, closeErr, closeParentErr))
		}
		// O_EVTONLY pins and normalizes a mode-000 directory without traversal
		// rights, but it is not a usable openat directory descriptor. Reopen only
		// after mode/ACL normalization, and compare while the original inode remains
		// pinned so a pathname exchange cannot substitute another directory.
		usableFD, usableErr := unix.Openat(parentFD, name,
			unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if usableErr != nil {
			cleanupErr := cleanup(usableErr)
			return darwinPrivateDirectory{}, errors.Join(cleanupErr, pin.Close(), parentFile.Close())
		}
		file := os.NewFile(uintptr(usableFD), path)
		if file == nil {
			_ = unix.Close(usableFD)
			cleanupErr := cleanup(errors.New("wrap usable atomic umask probe directory"))
			return darwinPrivateDirectory{}, errors.Join(cleanupErr, pin.Close(), parentFile.Close())
		}
		var usable unix.Stat_t
		usableStatErr := unix.Fstat(usableFD, &usable)
		if usableStatErr != nil || usable.Dev != opened.Dev || usable.Ino != opened.Ino {
			cleanupErr := cleanup(errors.Join(usableStatErr,
				errors.New("private atomic umask probe directory changed before reopen")))
			return darwinPrivateDirectory{}, errors.Join(cleanupErr,
				file.Close(), pin.Close(), parentFile.Close())
		}
		return darwinPrivateDirectory{
			parentPath: parent, name: name, parent: parentFile, parentStat: parentStat,
			pin: pin, file: file, stat: opened,
		}, nil
	}
	return darwinPrivateDirectory{}, errors.Join(
		errors.New("create private atomic umask probe directory: too many name collisions"), parentFile.Close())
}

func prepareDarwinPrivateDirectoryOpen(parentFD int, name string, initial, parent unix.Stat_t) error {
	_, aclAllowsMutation, err := inspectDarwinDirectoryACL(parentFD)
	if err != nil {
		return fmt.Errorf("inspect private atomic parent ACL: %w", err)
	}
	if parent.Uid != uint32(os.Geteuid()) || parent.Mode&0o022 != 0 || aclAllowsMutation {
		// Fchmodat and its following identity check cannot be one operation. Only
		// use that fallback when filesystem permissions exclude a cross-identity
		// parent-name exchange; otherwise fail without mutating an unpinned leaf.
		return errors.New("cannot normalize a mode-000 private directory in a shared parent")
	}
	if err := unix.Fchmodat(parentFD, name, 0o700, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return fmt.Errorf("normalize mode-000 private atomic directory: %w", err)
	}
	var current unix.Stat_t
	if err := unix.Fstatat(parentFD, name, &current, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	if current.Dev != initial.Dev || current.Ino != initial.Ino || current.Mode&unix.S_IFMT != unix.S_IFDIR ||
		current.Uid != uint32(os.Geteuid()) {
		return errors.New("private atomic directory changed during mode normalization")
	}
	return nil
}

func inspectDarwinDirectoryACL(fd int) (present, allowsMutation bool, resultErr error) {
	attributes := unix.Attrlist{Bitmapcount: unix.ATTR_BIT_MAP_COUNT,
		Commonattr: unix.ATTR_CMN_RETURNED_ATTRS | unix.ATTR_CMN_EXTENDED_SECURITY}
	buffer := make([]byte, 512)
	for attempts := 0; attempts < 4; attempts++ {
		_, _, errno := unix.Syscall6(unix.SYS_FGETATTRLIST,
			uintptr(fd), uintptr(unsafe.Pointer(&attributes)), uintptr(unsafe.Pointer(&buffer[0])),
			uintptr(len(buffer)), uintptr(unix.FSOPT_REPORT_FULLSIZE), 0)
		runtime.KeepAlive(&attributes)
		runtime.KeepAlive(buffer)
		if errors.Is(errno, unix.ENOTSUP) || errors.Is(errno, unix.EOPNOTSUPP) {
			return false, false, nil
		}
		if errno != 0 {
			return false, false, errno
		}
		if len(buffer) < 4 {
			return false, false, errors.New("short private directory ACL result")
		}
		total := int(binary.NativeEndian.Uint32(buffer[:4]))
		if total < 24 || total > 64<<10+32 {
			return false, false, fmt.Errorf("invalid private directory ACL result size: %d", total)
		}
		if total > len(buffer) {
			buffer = make([]byte, total)
			continue
		}
		buffer = buffer[:total]
		if binary.NativeEndian.Uint32(buffer[4:8])&unix.ATTR_CMN_EXTENDED_SECURITY == 0 {
			return false, false, nil
		}
		if len(buffer) < 32 {
			return false, false, errors.New("short private directory ACL reference")
		}
		offset := int(int32(binary.NativeEndian.Uint32(buffer[24:28])))
		length := int(binary.NativeEndian.Uint32(buffer[28:32]))
		start := 24 + offset
		if offset%4 != 0 || start < 32 || length < 44 || start > len(buffer) || length > len(buffer)-start {
			return false, false, errors.New("invalid private directory ACL result")
		}
		const (
			fileSecurityMagic = 0x012cc16d
			fileSecurityNoACL = ^uint32(0)
			aceSize           = 24
			maxEntries        = 128
		)
		acl := buffer[start : start+length]
		if binary.NativeEndian.Uint32(acl[:4]) != fileSecurityMagic {
			return false, false, errors.New("invalid private directory ACL magic")
		}
		entries := binary.NativeEndian.Uint32(acl[36:40])
		if entries == fileSecurityNoACL {
			if len(acl) != 44 {
				return false, false, errors.New("invalid private directory no-ACL record")
			}
			return false, false, nil
		}
		if entries > maxEntries || len(acl) != 44+int(entries)*aceSize {
			return false, false, errors.New("invalid private directory ACL entries")
		}
		const (
			aceKindMask = 0xf
			acePermit   = 1
			// Reject direct directory-entry grants and grants that could let an
			// inheriting child owner rewrite its policy. Inherit-only ACEs count:
			// the freshly-created directory can receive them before it is pinned.
			parentMutationRights = 1<<2 | 1<<4 | 1<<5 | 1<<6 | 1<<8 | 1<<10 |
				1<<12 | 1<<13 | 1<<21 | 1<<23
		)
		for index := uint32(0); index < entries; index++ {
			entry := acl[44+int(index)*aceSize:]
			flags := binary.NativeEndian.Uint32(entry[16:20])
			rights := binary.NativeEndian.Uint32(entry[20:24])
			if flags&aceKindMask == acePermit && rights&parentMutationRights != 0 {
				return true, true, nil
			}
		}
		return true, false, nil
	}
	return false, false, errors.New("private directory ACL size changed during inspection")
}

func normalizeDarwinPrivateDirectory(fd int) error {
	const (
		fileSecurityMagic = 0x012cc16d
		fileSecurityNoACL = ^uint32(0)
	)
	// Normalize through the pinned vnode before creating any artifact-bearing
	// file. mkdir is subject to umask, and Darwin ACL grants are independent of
	// mode bits, so both mode 0700 and the no-ACL sentinel must be explicit.
	attributes := unix.Attrlist{Bitmapcount: unix.ATTR_BIT_MAP_COUNT,
		Commonattr: unix.ATTR_CMN_ACCESSMASK | unix.ATTR_CMN_EXTENDED_SECURITY}
	packed := make([]byte, 4+8+44)
	binary.NativeEndian.PutUint32(packed[:4], 0o700)
	binary.NativeEndian.PutUint32(packed[4:8], 8)
	binary.NativeEndian.PutUint32(packed[8:12], 44)
	binary.NativeEndian.PutUint32(packed[12:16], fileSecurityMagic)
	binary.NativeEndian.PutUint32(packed[48:52], fileSecurityNoACL)
	_, _, errno := unix.Syscall6(unix.SYS_FSETATTRLIST,
		uintptr(fd), uintptr(unsafe.Pointer(&attributes)), uintptr(unsafe.Pointer(&packed[0])),
		uintptr(len(packed)), 0, 0)
	runtime.KeepAlive(&attributes)
	runtime.KeepAlive(packed)
	if errors.Is(errno, unix.ENOTSUP) || errors.Is(errno, unix.EOPNOTSUPP) {
		// Filesystems without extended-security support still need descriptor-bound
		// mode normalization. Verification below accepts unsupported ACL inspection
		// only because such a filesystem cannot have inherited an ACL.
		attributes.Commonattr = unix.ATTR_CMN_ACCESSMASK
		var mode [4]byte
		binary.NativeEndian.PutUint32(mode[:], 0o700)
		_, _, errno = unix.Syscall6(unix.SYS_FSETATTRLIST,
			uintptr(fd), uintptr(unsafe.Pointer(&attributes)), uintptr(unsafe.Pointer(&mode[0])),
			uintptr(len(mode)), 0, 0)
		runtime.KeepAlive(&attributes)
		runtime.KeepAlive(&mode)
	}
	if errno != 0 {
		return errno
	}
	present, _, err := inspectDarwinDirectoryACL(fd)
	if err != nil {
		return err
	}
	if present {
		return errors.New("private atomic directory retained an inherited ACL")
	}
	return nil
}

func removeDarwinPrivateDirectory(directory darwinPrivateDirectory) error {
	return removeDarwinPrivateDirectoryName(int(directory.parent.Fd()), directory.name, directory.stat)
}

func removeDarwinPrivateDirectoryName(parentFD int, name string, expected unix.Stat_t) error {
	var current unix.Stat_t
	if err := unix.Fstatat(parentFD, name, &current, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return fmt.Errorf("private atomic directory changed before cleanup: %w", err)
	}
	if current.Dev != expected.Dev || current.Ino != expected.Ino {
		return errors.New("private atomic directory changed before cleanup")
	}
	// Remove only an empty directory relative to the pinned physical parent.
	// Even a final name-exchange race cannot make AT_REMOVEDIR delete a file or a
	// directory containing user data.
	return unix.Unlinkat(parentFD, name, unix.AT_REMOVEDIR)
}
