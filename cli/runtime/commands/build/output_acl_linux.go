//go:build linux

package build

import (
	"bytes"
	"errors"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

const (
	linuxAccessACLName        = "system.posix_acl_access"
	maxLinuxAccessACLSize     = 64 << 10
	maxLinuxSecurityLabelSize = 4 << 10
)

var linuxSecurityLabelNames = [...]string{
	"security.selinux",
	"security.SMACK64",
	"security.SMACK64EXEC",
	"security.SMACK64TRANSMUTE",
	"security.SMACK64MMAP",
}

func captureBuildAccessMetadata(path string, expected buildFileIdentity) ([]byte, bool, []buildOutputSecurityLabel, os.FileInfo, error) {
	// Match the former O_WRONLY write authorization while binding ACL and label
	// reads to the opened inode instead of trusting the mutable pathname.
	fd, err := syscall.Open(path, unix.O_WRONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, false, nil, nil, buildErrorSE("open output metadata %s: %w", path, err)
	}
	// A descriptor returned by a successful unix.Open is valid, so NewFile cannot
	// take its documented nil path here.
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, false, nil, nil, buildErrorSE("inspect output metadata %s: %w", path, err)
	}
	identity, err := captureBuildFileIdentity(path, false, info)
	if err != nil {
		return nil, false, nil, nil, err
	}
	if !sameBuildFileIdentity(expected, identity) {
		return nil, false, nil, nil, newBuildError("output changed during build")
	}
	if err := validateBuildOutputPublication(path, info, identity); err != nil {
		return nil, false, nil, nil, err
	}
	if err := validateLinuxBuildOutputAttributes(fd); err != nil {
		return nil, false, nil, nil, buildErrorSE("inspect persistent Linux attributes %s: %w", path, err)
	}
	if unpreserved, err := hasUnpreservedBuildOutputXattrs(fd); err != nil {
		return nil, false, nil, nil, buildErrorSE("inspect output extended attributes %s: %w", path, err)
	} else if unpreserved {
		return nil, false, nil, nil, buildErrorS("output has unpreserved or invalid extended attributes: %s", path)
	}
	acl, present, err := readLinuxXattrFD(fd, linuxAccessACLName, maxLinuxAccessACLSize)
	if err != nil {
		return nil, false, nil, nil, buildErrorSE("inspect output ACL %s: %w", path, err)
	}
	labels, err := captureLinuxSecurityLabels(fd, linuxSecurityLabelNames[:])
	if err != nil {
		return nil, false, nil, nil, err
	}
	return acl, present, labels, info, nil
}

func preservedBuildOutputXattr(name []byte) bool {
	if string(name) == linuxAccessACLName {
		return true
	}
	for _, modeled := range linuxSecurityLabelNames {
		if string(name) == modeled {
			return true
		}
	}
	return false
}

const linuxBuildOutputPersistentAttributes = uint64(
	unix.STATX_ATTR_VERITY |
		unix.STATX_ATTR_IMMUTABLE |
		unix.STATX_ATTR_APPEND |
		unix.STATX_ATTR_COMPRESSED |
		unix.STATX_ATTR_ENCRYPTED |
		unix.STATX_ATTR_NODUMP |
		unix.STATX_ATTR_DAX)

func validateLinuxBuildOutputAttributes(fd int) error {
	var stat unix.Statx_t
	if err := unix.Statx(fd, "", unix.AT_EMPTY_PATH|unix.AT_SYMLINK_NOFOLLOW, unix.STATX_BASIC_STATS, &stat); err != nil {
		// Replacing an inode when its persistent flags cannot be inspected could
		// bypass immutable/verity authorization or silently change storage policy.
		return err
	}
	return validateLinuxBuildOutputAttributeValues(stat.Attributes, stat.Attributes_mask)
}

func validateLinuxBuildOutputAttributeValues(attributes, supported uint64) error {
	// Attributes_mask is the kernel/filesystem declaration of meaningful bits on
	// this inode. A bit outside that mask has no supported persistent semantic to
	// preserve; every reported and set access/storage bit fails closed.
	if unsafeAttributes := attributes & supported & linuxBuildOutputPersistentAttributes; unsafeAttributes != 0 {
		return formatBuildError("output has persistent Linux file attributes %#x", unsafeAttributes)
	}
	return nil
}

func captureLinuxSecurityLabels(fd int, names []string) ([]buildOutputSecurityLabel, error) {
	// Preserve only persistent access-control labels. Content writes historically
	// clear file capabilities, and copying IMA/EVM hashes or arbitrary xattrs would
	// give metadata with different replacement semantics to new artifact bytes.
	labels := make([]buildOutputSecurityLabel, 0, len(names))
	for _, name := range names {
		value, present, err := readLinuxXattrFD(fd, name, maxLinuxSecurityLabelSize)
		if err != nil {
			return nil, buildErrorSE("inspect security label %s: %w", name, err)
		}
		labels = append(labels, buildOutputSecurityLabel{name: name, value: value, present: present})
	}
	return labels, nil
}

func readLinuxXattrFD(fd int, name string, maximumSize int) ([]byte, bool, error) {
	for attempts := 0; attempts < 4; attempts++ {
		size, err := unix.Fgetxattr(fd, name, nil)
		if errors.Is(err, unix.ENODATA) || errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.EOPNOTSUPP) {
			return nil, false, nil
		}
		if err != nil {
			return nil, false, err
		}
		if size > maximumSize {
			return nil, false, formatBuildError("extended attribute is too large: %d bytes", size)
		}
		acl := make([]byte, size)
		size, err = unix.Fgetxattr(fd, name, acl)
		if errors.Is(err, unix.ERANGE) || errors.Is(err, unix.ENODATA) {
			continue
		}
		if err != nil {
			return nil, false, err
		}
		return acl[:size], true, nil
	}
	return nil, false, buildErrorS("extended attribute %s kept changing", name)
}

func applyBuildAccessMetadata(file *os.File, acl []byte, present bool, labels []buildOutputSecurityLabel) error {
	fd := int(file.Fd())
	if present {
		update, err := linuxXattrNeedsUpdate(fd, linuxAccessACLName, acl, maxLinuxAccessACLSize)
		if err != nil {
			return err
		}
		if update {
			if err := unix.Fsetxattr(fd, linuxAccessACLName, acl, 0); err != nil {
				return err
			}
		}
	} else {
		// A directory default ACL can be inherited by the staged inode even when the
		// prior artifact had none, so explicitly remove it to preserve effective access.
		if err := ensureLinuxXattrAbsent(fd, linuxAccessACLName); err != nil {
			return err
		}
	}
	for _, label := range labels {
		if label.present {
			update, err := linuxXattrNeedsUpdate(fd, label.name, label.value, maxLinuxSecurityLabelSize)
			if err != nil {
				return buildErrorSE("inspect staged %s: %w", label.name, err)
			}
			// Setting an unchanged SELinux/Smack label can require relabel
			// authority that the former in-place content write did not need.
			if update {
				if err := unix.Fsetxattr(fd, label.name, label.value, 0); err != nil {
					return buildErrorSE("set %s: %w", label.name, err)
				}
			}
		} else if err := ensureLinuxXattrAbsent(fd, label.name); err != nil {
			return buildErrorSE("remove %s: %w", label.name, err)
		}
	}
	return nil
}

func linuxXattrNeedsUpdate(fd int, name string, value []byte, maximumSize int) (bool, error) {
	current, present, err := readLinuxXattrFD(fd, name, maximumSize)
	return !present || !bytes.Equal(current, value), err
}

func ensureLinuxXattrAbsent(fd int, name string) error {
	// Some security namespaces reject removal before checking whether an
	// attribute exists. Inspect the staged inode first so an already-absent label
	// remains a no-op, while an inherited label still must be removed or fail.
	_, err := unix.Fgetxattr(fd, name, nil)
	if errors.Is(err, unix.ENODATA) || errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.EOPNOTSUPP) {
		return nil
	}
	if err != nil {
		return err
	}
	return unix.Fremovexattr(fd, name)
}
