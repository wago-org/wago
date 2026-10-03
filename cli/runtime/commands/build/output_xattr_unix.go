//go:build linux || darwin

package build

import (
	"golang.org/x/sys/unix"
)

const maxModeledBuildOutputXattrNamesSize = 128

func hasUnpreservedBuildOutputXattrs(fd int) (bool, error) {
	// Atomic replacement cannot retain arbitrary inode metadata. Inspect names
	// through the already identity-checked descriptor and fail closed instead of
	// copying stale signatures, quarantine data, resource forks, or user payloads.
	// The fixed buffer holds all six unique modeled Linux names (126 bytes with
	// terminators), and Darwin models only one. ERANGE therefore proves that at
	// least one unmodeled name exists and can fail closed without an allocation.
	var names [maxModeledBuildOutputXattrNamesSize]byte
	size, err := unix.Flistxattr(fd, names[:])
	if err == unix.ENOTSUP || err == unix.EOPNOTSUPP {
		return false, nil
	}
	if err == unix.ERANGE {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if size > len(names) {
		return true, nil
	}
	if size == 0 {
		return false, nil
	}
	listed := names[:size]
	start := 0
	for end, b := range listed {
		if b != 0 {
			continue
		}
		if end == start || !preservedBuildOutputXattr(listed[start:end]) {
			return true, nil
		}
		start = end + 1
	}
	return start != len(listed), nil
}
