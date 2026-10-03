//go:build linux && !(tinygo && wago_lean && wago_minimal)

package atomicfile

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"syscall"

	"golang.org/x/sys/unix"
)

func probeUmaskMode(destination string, requested fs.FileMode, requireExistingParent bool) (_ fs.FileMode, resultErr error) {
	directory := filepath.Dir(destination)
	if !requireExistingParent {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return 0, err
		}
	}
	// Pin the parent before interpreting O_TMPFILE failures. That keeps a real
	// missing-parent or traversal error distinct from filesystem feature support.
	parentFD, err := syscall.Openat(unix.AT_FDCWD, directory, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return 0, err
	}

	// O_TMPFILE asks the destination filesystem to apply umask and any default ACL
	// without ever publishing a pathname that a directory writer could exchange
	// before cleanup. The empty inode disappears when this descriptor closes.
	fd, err := syscall.Openat(parentFD, ".", unix.O_TMPFILE|unix.O_RDWR|unix.O_CLOEXEC, uint32(requested.Perm()))
	if err == nil {
		mode, probeErr := linuxUmaskProbeMode(fd, directory)
		return mode, joinErrors(probeErr, syscall.Close(parentFD))
	}
	if !linuxUnnamedTempUnsupported(err) {
		return 0, joinErrors(formatError("create unnamed umask probe in %s: %w", directory, err),
			syscall.Close(parentFD))
	}
	mode, probeErr := probeLinuxUmaskModeInPrivateDirectory(parentFD, directory, requested)
	return mode, joinErrors(probeErr, syscall.Close(parentFD))
}

func linuxUnnamedTempUnsupported(err error) bool {
	// Linux and filesystems have used these errors for unsupported O_TMPFILE.
	// Because the existing parent is pinned above, ENOENT cannot mean a missing
	// pathname here. Do not hide authorization or resource failures.
	return errors.Is(err, unix.EOPNOTSUPP) || errors.Is(err, unix.EISDIR) ||
		errors.Is(err, unix.EINVAL) || errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.ENOENT)
}

func linuxUmaskProbeMode(fd int, name string) (fs.FileMode, error) {
	// Both callers pass a descriptor returned by a successful Openat, so NewFile's
	// invalid-descriptor nil case is unreachable.
	probe := os.NewFile(uintptr(fd), name)
	info, statErr := probe.Stat()
	closeErr := probe.Close()
	if statErr != nil || closeErr != nil {
		return 0, joinErrors(statErr, closeErr)
	}
	return info.Mode().Perm(), nil
}

func probeLinuxUmaskModeInPrivateDirectory(parentFD int, parentPath string, requested fs.FileMode) (fs.FileMode, error) {
	private, err := createLinuxPrivateDirectory(parentFD, ".wago-umask-")
	if err != nil {
		return 0, err
	}

	fd, err := syscall.Openat(private.pinFD, "mode",
		unix.O_RDWR|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, uint32(requested.Perm()))
	mode := fs.FileMode(0)
	probeErr := err
	if probeErr == nil {
		// Unlink relative to the normalized private directory before closing the
		// empty probe. No pathname cleanup can be redirected onto another file.
		unlinkErr := syscall.Unlinkat(private.pinFD, "mode")
		mode, probeErr = linuxUmaskProbeMode(fd, filepath.Join(parentPath, private.name, "mode"))
		probeErr = joinErrors(unlinkErr, probeErr)
	}
	removeErr := removeLinuxPrivateDirectory(private)
	closeErr := syscall.Close(private.pinFD)
	return mode, joinErrors(probeErr, removeErr, closeErr)
}

func createLinuxPrivateDirectory(parentFD int, prefix string) (linuxPrivateDirectory, error) {
	for attempts := 0; attempts < 100; attempts++ {
		var random [16]byte
		if _, err := rand.Read(random[:]); err != nil {
			return linuxPrivateDirectory{}, err
		}
		name := prefix + hex.EncodeToString(random[:])
		if err := syscall.Mkdirat(parentFD, name, 0o700); errors.Is(err, unix.EEXIST) {
			continue
		} else if err != nil {
			return linuxPrivateDirectory{}, err
		}

		private, err := openLinuxPrivateDirectory(parentFD, name)
		if err != nil {
			// The pinned helper owns cleanup only after it has proved identity. A
			// writable-directory peer may have exchanged the new name on failure;
			// leaving an empty directory is safer than deleting a replacement.
			return linuxPrivateDirectory{}, err
		}
		return private, nil
	}
	return linuxPrivateDirectory{}, newError("create private staging directory: name collisions")
}

type linuxPrivateDirectory struct {
	parentFD int
	name     string
	pinFD    int
	identity linuxFileIdentity
}

type linuxFileIdentity struct {
	dev uint64
	ino uint64
}

func (identity linuxFileIdentity) matches(stat *unix.Stat_t) bool {
	return identity.dev == stat.Dev && identity.ino == stat.Ino
}

func openLinuxPrivateDirectory(parentFD int, name string) (linuxPrivateDirectory, error) {
	pinFD, err := syscall.Openat(parentFD, name, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return linuxPrivateDirectory{}, err
	}
	var pinned syscall.Stat_t
	var named unix.Stat_t
	if err := syscall.Fstat(pinFD, &pinned); err != nil {
		return linuxPrivateDirectory{}, joinErrors(err, syscall.Close(pinFD))
	}
	if err := unix.Fstatat(parentFD, name, &named, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return linuxPrivateDirectory{}, joinErrors(err, syscall.Close(pinFD))
	}
	if pinned.Dev != named.Dev || pinned.Ino != named.Ino || pinned.Mode&unix.S_IFMT != unix.S_IFDIR ||
		pinned.Uid != uint32(os.Geteuid()) {
		return linuxPrivateDirectory{}, joinErrors(
			newError("private staging directory is unsafe"), syscall.Close(pinFD))
	}
	identity := linuxFileIdentity{dev: uint64(pinned.Dev), ino: uint64(pinned.Ino)}
	if err := normalizeLinuxPrivateDirectoryMode(pinFD, pinned.Mode); err != nil {
		return failLinuxPrivateDirectory(parentFD, name, pinFD, identity,
			formatError("set private directory mode: %w", err))
	}
	if err := syscall.Fstat(pinFD, &pinned); err != nil {
		return failLinuxPrivateDirectory(parentFD, name, pinFD, identity, err)
	}
	if err := unix.Fstatat(parentFD, name, &named, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return failLinuxPrivateDirectory(parentFD, name, pinFD, identity, err)
	}
	const desiredMode = uint32(0o700)
	if identity.dev != uint64(pinned.Dev) || identity.ino != uint64(pinned.Ino) || !identity.matches(&named) ||
		pinned.Mode&0o777 != desiredMode || named.Mode&0o777 != desiredMode {
		return failLinuxPrivateDirectory(parentFD, name, pinFD, identity,
			newError("private staging directory changed during setup"))
	}
	return linuxPrivateDirectory{
		parentFD: parentFD, name: name, pinFD: pinFD, identity: identity,
	}, nil
}

func failLinuxPrivateDirectory(parentFD int, name string, pinFD int,
	expected linuxFileIdentity, result error) (linuxPrivateDirectory, error) {
	removeErr := removeLinuxPrivateDirectoryName(parentFD, name, expected)
	return linuxPrivateDirectory{}, joinErrors(result, removeErr, syscall.Close(pinFD))
}

func normalizeLinuxPrivateDirectoryMode(pinFD int, currentMode uint32) error {
	const desired = uint32(0o700)
	if currentMode&0o777 == desired {
		// The common case needs no new-kernel syscall or /proc dependency.
		return nil
	}
	if err := unix.Fchmodat(pinFD, "", desired, unix.AT_EMPTY_PATH); err == nil {
		return nil
	} else if !errors.Is(err, unix.EOPNOTSUPP) && !errors.Is(err, unix.ENOSYS) && !errors.Is(err, unix.EINVAL) {
		return err
	}
	// Older kernels lack fchmodat2(AT_EMPTY_PATH). /proc/self/fd follows this
	// already-pinned O_PATH descriptor, so the fallback cannot chmod a pathname
	// replacement in the writable output directory.
	return unix.Chmod("/proc/self/fd/"+strconv.Itoa(pinFD), desired)
}

func removeLinuxPrivateDirectory(private linuxPrivateDirectory) error {
	return removeLinuxPrivateDirectoryName(private.parentFD, private.name, private.identity)
}

func removeLinuxPrivateDirectoryName(parentFD int, name string, expected linuxFileIdentity) error {
	var current unix.Stat_t
	if err := unix.Fstatat(parentFD, name, &current, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return formatError("private directory changed before cleanup: %w", err)
	}
	if !expected.matches(&current) {
		return newError("private directory changed before cleanup")
	}
	// AT_REMOVEDIR can remove only an empty directory, never an exchanged file or
	// a directory containing another user's data.
	return unix.Unlinkat(parentFD, name, unix.AT_REMOVEDIR)
}
