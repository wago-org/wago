//go:build linux

package build

import (
	"syscall"

	"golang.org/x/sys/unix"
)

func validateBuildOutputWriteAccess(path string, expected buildFileIdentity) error {
	// Atomic publication needs only directory replacement permission, but the
	// former os.WriteFile path also had to open an existing artifact for writing.
	// Probe that exact authorization without O_TRUNC so a denial cannot damage the
	// old artifact. O_NOFOLLOW is safe because build resolves output symlinks first.
	fd, err := syscall.Open(path, unix.O_WRONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return buildErrorSE("open existing output for writing %s: %w", path, err)
	}
	var opened syscall.Stat_t
	statErr := syscall.Fstat(fd, &opened)
	closeErr := syscall.Close(fd)
	if statErr != nil {
		return buildErrorSE("inspect writable output %s: %w", path, statErr)
	}
	if closeErr != nil {
		// This descriptor never mutates data, but still surface a lifecycle error
		// rather than silently weakening the authorization probe contract.
		return buildErrorSE("close writable-output probe %s: %w", path, closeErr)
	}
	if uint64(opened.Dev) != expected.dev || uint64(opened.Ino) != expected.ino {
		return newBuildError("output changed during build")
	}
	return nil
}
