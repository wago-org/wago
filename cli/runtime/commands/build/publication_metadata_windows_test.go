//go:build windows

package build

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wago-org/wago/internal/atomicfile"
	"golang.org/x/sys/windows"
)

func TestBuildPublicationBoundaryRejectsDACLChange(t *testing.T) {
	dir := t.TempDir()
	destination := filepath.Join(dir, "artifact.wago")
	if err := os.WriteFile(destination, []byte("original artifact"), 0o600); err != nil {
		t.Fatal(err)
	}
	expected := windowsBoundarySecurityDescriptor(t, destination)

	options := atomicfile.Options{Mode: 0o600, Sync: true, Hooks: &atomicfile.Hooks{
		Sync: func(file *os.File) error {
			if err := file.Sync(); err != nil {
				return err
			}
			setWindowsBoundaryDistinctDACL(t, destination)
			if windowsBoundarySecurityDescriptor(t, destination) == expected {
				t.Skip("filesystem did not retain a distinct test DACL")
			}
			return nil
		},
	}}
	setOptionalAtomicfileOption(&options, "BeforeReplace", func(path string) error {
		if windowsBoundarySecurityDescriptor(t, path) != expected {
			return errors.New("output metadata changed before publication")
		}
		return nil
	})

	err := atomicfile.ReplaceFile(destination, options, func(writer io.Writer) error {
		_, err := io.WriteString(writer, "new compiled artifact")
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "metadata changed before publication") {
		t.Fatalf("publication after DACL change = %v", err)
	}
	if got, err := os.ReadFile(destination); err != nil || string(got) != "original artifact" {
		t.Fatalf("destination after rejected publication = %q, %v", got, err)
	}
	if windowsBoundarySecurityDescriptor(t, destination) == expected {
		t.Fatal("concurrent DACL change was not retained")
	}
	assertNoAtomicBuildTemps(t, dir)
}

func setWindowsBoundaryDistinctDACL(t *testing.T, path string) {
	t.Helper()
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	acl, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{{
		AccessPermissions: windows.FILE_GENERIC_READ | windows.FILE_GENERIC_WRITE | windows.READ_CONTROL | windows.WRITE_DAC | windows.DELETE,
		AccessMode:        windows.GRANT_ACCESS,
		Inheritance:       windows.NO_INHERITANCE,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_USER,
			TrusteeValue: windows.TrusteeValueFromSID(user.User.Sid),
		},
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, acl, nil); err != nil {
		t.Fatal(err)
	}
}

func windowsBoundarySecurityDescriptor(t *testing.T, path string) string {
	t.Helper()
	descriptor, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.GROUP_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	return descriptor.String()
}
