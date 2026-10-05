//go:build windows

package build

import (
	"errors"
	"fmt"
	"os"

	"github.com/wago-org/wago/internal/windowsfilepath"
	"golang.org/x/sys/windows"
)

type buildFileIdentity struct {
	volumeSerialNumber uint32
	fileIndexHigh      uint32
	fileIndexLow       uint32
	linkCount          uint32
	fileAttributes     uint32
}

const windowsBuildOutputPreservedAttributes = uint32(
	windows.FILE_ATTRIBUTE_HIDDEN |
		windows.FILE_ATTRIBUTE_SYSTEM |
		windows.FILE_ATTRIBUTE_TEMPORARY |
		windows.FILE_ATTRIBUTE_NOT_CONTENT_INDEXED)

const windowsBuildOutputAllowedAttributes = windowsBuildOutputPreservedAttributes |
	windows.FILE_ATTRIBUTE_ARCHIVE | windows.FILE_ATTRIBUTE_NORMAL

func captureBuildFileIdentity(path string, followSymlinks bool, _ os.FileInfo) (buildFileIdentity, error) {
	// Ask only for identity and permit every sharing mode. Go's lazy SameFile
	// lookup opens with exclusive sharing on Windows, which would reject valid
	// builds while a runtime or another reader retains the old artifact handle.
	handle, err := openBuildFile(path, 0, followSymlinks)
	if err != nil {
		return buildFileIdentity{}, fmt.Errorf("identify file %s: %w", path, err)
	}
	identity, identityErr := buildFileIdentityFromHandle(handle)
	closeErr := windows.CloseHandle(handle)
	if identityErr != nil || closeErr != nil {
		return buildFileIdentity{}, fmt.Errorf("identify file %s: %w", path, errors.Join(identityErr, closeErr))
	}
	return identity, nil
}

func openBuildFile(path string, access uint32, followSymlinks bool) (windows.Handle, error) {
	pathPointer, err := windowsfilepath.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	flags := uint32(windows.FILE_FLAG_BACKUP_SEMANTICS)
	if !followSymlinks {
		flags |= windows.FILE_FLAG_OPEN_REPARSE_POINT
	}
	return windows.CreateFile(pathPointer, access,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, flags, 0)
}

func buildFileIdentityFromHandle(handle windows.Handle) (buildFileIdentity, error) {
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return buildFileIdentity{}, err
	}
	return buildFileIdentity{
		volumeSerialNumber: info.VolumeSerialNumber,
		fileIndexHigh:      info.FileIndexHigh,
		fileIndexLow:       info.FileIndexLow,
		linkCount:          info.NumberOfLinks,
		fileAttributes:     info.FileAttributes,
	}, nil
}

func sameBuildFileIdentity(left, right buildFileIdentity) bool {
	return left.volumeSerialNumber == right.volumeSerialNumber &&
		left.fileIndexHigh == right.fileIndexHigh && left.fileIndexLow == right.fileIndexLow
}

func validateBuildOutputPublication(path string, info os.FileInfo, identity buildFileIdentity) error {
	if err := validateWindowsBuildOutputIdentity(path, identity); err != nil {
		return err
	}
	if info == nil {
		return nil
	}
	// Atomic rename authorization on the parent must not bypass the target's
	// established write policy. Go's former os.WriteFile path requested
	// GENERIC_WRITE, so request the same rights with OPEN_EXISTING (no truncation)
	// and bind the result to the same identity that the build inspected.
	handle, err := openBuildFile(path, windows.GENERIC_WRITE, false)
	if err != nil {
		return fmt.Errorf("write existing output %s: %w", path, err)
	}
	openedIdentity, identityErr := buildFileIdentityFromHandle(handle)
	closeErr := windows.CloseHandle(handle)
	if identityErr != nil || closeErr != nil {
		return fmt.Errorf("write existing output %s: %w", path, errors.Join(identityErr, closeErr))
	}
	if !sameBuildFileIdentity(identity, openedIdentity) {
		return fmt.Errorf("output %s changed during build", path)
	}
	return nil
}

func validateWindowsBuildOutputIdentity(path string, identity buildFileIdentity) error {
	if identity.linkCount > 1 {
		// Renaming a new inode updates only one directory entry, unlike the old
		// in-place write shared by every hard link. Reject instead of reporting a
		// successful build while aliases continue serving stale bytes.
		return fmt.Errorf("output %s has multiple hard links", path)
	}
	if identity.fileAttributes&windows.FILE_ATTRIBUTE_READONLY != 0 {
		// The former in-place write could not open a read-only output. Reject before
		// compiling or staging so atomic replacement cannot bypass that authorization.
		return fmt.Errorf("output %s has the Windows read-only attribute", path)
	}
	if identity.fileAttributes&windows.FILE_ATTRIBUTE_ENCRYPTED != 0 {
		// Atomic replacement creates a distinct inode that may be plaintext even
		// when this file was encrypted. Reject rather than silently broadening access
		// or attempting an EFS transformation with different authorization needs.
		return fmt.Errorf("output %s is EFS-encrypted", path)
	}
	if unsupported := identity.fileAttributes &^ windowsBuildOutputAllowedAttributes; unsupported != 0 {
		// Compression, sparseness, integrity, recall, pinning, and other
		// filesystem/provider attributes require feature-specific operations. Fail
		// closed instead of publishing a new inode with different storage semantics.
		return fmt.Errorf("output %s has unsupported Windows file attributes %#x", path, unsupported)
	}
	return nil
}

func validateStagedBuildOutputAttributes(attributes uint32) error {
	if attributes&windows.FILE_ATTRIBUTE_ENCRYPTED != 0 {
		return errors.New("staged output unexpectedly inherited EFS encryption")
	}
	if unsupported := attributes &^ windowsBuildOutputAllowedAttributes; unsupported != 0 {
		return fmt.Errorf("staged output has unsupported Windows file attributes %#x", unsupported)
	}
	return nil
}
