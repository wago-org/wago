//go:build windows

package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/command"
	"golang.org/x/sys/windows"
)

func TestBuildRejectsEncryptedWindowsIdentity(t *testing.T) {
	err := validateBuildOutputPublication("artifact.wago", nil, buildFileIdentity{
		fileAttributes: windows.FILE_ATTRIBUTE_ENCRYPTED,
	})
	if err == nil || !strings.Contains(err.Error(), "EFS-encrypted") {
		t.Fatalf("encrypted output validation = %v", err)
	}
}

func TestBuildValidatesWindowsOutputAttributes(t *testing.T) {
	tests := []struct {
		name       string
		attributes uint32
		wantError  string
	}{
		{name: "normal", attributes: windows.FILE_ATTRIBUTE_NORMAL},
		{name: "archive", attributes: windows.FILE_ATTRIBUTE_ARCHIVE},
		{name: "preserved", attributes: windowsBuildOutputPreservedAttributes},
		{name: "read-only", attributes: windows.FILE_ATTRIBUTE_READONLY, wantError: "read-only attribute"},
		{name: "encrypted", attributes: windows.FILE_ATTRIBUTE_ENCRYPTED, wantError: "EFS-encrypted"},
		{name: "directory", attributes: windows.FILE_ATTRIBUTE_DIRECTORY, wantError: "unsupported Windows file attributes"},
		{name: "device", attributes: windows.FILE_ATTRIBUTE_DEVICE, wantError: "unsupported Windows file attributes"},
		{name: "sparse", attributes: windows.FILE_ATTRIBUTE_SPARSE_FILE, wantError: "unsupported Windows file attributes"},
		{name: "reparse", attributes: windows.FILE_ATTRIBUTE_REPARSE_POINT, wantError: "unsupported Windows file attributes"},
		{name: "compressed", attributes: windows.FILE_ATTRIBUTE_COMPRESSED, wantError: "unsupported Windows file attributes"},
		{name: "offline", attributes: windows.FILE_ATTRIBUTE_OFFLINE, wantError: "unsupported Windows file attributes"},
		{name: "integrity", attributes: windows.FILE_ATTRIBUTE_INTEGRITY_STREAM, wantError: "unsupported Windows file attributes"},
		{name: "virtual", attributes: windows.FILE_ATTRIBUTE_VIRTUAL, wantError: "unsupported Windows file attributes"},
		{name: "no-scrub", attributes: windows.FILE_ATTRIBUTE_NO_SCRUB_DATA, wantError: "unsupported Windows file attributes"},
		{name: "recall-on-open", attributes: windows.FILE_ATTRIBUTE_RECALL_ON_OPEN, wantError: "unsupported Windows file attributes"},
		{name: "pinned", attributes: 0x00080000, wantError: "unsupported Windows file attributes"},
		{name: "unpinned", attributes: 0x00100000, wantError: "unsupported Windows file attributes"},
		{name: "recall-on-data-access", attributes: windows.FILE_ATTRIBUTE_RECALL_ON_DATA_ACCESS, wantError: "unsupported Windows file attributes"},
		{name: "unknown", attributes: 0x80000000, wantError: "unsupported Windows file attributes"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateBuildOutputPublication("artifact.wago", nil, buildFileIdentity{
				fileAttributes: test.attributes,
			})
			if test.wantError == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("validation error = %v, want %q", err, test.wantError)
			}
		})
	}
}

func TestApplyBuildOutputAttributes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "staged.wago")
	if err := os.WriteFile(path, []byte("staged"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	current := windowsTestFileAttributes(t, path)
	preserved := uint32(windows.FILE_ATTRIBUTE_HIDDEN | windows.FILE_ATTRIBUTE_SYSTEM)
	if err := applyBuildOutputAttributes(windows.Handle(file.Fd()), current, preserved); err != nil {
		t.Fatal(err)
	}
	attributes := windowsTestFileAttributes(t, path)
	if attributes&preserved != preserved {
		t.Fatalf("staged attributes = %#x, want preserved bits %#x", attributes, preserved)
	}
	if current&windows.FILE_ATTRIBUTE_ARCHIVE != 0 && attributes&windows.FILE_ATTRIBUTE_ARCHIVE == 0 {
		t.Fatalf("staged attributes = %#x, lost write-derived archive bit", attributes)
	}
}

func TestBuildValidatesStagedWindowsAttributes(t *testing.T) {
	for _, attributes := range []uint32{
		windows.FILE_ATTRIBUTE_COMPRESSED,
		windows.FILE_ATTRIBUTE_SPARSE_FILE,
		windows.FILE_ATTRIBUTE_ENCRYPTED,
		windows.FILE_ATTRIBUTE_OFFLINE,
		windows.FILE_ATTRIBUTE_INTEGRITY_STREAM,
		windows.FILE_ATTRIBUTE_NO_SCRUB_DATA,
		windows.FILE_ATTRIBUTE_RECALL_ON_OPEN,
		0x00080000, // FILE_ATTRIBUTE_PINNED
		0x00100000, // FILE_ATTRIBUTE_UNPINNED
		windows.FILE_ATTRIBUTE_RECALL_ON_DATA_ACCESS,
		0x80000000,
	} {
		if err := validateStagedBuildOutputAttributes(attributes); err == nil {
			t.Fatalf("staged attributes %#x accepted", attributes)
		}
	}
	for _, attributes := range []uint32{
		windows.FILE_ATTRIBUTE_NORMAL,
		windows.FILE_ATTRIBUTE_ARCHIVE,
		windowsBuildOutputPreservedAttributes,
	} {
		if err := validateStagedBuildOutputAttributes(attributes); err != nil {
			t.Fatalf("staged attributes %#x rejected: %v", attributes, err)
		}
	}
}

func TestBuildOutputIdentityWithSharedWindowsHandle(t *testing.T) {
	for _, throughSymlink := range []bool{false, true} {
		name := "direct"
		if throughSymlink {
			name = "symlink-target"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "artifact.wago")
			if err := os.WriteFile(target, []byte("old artifact"), 0o600); err != nil {
				t.Fatal(err)
			}
			output := target
			if throughSymlink {
				output = filepath.Join(dir, "output.wago")
				if err := os.Symlink(filepath.Base(target), output); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
				requireTestSymlink(t, output)
			}
			handle := openSharedWindowsBuildOutput(t, target)
			defer windows.CloseHandle(handle)

			snapshot, err := inspectBuildOutput(output)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, _, _, err := snapshot.revalidate(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestBuildOutputMetadataWithSharedWindowsHandle(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "artifact.wago")
	if err := os.WriteFile(target, []byte("old artifact"), 0o600); err != nil {
		t.Fatal(err)
	}
	wantSecurity := setDistinctWindowsOutputDACL(t, target)
	handle := openSharedWindowsBuildOutput(t, target)
	defer windows.CloseHandle(handle)

	info, err := os.Lstat(target)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := captureBuildFileIdentity(target, false, info)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := captureBuildOutputMetadata(target, info, identity)
	if err != nil {
		t.Fatal(err)
	}
	staged, err := os.CreateTemp(dir, ".artifact.wago-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		staged.Close()
		os.Remove(staged.Name())
	})
	if err := applyBuildOutputMetadata(staged, metadata); err != nil {
		t.Fatal(err)
	}
	if got := windowsOutputSecurityDescriptor(t, staged.Name()); got != wantSecurity {
		t.Fatalf("staged security descriptor = %q, want %q", got, wantSecurity)
	}
}

func TestBuildWithSharedWindowsOutputHandle(t *testing.T) {
	for _, throughSymlink := range []bool{false, true} {
		name := "direct"
		if throughSymlink {
			name = "symlink-target"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			input := filepath.Join(dir, "input.wasm")
			if err := os.WriteFile(input, []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0}, 0o600); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(dir, "artifact.wago")
			if err := os.WriteFile(target, []byte("old artifact"), 0o600); err != nil {
				t.Fatal(err)
			}
			wantSecurity := setDistinctWindowsOutputDACL(t, target)
			output := target
			if throughSymlink {
				output = filepath.Join(dir, "output.wago")
				if err := os.Symlink(filepath.Base(target), output); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
				requireTestSymlink(t, output)
			}
			handle := openSharedWindowsBuildOutput(t, target)
			defer windows.CloseHandle(handle)

			Command(testEnvironment{}).Run(command.NewContext(
				[]string{input}, map[string]string{"output": output}, nil,
			))

			artifact, err := os.ReadFile(target)
			if err != nil {
				t.Fatal(err)
			}
			if !wago.IsCompiled(artifact) {
				t.Fatalf("shared output target is not a compiled artifact: %x", artifact)
			}
			if got := windowsOutputSecurityDescriptor(t, target); got != wantSecurity {
				t.Fatalf("output security descriptor = %q, want %q", got, wantSecurity)
			}
			if throughSymlink {
				requireTestSymlink(t, output)
			}
		})
	}
}

func setDistinctWindowsOutputDACL(t *testing.T, path string) string {
	t.Helper()
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	acl, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{{
		AccessPermissions: windows.GENERIC_ALL,
		AccessMode:        windows.GRANT_ACCESS,
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
	return windowsOutputSecurityDescriptor(t, path)
}

func windowsOutputSecurityDescriptor(t *testing.T, path string) string {
	t.Helper()
	descriptor, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.GROUP_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	return descriptor.String()
}

func openSharedWindowsBuildOutput(t *testing.T, path string) windows.Handle {
	t.Helper()
	pathPointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(pathPointer, windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	return handle
}
