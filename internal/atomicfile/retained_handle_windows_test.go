//go:build windows

package atomicfile

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestReplaceFileRetainedTempDoesNotInheritReadableDACL(t *testing.T) {
	dir := t.TempDir()
	setWindowsReadableAtomicTestParentDACL(t, dir)
	destination := filepath.Join(dir, "artifact.wago")
	options := Options{Mode: 0o600}
	setOptionalAtomicfileTestOption(&options, "ModeSet", true)
	setOptionalRetainReplaceHandle(&options)

	callbackReached := false
	err := ReplaceFile(destination, options, func(writer io.Writer) error {
		callbackReached = true
		file, ok := writer.(*os.File)
		if !ok {
			return errors.New("unexpected non-file writer")
		}
		assertWindowsRetainedTempDACL(t, windows.Handle(file.Fd()))
		_, err := io.WriteString(writer, "complete artifact")
		return err
	})
	if err != nil {
		if !callbackReached && strings.Contains(err.Error(), "protect retained atomic temporary file") {
			assertNoRetainedHandleTemps(t, dir)
			t.Skipf("filesystem cannot enforce a protected staging DACL: %v", err)
		}
		t.Fatal(err)
	}
}

func TestReplaceFileRetainsDeleteAuthorizationAfterDACLChange(t *testing.T) {
	dir := t.TempDir()
	setWindowsAtomicTestParentDACL(t, dir)
	destination := filepath.Join(dir, "artifact.wago")
	t.Cleanup(func() { setWindowsAtomicTestFullControlBestEffort(destination) })
	options := Options{Mode: 0o600, Sync: true}
	setOptionalAtomicfileTestOption(&options, "ModeSet", true)
	setOptionalRetainReplaceHandle(&options)

	err := ReplaceFile(destination, options, func(writer io.Writer) error {
		if _, err := io.WriteString(writer, "complete artifact"); err != nil {
			return err
		}
		file, ok := writer.(*os.File)
		if !ok {
			return errors.New("unexpected non-file writer")
		}
		setWindowsNoDeleteDACL(t, file.Name())
		return nil
	})
	if err != nil {
		t.Fatalf("replace after restrictive DACL = %v", err)
	}
	if got, err := os.ReadFile(destination); err != nil || string(got) != "complete artifact" {
		t.Fatalf("published destination = %q, %v", got, err)
	}
}

func TestReplaceFileRetainedHandleCleansRestrictiveDACLTemp(t *testing.T) {
	dir := t.TempDir()
	setWindowsAtomicTestParentDACL(t, dir)
	destination := filepath.Join(dir, "artifact.wago")
	options := Options{Mode: 0o600}
	setOptionalAtomicfileTestOption(&options, "ModeSet", true)
	setOptionalAtomicfileTestOption(&options, "BeforeReplace", func(string) error {
		return errors.New("reject publication")
	})
	setOptionalRetainReplaceHandle(&options)

	err := ReplaceFile(destination, options, func(writer io.Writer) error {
		if _, err := io.WriteString(writer, "complete artifact"); err != nil {
			return err
		}
		file, ok := writer.(*os.File)
		if !ok {
			return errors.New("unexpected non-file writer")
		}
		setWindowsNoDeleteDACL(t, file.Name())
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "reject publication") {
		t.Fatalf("rejected replacement = %v", err)
	}
	assertNoRetainedHandleTemps(t, dir)
}

func setOptionalRetainReplaceHandle(options *Options) {
	// Reflection keeps this behavioral regression runnable in the red commit,
	// where the production opt-in does not exist and the unsafe reopen path runs.
	setOptionalAtomicfileTestOption(options, "RetainReplaceHandle", true)
}

func setOptionalAtomicfileTestOption(options *Options, name string, value any) {
	field := reflect.ValueOf(options).Elem().FieldByName(name)
	if field.IsValid() {
		field.Set(reflect.ValueOf(value))
	}
}

func setWindowsNoDeleteDACL(t *testing.T, path string) {
	t.Helper()
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	trustee := windows.TRUSTEE{
		TrusteeForm:  windows.TRUSTEE_IS_SID,
		TrusteeType:  windows.TRUSTEE_IS_USER,
		TrusteeValue: windows.TrusteeValueFromSID(user.User.Sid),
	}
	acl, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{
		{AccessPermissions: windows.DELETE, AccessMode: windows.DENY_ACCESS, Trustee: trustee},
		{
			AccessPermissions: windows.FILE_GENERIC_READ | windows.FILE_GENERIC_WRITE | windows.READ_CONTROL | windows.WRITE_DAC,
			AccessMode:        windows.GRANT_ACCESS,
			Trustee:           trustee,
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, acl, nil); err != nil {
		t.Fatal(err)
	}
}

func setWindowsAtomicTestParentDACL(t *testing.T, path string) {
	t.Helper()
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	trustee := windows.TRUSTEE{
		TrusteeForm:  windows.TRUSTEE_IS_SID,
		TrusteeType:  windows.TRUSTEE_IS_USER,
		TrusteeValue: windows.TrusteeValueFromSID(user.User.Sid),
	}
	acl, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{
		{
			AccessPermissions: windows.FILE_GENERIC_READ | windows.FILE_GENERIC_WRITE | windows.READ_CONTROL | windows.WRITE_DAC,
			AccessMode:        windows.GRANT_ACCESS,
			Trustee:           trustee,
		},
		{
			AccessPermissions: windows.GENERIC_ALL,
			AccessMode:        windows.GRANT_ACCESS,
			Inheritance:       windows.SUB_OBJECTS_ONLY_INHERIT | windows.INHERIT_ONLY,
			Trustee:           trustee,
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, acl, nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { setWindowsAtomicTestFullControlBestEffort(path) })
}

func setWindowsReadableAtomicTestParentDACL(t *testing.T, path string) {
	t.Helper()
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	everyone, err := windows.CreateWellKnownSid(windows.WinWorldSid)
	if err != nil {
		t.Fatal(err)
	}
	acl, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{
		{
			AccessPermissions: windows.GENERIC_ALL,
			AccessMode:        windows.GRANT_ACCESS,
			Inheritance:       windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT,
			Trustee: windows.TRUSTEE{TrusteeForm: windows.TRUSTEE_IS_SID,
				TrusteeType: windows.TRUSTEE_IS_USER, TrusteeValue: windows.TrusteeValueFromSID(user.User.Sid)},
		},
		{
			AccessPermissions: windows.FILE_GENERIC_READ,
			AccessMode:        windows.GRANT_ACCESS,
			Inheritance:       windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT,
			Trustee: windows.TRUSTEE{TrusteeForm: windows.TRUSTEE_IS_SID,
				TrusteeType: windows.TRUSTEE_IS_WELL_KNOWN_GROUP, TrusteeValue: windows.TrusteeValueFromSID(everyone)},
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, acl, nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { setWindowsAtomicTestFullControlBestEffort(path) })
}

func assertWindowsRetainedTempDACL(t *testing.T, handle windows.Handle) {
	t.Helper()
	descriptor, err := windows.GetSecurityInfo(handle, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	control, _, err := descriptor.Control()
	if err != nil {
		t.Fatal(err)
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		t.Fatalf("retained temporary file DACL is not protected: control=%#x descriptor=%q", control, descriptor.String())
	}
	owner, _, err := descriptor.Owner()
	if err != nil {
		t.Fatal(err)
	}
	user, err := windows.GetCurrentThreadEffectiveToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	if owner == nil || !windows.EqualSid(owner, user.User.Sid) {
		t.Fatalf("retained temporary file owner = %v, want effective user %v", owner, user.User.Sid)
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		t.Fatal(err)
	}
	everyone, err := windows.CreateWellKnownSid(windows.WinWorldSid)
	if err != nil {
		t.Fatal(err)
	}
	for index := uint32(0); index < uint32(dacl.AceCount); index++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, index, &ace); err != nil {
			t.Fatal(err)
		}
		if ace.Header.AceFlags&windows.INHERITED_ACE != 0 {
			t.Fatalf("retained temporary file inherited ACE %d", index)
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if windows.EqualSid(sid, everyone) {
			t.Fatalf("retained temporary file grants Everyone access in ACE %d", index)
		}
	}
}

func setWindowsAtomicTestFullControlBestEffort(path string) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return
	}
	acl, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{{
		AccessPermissions: windows.GENERIC_ALL,
		AccessMode:        windows.GRANT_ACCESS,
		Inheritance:       windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_USER,
			TrusteeValue: windows.TrusteeValueFromSID(user.User.Sid),
		},
	}}, nil)
	if err == nil {
		_ = windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
			windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
			nil, nil, acl, nil)
	}
}

func assertNoRetainedHandleTemps(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".wago-atomic-") {
			// Make a red-run leak removable so TempDir cleanup does not obscure the
			// assertion with a second failure.
			path := filepath.Join(dir, entry.Name())
			user, userErr := windows.GetCurrentProcessToken().GetTokenUser()
			if userErr == nil {
				acl, aclErr := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{{
					AccessPermissions: windows.GENERIC_ALL,
					AccessMode:        windows.GRANT_ACCESS,
					Trustee: windows.TRUSTEE{TrusteeForm: windows.TRUSTEE_IS_SID,
						TrusteeType: windows.TRUSTEE_IS_USER, TrusteeValue: windows.TrusteeValueFromSID(user.User.Sid)},
				}}, nil)
				if aclErr == nil {
					_ = windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
						windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
						nil, nil, acl, nil)
				}
			}
			_ = os.Remove(path)
			t.Fatalf("replacement left restrictive staging file %s", entry.Name())
		}
	}
}
