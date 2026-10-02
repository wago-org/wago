//go:build darwin

package build

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func validateBuildOutputWriteAccess(path string, expected buildFileIdentity) error {
	// Atomic publication needs only directory replacement permission, but the
	// former os.WriteFile path also opened an existing artifact for writing.
	// Probe that authorization without truncation. ACL capture uses O_EVTONLY
	// separately because macOS does not expose ACL attrlist data on O_WRONLY.
	fd, err := unix.Open(path, unix.O_WRONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("open existing output for writing %s: %w", path, err)
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return fmt.Errorf("open existing output for writing %s: invalid file descriptor", path)
	}
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("inspect writable output %s: %w", path, errors.Join(err, file.Close()))
	}
	identity, err := captureBuildFileIdentity(path, false, info)
	if err != nil {
		return errors.Join(err, file.Close())
	}
	if !sameBuildFileIdentity(expected, identity) {
		return errors.Join(fmt.Errorf("output %s changed during build", path), file.Close())
	}
	if unpreserved, err := hasUnpreservedBuildOutputXattrs(fd); err != nil {
		return errors.Join(fmt.Errorf("inspect output extended attributes %s: %w", path, err), file.Close())
	} else if unpreserved {
		return errors.Join(fmt.Errorf("output has unpreserved or invalid extended attributes: %s", path), file.Close())
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close writable-output probe %s: %w", path, err)
	}
	return nil
}
