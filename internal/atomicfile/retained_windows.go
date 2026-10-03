//go:build windows

package atomicfile

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"unsafe"

	"github.com/wago-org/wago/internal/windowsfilepath"
	"golang.org/x/sys/windows"
)

type retainedReplaceHandle struct {
	handle windows.Handle
}

type retainedTempSecurity struct {
	attributes windows.SecurityAttributes
	descriptor *windows.SECURITY_DESCRIPTOR
	userSID    string
}

const localSystemSID = "S-1-5-18"

func retainedTempSecuritySDDL(userSID string) string {
	// Pin the owner to the effective user as well as the DACL. TOKEN_OWNER can be
	// an owner-enabled group whose members would otherwise receive implicit
	// WRITE_DAC and could broaden access to a partially written artifact.
	sddl := "O:" + userSID + "D:P(A;;GA;;;" + userSID + ")"
	if userSID != localSystemSID {
		sddl += "(A;;GA;;;SY)"
	}
	return sddl
}

func newRetainedTempSecurity() (retainedTempSecurity, error) {
	// Use the effective thread token so callers that deliberately impersonate do
	// not give the process identity access to their staged bytes. A protected DACL
	// must be installed by CreateFile itself: setting it after creation leaves a
	// window where inheritable directory ACEs can expose a partial artifact.
	user, err := windows.GetCurrentThreadEffectiveToken().GetTokenUser()
	if err != nil {
		return retainedTempSecurity{}, fmt.Errorf("inspect effective user for atomic temporary file: %w", err)
	}
	userSID := user.User.Sid.String()
	if userSID == "" {
		return retainedTempSecurity{}, errors.New("format effective user for atomic temporary file")
	}
	descriptor, err := windows.SecurityDescriptorFromString(retainedTempSecuritySDDL(userSID))
	if err != nil {
		return retainedTempSecurity{}, fmt.Errorf("create protected atomic temporary file DACL: %w", err)
	}
	security := retainedTempSecurity{descriptor: descriptor, userSID: userSID}
	security.attributes = windows.SecurityAttributes{
		Length:             uint32(unsafe.Sizeof(windows.SecurityAttributes{})),
		SecurityDescriptor: descriptor,
	}
	return security, nil
}

func createRetainedReplacementTemp(destination string, requireExistingParent bool) (*os.File, retainedReplaceHandle, error) {
	directory := filepath.Dir(destination)
	if !requireExistingParent {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return nil, retainedReplaceHandle{}, err
		}
	}
	// An effective token is thread-specific under impersonation. Pin this goroutine
	// from identity capture through CreateFile so the DACL protects the identity
	// whose authorization Windows actually uses for creation.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	security, err := newRetainedTempSecurity()
	if err != nil {
		return nil, retainedReplaceHandle{}, err
	}
	for attempts := 0; attempts < 100; attempts++ {
		var random [16]byte
		if _, err := rand.Read(random[:]); err != nil {
			return nil, retainedReplaceHandle{}, fmt.Errorf("generate atomic temporary name: %w", err)
		}
		name := filepath.Join(directory, ".wago-atomic-"+hex.EncodeToString(random[:]))
		namePointer, err := windowsfilepath.UTF16PtrFromString(name)
		if err != nil {
			return nil, retainedReplaceHandle{}, err
		}
		// DELETE must be granted before a build callback installs the output's
		// DACL. Go's ordinary CreateTemp handle does not share deletion, so create
		// this opt-in stage directly and duplicate its granted authorization.
		handle, err := windows.CreateFile(namePointer,
			windows.GENERIC_READ|windows.GENERIC_WRITE|windows.DELETE|windows.READ_CONTROL|windows.WRITE_DAC|windows.WRITE_OWNER,
			windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, &security.attributes,
			windows.CREATE_NEW, windows.FILE_ATTRIBUTE_NORMAL, 0)
		// The self-relative descriptor is Go-owned. Keep it alive until the
		// synchronous CreateFile has consumed SecurityAttributes.
		runtime.KeepAlive(security)
		if errors.Is(err, windows.ERROR_FILE_EXISTS) || errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
			continue
		}
		if err != nil {
			return nil, retainedReplaceHandle{}, fmt.Errorf("create retained atomic temporary file: %w", err)
		}
		// Verify the creation-time descriptor before exposing the writer. Trying to
		// repair a broad inherited DACL afterward is too late: a peer could already
		// retain a read handle and observe bytes written under the tightened DACL.
		securityErr := validateRetainedTempSecurity(handle, security.userSID)
		if securityErr != nil {
			reservation := retainedReplaceHandle{handle: handle}
			cleanupErr := reservation.remove()
			closeErr := reservation.close()
			return nil, retainedReplaceHandle{}, fmt.Errorf("protect retained atomic temporary file: %w",
				errors.Join(securityErr, cleanupErr, closeErr))
		}
		process := windows.CurrentProcess()
		var retained windows.Handle
		if err := windows.DuplicateHandle(process, handle, process, &retained, 0, false, windows.DUPLICATE_SAME_ACCESS); err != nil {
			reservation := retainedReplaceHandle{handle: handle}
			cleanupErr := reservation.remove()
			closeErr := reservation.close()
			return nil, retainedReplaceHandle{}, fmt.Errorf("retain atomic temporary handle: %w",
				errors.Join(err, cleanupErr, closeErr))
		}
		file := os.NewFile(uintptr(handle), name)
		if file == nil {
			reservation := retainedReplaceHandle{handle: retained}
			cleanupErr := reservation.remove()
			closeErr := errors.Join(windows.CloseHandle(handle), reservation.close())
			return nil, retainedReplaceHandle{}, errors.Join(
				errors.New("wrap retained atomic temporary handle"), cleanupErr, closeErr)
		}
		return file, retainedReplaceHandle{handle: retained}, nil
	}
	return nil, retainedReplaceHandle{}, errors.New("create retained atomic temporary file: too many name collisions")
}

func validateRetainedTempSecurity(handle windows.Handle, userSID string) error {
	descriptor, err := windows.GetSecurityInfo(handle, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	owner, _, err := descriptor.Owner()
	if err != nil {
		return err
	}
	if owner == nil || owner.String() != userSID {
		return errors.New("atomic temporary file owner differs from its effective creator")
	}
	control, _, err := descriptor.Control()
	if err != nil {
		return err
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		return errors.New("atomic temporary file DACL is not protected")
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		return err
	}
	return validateRetainedTempDACL(dacl, userSID)
}

func validateRetainedTempDACL(dacl *windows.ACL, userSID string) error {
	if dacl == nil {
		return errors.New("atomic temporary file has a null DACL")
	}
	seenUser, seenSystem := false, userSID == localSystemSID
	requiredAccess := windows.ACCESS_MASK(windows.FILE_GENERIC_READ | windows.FILE_GENERIC_WRITE |
		windows.DELETE | windows.READ_CONTROL | windows.WRITE_DAC | windows.WRITE_OWNER)
	for index := uint32(0); index < uint32(dacl.AceCount); index++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, index, &ace); err != nil {
			return err
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || ace.Header.AceFlags&windows.INHERITED_ACE != 0 {
			return errors.New("atomic temporary file DACL contains an unexpected ACE")
		}
		if ace.Mask&requiredAccess != requiredAccess {
			return errors.New("atomic temporary file DACL grants insufficient creator or system access")
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart)).String()
		switch sid {
		case userSID:
			seenUser = true
		case localSystemSID:
			seenSystem = true
		default:
			return fmt.Errorf("atomic temporary file DACL grants unexpected identity %s", sid)
		}
	}
	if !seenUser || !seenSystem {
		return errors.New("atomic temporary file DACL lacks its required owner or system ACE")
	}
	return nil
}

func (handle retainedReplaceHandle) valid() bool {
	return handle.handle != 0 && handle.handle != windows.InvalidHandle
}

func (handle retainedReplaceHandle) replace(destination string) error {
	return replaceExistingHandle(handle.handle, destination)
}

func (handle retainedReplaceHandle) remove() error {
	flags := uint32(windows.FILE_DISPOSITION_DELETE | windows.FILE_DISPOSITION_POSIX_SEMANTICS |
		windows.FILE_DISPOSITION_IGNORE_READONLY_ATTRIBUTE)
	err := windows.SetFileInformationByHandle(handle.handle, windows.FileDispositionInfoEx,
		(*byte)(unsafe.Pointer(&flags)), uint32(unsafe.Sizeof(flags)))
	if err == nil {
		return nil
	}
	if !errors.Is(err, windows.ERROR_INVALID_PARAMETER) && !errors.Is(err, windows.ERROR_NOT_SUPPORTED) &&
		!errors.Is(err, windows.ERROR_INVALID_FUNCTION) {
		return err
	}
	deleteFile := byte(1)
	return windows.SetFileInformationByHandle(handle.handle, windows.FileDispositionInfo,
		&deleteFile, uint32(unsafe.Sizeof(deleteFile)))
}

func (handle retainedReplaceHandle) close() error {
	if !handle.valid() {
		return nil
	}
	return windows.CloseHandle(handle.handle)
}
