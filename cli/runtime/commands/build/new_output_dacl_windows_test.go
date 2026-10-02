//go:build windows

package build

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wago-org/wago/cli/internal/command"
	"golang.org/x/sys/windows"
)

func TestBuildNewWindowsOutputInheritsParentDACL(t *testing.T) {
	dir := t.TempDir()
	setWindowsBuildReadableParentDACL(t, dir)
	input := filepath.Join(dir, "input.wasm")
	if err := os.WriteFile(input, []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0}, 0o600); err != nil {
		t.Fatal(err)
	}
	ordinary := filepath.Join(dir, "ordinary.wago")
	if err := os.WriteFile(ordinary, []byte("ordinary"), 0o600); err != nil {
		t.Fatal(err)
	}
	wantSecurity := windowsNewOutputSecurityDescriptor(t, ordinary)
	output := filepath.Join(dir, "built.wago")

	Command(testEnvironment{}).Run(command.NewContext(
		[]string{input}, map[string]string{"output": output}, nil,
	))
	if got := windowsNewOutputSecurityDescriptor(t, output); got != wantSecurity {
		t.Fatalf("new output security descriptor = %q, want inherited %q", got, wantSecurity)
	}
}

func setWindowsBuildReadableParentDACL(t *testing.T, path string) {
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
}

func windowsNewOutputSecurityDescriptor(t *testing.T, path string) string {
	t.Helper()
	descriptor, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.GROUP_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	return descriptor.String()
}
