//go:build windows

package build

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"unsafe"

	"github.com/wago-org/wago/internal/windowsfilepath"
	"golang.org/x/sys/windows"
)

type buildOutputMetadata struct {
	descriptor               *windows.SECURITY_DESCRIPTOR
	integrityLabelDescriptor *windows.SECURITY_DESCRIPTOR
	accessPolicyDescriptor   *windows.SECURITY_DESCRIPTOR
	fileAttributes           uint32
}

func sameBuildOutputMetadata(left, right buildOutputMetadata) bool {
	return left.fileAttributes == right.fileAttributes &&
		sameWindowsSecurityDescriptor(left.descriptor, right.descriptor) &&
		sameWindowsFilteredSACLDescriptor(left.integrityLabelDescriptor, right.integrityLabelDescriptor) &&
		sameWindowsFilteredSACLDescriptor(left.accessPolicyDescriptor, right.accessPolicyDescriptor)
}

func sameWindowsFilteredSACLDescriptor(left, right *windows.SECURITY_DESCRIPTOR) bool {
	leftEmpty, leftErr := windowsFilteredSACLEmpty(left)
	rightEmpty, rightErr := windowsFilteredSACLEmpty(right)
	if leftErr != nil || rightErr != nil {
		return false
	}
	if leftEmpty || rightEmpty {
		// Filtered SACL queries can represent “no matching ACE” either as no SACL
		// or as an empty-but-present SACL depending on other SACL components. Both
		// forms have identical authorization semantics for the queried component.
		return leftEmpty && rightEmpty
	}
	return sameWindowsSecurityDescriptor(left, right)
}

func windowsFilteredSACLEmpty(descriptor *windows.SECURITY_DESCRIPTOR) (bool, error) {
	if descriptor == nil {
		return true, nil
	}
	acl, _, err := descriptor.SACL()
	if errors.Is(err, windows.ERROR_OBJECT_NOT_FOUND) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return acl == nil || acl.AceCount == 0, nil
}

func sameWindowsSecurityDescriptor(left, right *windows.SECURITY_DESCRIPTOR) bool {
	if (left == nil) != (right == nil) {
		return false
	}
	if left == nil {
		return true
	}
	leftBytes := unsafe.Slice((*byte)(unsafe.Pointer(left)), int(left.Length()))
	rightBytes := unsafe.Slice((*byte)(unsafe.Pointer(right)), int(right.Length()))
	equal := bytes.Equal(leftBytes, rightBytes)
	// Both slices point into Go-owned self-relative descriptor allocations.
	runtime.KeepAlive(left)
	runtime.KeepAlive(right)
	return equal
}

type windowsBuildFileBasicInfo struct {
	creationTime   int64
	lastAccessTime int64
	lastWriteTime  int64
	changeTime     int64
	attributes     uint32
	_              uint32
}

func captureBuildOutputMetadata(path string, info os.FileInfo, expected buildFileIdentity) (buildOutputMetadata, error) {
	if info == nil {
		return buildOutputMetadata{}, nil
	}
	// Preserve owner, primary group, and DACL because replacing an inode would
	// otherwise reset who can access an established artifact. Audit-only SACL
	// ACEs are an explicit compatibility limitation: ordinary builders cannot
	// query them without ACCESS_SYSTEM_SECURITY/SeSecurityPrivilege, and they do
	// not grant or deny access. Mandatory labels, resource attributes, and central
	// access policy IDs do affect authorization and can be queried separately with
	// READ_CONTROL, so they are captured below and must match the staging inode.
	handle, err := openBuildFile(path,
		windows.READ_CONTROL|windows.FILE_READ_ATTRIBUTES|windows.GENERIC_WRITE, false)
	if err != nil {
		return buildOutputMetadata{}, fmt.Errorf("inspect output access metadata %s: %w", path, err)
	}
	metadata, identity, metadataErr := captureWindowsBuildOutputMetadata(handle, path)
	closeErr := windows.CloseHandle(handle)
	if metadataErr != nil || closeErr != nil {
		return buildOutputMetadata{}, fmt.Errorf("inspect output access metadata %s: %w", path,
			errors.Join(metadataErr, closeErr))
	}
	if !sameBuildFileIdentity(expected, identity) {
		return buildOutputMetadata{}, fmt.Errorf("output %s changed during build", path)
	}
	if err := validateWindowsBuildOutputIdentity(path, identity); err != nil {
		return buildOutputMetadata{}, err
	}
	return metadata, nil
}

func captureWindowsBuildOutputMetadata(handle windows.Handle, path string) (buildOutputMetadata, buildFileIdentity, error) {
	identity, identityErr := buildFileIdentityFromHandle(handle)
	descriptor, descriptorErr := windows.GetSecurityInfo(handle, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.GROUP_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	// LABEL_SECURITY_INFORMATION exposes only the mandatory-integrity component
	// using ordinary READ_CONTROL. Audit-only SACL ACEs require a privilege that
	// normal builders do not have and do not participate in access authorization.
	integrityLabelDescriptor, integrityErr := windows.GetSecurityInfo(handle, windows.SE_FILE_OBJECT,
		windows.LABEL_SECURITY_INFORMATION)
	// Resource-attribute and scoped-policy ACEs live in the SACL, but unlike
	// audit ACEs they participate in authorization. Capture only those bounded
	// components rather than requesting the privileged, audit-bearing full SACL.
	accessPolicyDescriptor, accessPolicyErr := windows.GetSecurityInfo(handle, windows.SE_FILE_OBJECT,
		windows.ATTRIBUTE_SECURITY_INFORMATION|windows.SCOPE_SECURITY_INFORMATION)
	streamErr := validateWindowsBuildOutputStreams(handle, path)
	if identityErr != nil || descriptorErr != nil || integrityErr != nil || accessPolicyErr != nil || streamErr != nil {
		return buildOutputMetadata{}, buildFileIdentity{},
			errors.Join(identityErr, descriptorErr, integrityErr, accessPolicyErr, streamErr)
	}
	return buildOutputMetadata{
		descriptor:               descriptor,
		integrityLabelDescriptor: integrityLabelDescriptor,
		accessPolicyDescriptor:   accessPolicyDescriptor,
		fileAttributes:           identity.fileAttributes,
	}, identity, nil
}

func captureNewBuildOutputMetadata(path string) (buildOutputMetadata, error) {
	// The retained Windows stage is created with an owner-only DACL so incomplete
	// artifact bytes never inherit a readable directory ACL. Sample the directory's
	// ordinary creation metadata on a separate empty file, then restore it only
	// after the real artifact is complete so a new output keeps os.WriteFile's
	// established inheritance behavior without exposing partial contents.
	probe, probePath, err := createWindowsBuildMetadataProbe(filepath.Dir(path))
	if err != nil {
		return buildOutputMetadata{}, fmt.Errorf("create output access metadata probe: %w", err)
	}
	// Mark the empty probe for identity-bound deletion before inspecting it. A
	// close-then-path-remove cleanup would let a writable-directory peer exchange
	// the random name and trick build into deleting the replacement.
	if err := deleteWindowsBuildMetadataProbe(probe); err != nil {
		closeErr := windows.CloseHandle(probe)
		return buildOutputMetadata{}, fmt.Errorf("reserve output access metadata probe cleanup: %w",
			errors.Join(err, closeErr))
	}
	metadata, _, captureErr := captureWindowsBuildOutputMetadata(probe, probePath)
	closeErr := windows.CloseHandle(probe)
	if captureErr != nil || closeErr != nil {
		return buildOutputMetadata{}, fmt.Errorf("inspect inherited output access metadata: %w",
			errors.Join(captureErr, closeErr))
	}
	return metadata, nil
}

func createWindowsBuildMetadataProbe(directory string) (windows.Handle, string, error) {
	for attempts := 0; attempts < 100; attempts++ {
		var random [16]byte
		if _, err := rand.Read(random[:]); err != nil {
			return windows.InvalidHandle, "", err
		}
		path := filepath.Join(directory, ".wago-build-metadata-"+hex.EncodeToString(random[:]))
		pathPointer, err := windowsfilepath.UTF16PtrFromString(path)
		if err != nil {
			return windows.InvalidHandle, "", err
		}
		// A nil security descriptor intentionally obtains the same inherited access
		// policy as the historical os.WriteFile-created output. Keep the empty probe
		// exclusively open until it is delete-pending so a writable-directory peer
		// cannot mutate the sampled metadata or hold cleanup hostage.
		handle, err := windows.CreateFile(pathPointer,
			windows.GENERIC_READ|windows.GENERIC_WRITE|windows.READ_CONTROL|windows.DELETE,
			0, nil,
			windows.CREATE_NEW, windows.FILE_ATTRIBUTE_NORMAL, 0)
		if errors.Is(err, windows.ERROR_FILE_EXISTS) || errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
			continue
		}
		if err != nil {
			return windows.InvalidHandle, "", err
		}
		return handle, path, nil
	}
	return windows.InvalidHandle, "", errors.New("too many output access metadata probe name collisions")
}

func deleteWindowsBuildMetadataProbe(handle windows.Handle) error {
	flags := uint32(windows.FILE_DISPOSITION_DELETE | windows.FILE_DISPOSITION_POSIX_SEMANTICS |
		windows.FILE_DISPOSITION_IGNORE_READONLY_ATTRIBUTE)
	err := windows.SetFileInformationByHandle(handle, windows.FileDispositionInfoEx,
		(*byte)(unsafe.Pointer(&flags)), uint32(unsafe.Sizeof(flags)))
	if err == nil {
		return nil
	}
	if !errors.Is(err, windows.ERROR_INVALID_PARAMETER) && !errors.Is(err, windows.ERROR_NOT_SUPPORTED) &&
		!errors.Is(err, windows.ERROR_INVALID_FUNCTION) {
		return err
	}
	deleteFile := byte(1)
	return windows.SetFileInformationByHandle(handle, windows.FileDispositionInfo,
		&deleteFile, uint32(unsafe.Sizeof(deleteFile)))
}

func applyBuildOutputMetadata(writer io.Writer, metadata buildOutputMetadata) error {
	if metadata.descriptor == nil {
		return nil
	}
	file, ok := writer.(*os.File)
	if !ok {
		return fmt.Errorf("preserve output access metadata: unexpected writer %T", writer)
	}
	targetOwner, _, ownerErr := metadata.descriptor.Owner()
	targetGroup, _, groupErr := metadata.descriptor.Group()
	dacl, _, daclErr := metadata.descriptor.DACL()
	if errors.Is(daclErr, windows.ERROR_OBJECT_NOT_FOUND) {
		daclErr = nil
		dacl = nil
	}
	control, _, controlErr := metadata.descriptor.Control()
	if ownerErr != nil || groupErr != nil || daclErr != nil || controlErr != nil {
		return fmt.Errorf("decode output access metadata: %w", errors.Join(ownerErr, groupErr, daclErr, controlErr))
	}

	// Compare owner and group through the already-open staging handle. Only include
	// them in SetSecurityInfo when they differ; ordinary non-retained test files can
	// still copy a DACL through the owner's implicit WRITE_DAC permission.
	stagedHandle := windows.Handle(file.Fd())
	stagedIdentity, identityErr := buildFileIdentityFromHandle(stagedHandle)
	if identityErr == nil {
		identityErr = validateStagedBuildOutputAttributes(stagedIdentity.fileAttributes)
	}
	stagedDescriptor, descriptorErr := windows.GetSecurityInfo(stagedHandle, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.GROUP_SECURITY_INFORMATION)
	stagedIntegrityDescriptor, integrityErr := windows.GetSecurityInfo(stagedHandle, windows.SE_FILE_OBJECT,
		windows.LABEL_SECURITY_INFORMATION)
	stagedAccessPolicyDescriptor, accessPolicyErr := windows.GetSecurityInfo(stagedHandle, windows.SE_FILE_OBJECT,
		windows.ATTRIBUTE_SECURITY_INFORMATION|windows.SCOPE_SECURITY_INFORMATION)
	if identityErr != nil || descriptorErr != nil || integrityErr != nil || accessPolicyErr != nil {
		return fmt.Errorf("inspect staged output access metadata: %w", errors.Join(identityErr, descriptorErr, integrityErr, accessPolicyErr))
	}
	if !sameWindowsFilteredSACLDescriptor(metadata.integrityLabelDescriptor, stagedIntegrityDescriptor) {
		// Changing a mandatory integrity label can broaden or revoke write access.
		// Retain the old inode instead of requesting WRITE_OWNER solely to relabel.
		return errors.New("atomic replacement cannot preserve the output mandatory integrity label")
	}
	if !sameWindowsFilteredSACLDescriptor(metadata.accessPolicyDescriptor, stagedAccessPolicyDescriptor) {
		// Resource attributes and central policy IDs can restrict access. Setting
		// them requires WRITE_DAC or privileged ACCESS_SYSTEM_SECURITY, so do not
		// silently drop them or request rights the former in-place write did not
		// need; retain the old inode.
		return errors.New("atomic replacement cannot preserve the output resource attributes or central access policy")
	}
	stagedOwner, _, stagedOwnerErr := stagedDescriptor.Owner()
	stagedGroup, _, stagedGroupErr := stagedDescriptor.Group()
	if stagedOwnerErr != nil || stagedGroupErr != nil {
		return fmt.Errorf("decode staged output access metadata: %w", errors.Join(stagedOwnerErr, stagedGroupErr))
	}
	if err := applyBuildOutputAttributes(stagedHandle, stagedIdentity.fileAttributes,
		metadata.fileAttributes&windowsBuildOutputPreservedAttributes); err != nil {
		return fmt.Errorf("preserve output Windows attributes: %w", err)
	}

	securityInformation := windows.SECURITY_INFORMATION(windows.DACL_SECURITY_INFORMATION)
	// READ_CONTROL is implicit for the owner and keeps handle-based security
	// updates compatible with implementations that validate the descriptor too.
	access := uint32(windows.READ_CONTROL | windows.WRITE_DAC)
	var owner, group *windows.SID
	if !windows.EqualSid(targetOwner, stagedOwner) {
		owner = targetOwner
		access |= windows.WRITE_OWNER
		securityInformation |= windows.OWNER_SECURITY_INFORMATION
	}
	if !windows.EqualSid(targetGroup, stagedGroup) {
		group = targetGroup
		access |= windows.WRITE_OWNER
		securityInformation |= windows.GROUP_SECURITY_INFORMATION
	}
	if control&windows.SE_DACL_PROTECTED != 0 {
		securityInformation |= windows.PROTECTED_DACL_SECURITY_INFORMATION
	} else {
		securityInformation |= windows.UNPROTECTED_DACL_SECURITY_INFORMATION
	}
	// Retained Windows stages reserve WRITE_DAC and WRITE_OWNER on this original
	// identity-bound handle before installing their protected creator-only DACL.
	// Apply through that handle so an impersonating creator does not later depend
	// on which OS thread runs a pathname reopen. Tests and non-retained callers can
	// still supply an ordinary *os.File; those fall back to the checked reopen when
	// its generic-write handle lacks security-descriptor rights.
	setErr := windows.SetSecurityInfo(stagedHandle, windows.SE_FILE_OBJECT,
		securityInformation, owner, group, dacl, nil)
	if errors.Is(setErr, windows.ERROR_ACCESS_DENIED) {
		handle, openErr := openBuildFile(file.Name(), access, false)
		if openErr != nil {
			setErr = errors.Join(setErr, fmt.Errorf("open staged output access metadata: %w", openErr))
		} else {
			reopenedIdentity, identityErr := buildFileIdentityFromHandle(handle)
			if identityErr != nil || !sameBuildFileIdentity(stagedIdentity, reopenedIdentity) {
				if identityErr != nil {
					setErr = fmt.Errorf("revalidate staged output access metadata: %w", identityErr)
				} else {
					setErr = fmt.Errorf("staged output %s changed during build", file.Name())
				}
			} else {
				setErr = windows.SetSecurityInfo(handle, windows.SE_FILE_OBJECT,
					securityInformation, owner, group, dacl, nil)
			}
			setErr = errors.Join(setErr, windows.CloseHandle(handle))
		}
	}
	// Owner, group, and DACL pointers refer into descriptor's Go allocation.
	runtime.KeepAlive(metadata.descriptor)
	runtime.KeepAlive(metadata.integrityLabelDescriptor)
	runtime.KeepAlive(metadata.accessPolicyDescriptor)
	runtime.KeepAlive(stagedDescriptor)
	runtime.KeepAlive(stagedIntegrityDescriptor)
	runtime.KeepAlive(stagedAccessPolicyDescriptor)
	if setErr != nil {
		return fmt.Errorf("preserve output access metadata: %w", setErr)
	}
	return nil
}

func applyBuildOutputAttributes(handle windows.Handle, current, preserved uint32) error {
	// These simple DOS attributes describe the pathname's visibility/indexing and
	// can be reproduced exactly on the already-open staging inode. ARCHIVE is
	// deliberately left write-derived: Windows sets it when content changes, so
	// restoring a previously cleared bit would hide the new write from backups.
	desired := current&^windowsBuildOutputPreservedAttributes |
		preserved&windowsBuildOutputPreservedAttributes
	if desired&^windows.FILE_ATTRIBUTE_NORMAL != 0 {
		desired &^= windows.FILE_ATTRIBUTE_NORMAL
	} else if desired == 0 {
		desired = windows.FILE_ATTRIBUTE_NORMAL
	}
	if desired == current {
		return nil
	}
	info := windowsBuildFileBasicInfo{attributes: desired}
	return windows.SetFileInformationByHandle(handle, windows.FileBasicInfo,
		(*byte)(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)))
}
