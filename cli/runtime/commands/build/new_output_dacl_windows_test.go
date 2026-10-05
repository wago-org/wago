//go:build windows

package build

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/command"
	"golang.org/x/sys/windows"
)

func TestBuildNewWindowsOutputPreservesWriteOnlyInheritance(t *testing.T) {
	const childEnv = "WAGO_WINDOWS_WRITE_ONLY_BUILD_CHILD"
	if os.Getenv(childEnv) == "1" {
		Command(testEnvironment{}).Run(command.NewContext(
			[]string{os.Getenv("WAGO_WINDOWS_WRITE_ONLY_INPUT")},
			map[string]string{"output": os.Getenv("WAGO_WINDOWS_WRITE_ONLY_OUTPUT")}, nil,
		))
		return
	}

	dir := t.TempDir()
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	trustee := windows.TRUSTEE{TrusteeForm: windows.TRUSTEE_IS_SID,
		TrusteeType: windows.TRUSTEE_IS_USER, TrusteeValue: windows.TrusteeValueFromSID(user.User.Sid)}
	parentOnly := windows.EXPLICIT_ACCESS{AccessPermissions: windows.GENERIC_ALL,
		AccessMode: windows.GRANT_ACCESS, Trustee: trustee}
	childOnly := windows.EXPLICIT_ACCESS{
		// Keep metadata inspection and exact-handle cleanup rights, but grant no
		// data-read rights. Generic-read denial is qualified separately below.
		AccessPermissions: windows.FILE_GENERIC_WRITE | windows.READ_CONTROL | windows.WRITE_DAC | windows.DELETE,
		AccessMode:        windows.GRANT_ACCESS, Inheritance: windows.OBJECT_INHERIT_ACE | windows.INHERIT_ONLY_ACE,
		Trustee: trustee,
	}
	acl, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{parentOnly, childOnly}, nil)
	if err != nil {
		t.Fatal(err)
	}
	old, err := windows.GetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	oldACL, _, err := old.DACL()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT,
			windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
			nil, nil, oldACL, nil); err != nil {
			t.Errorf("restore temporary parent DACL: %v", err)
		}
	})
	if err := windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, acl, nil); err != nil {
		t.Fatal(err)
	}
	control := filepath.Join(dir, "ordinary.wago")
	if err := os.WriteFile(control, []byte("ordinary"), 0o600); err != nil {
		t.Fatalf("ordinary write-only creation: %v", err)
	}
	writeOnly, err := os.OpenFile(control, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("reopen ordinary file for writing: %v", err)
	}
	if err := writeOnly.Close(); err != nil {
		t.Fatal(err)
	}
	assertReadDenied := func(path string) {
		t.Helper()
		file, err := os.Open(path)
		if err == nil {
			_ = file.Close()
			if path == control {
				t.Skip("filesystem does not enforce write-only inherited ACL")
			}
			t.Fatal("published output unexpectedly grants generic-read access")
		}
		if !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			t.Fatalf("read denial for %s: %v", path, err)
		}
	}
	assertReadDenied(control)
	wantSecurity := windowsNewOutputSecurityDescriptor(t, control)
	input := filepath.Join(t.TempDir(), "input.wasm")
	if err := os.WriteFile(input, []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0}, 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "built.wago")
	child := exec.Command(os.Args[0], "-test.run=^TestBuildNewWindowsOutputPreservesWriteOnlyInheritance$")
	child.Env = append(os.Environ(), childEnv+"=1", "WAGO_WINDOWS_WRITE_ONLY_INPUT="+input,
		"WAGO_WINDOWS_WRITE_ONLY_OUTPUT="+output)
	if combined, err := child.CombinedOutput(); err != nil {
		t.Fatalf("write-only inherited ACL build: %v: %s", err, combined)
	}
	if got := windowsNewOutputSecurityDescriptor(t, output); got != wantSecurity {
		t.Fatalf("new output security descriptor = %q, want inherited %q", got, wantSecurity)
	}
	assertReadDenied(output)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".wago-") {
			t.Fatalf("publication entry remains: %s", entry.Name())
		}
	}
	// Grant access only after qualifying inheritance, so byte validation cannot
	// mask a publication that changed the caller's intended access policy.
	readable, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{parentOnly}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(output, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, readable, nil); err != nil {
		t.Fatal(err)
	}
	artifact, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if !wago.IsCompiled(artifact) {
		t.Fatal("write-only output is not a compiled artifact")
	}
	if _, err := wago.LoadTrustedArtifact(artifact); err != nil {
		t.Fatalf("load write-only output: %v", err)
	}
}

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
