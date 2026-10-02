//go:build windows

package build

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
	"unsafe"

	"github.com/wago-org/wago/cli/internal/command"
	"golang.org/x/sys/windows"
)

func TestBuildRejectsWindowsAlternateDataStreams(t *testing.T) {
	if os.Getenv("WAGO_BUILD_ADS_CHILD") == "1" {
		Command(testEnvironment{}).Run(command.NewContext(
			[]string{os.Getenv("WAGO_BUILD_INPUT")},
			map[string]string{"output": os.Getenv("WAGO_BUILD_OUTPUT")}, nil,
		))
		return
	}
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
			original := []byte("existing artifact")
			if err := os.WriteFile(target, original, 0o600); err != nil {
				t.Fatal(err)
			}
			streamPath := target + ":provenance:$DATA"
			streamData := []byte("trusted provenance")
			if err := os.WriteFile(streamPath, streamData, 0o600); err != nil {
				t.Skipf("named streams unavailable: %v", err)
			}
			streams, err := windowsTestStreamNames(target)
			if err != nil {
				t.Skipf("named-stream enumeration unavailable: %v", err)
			}
			if !containsWindowsTestStream(streams, ":provenance:$DATA") {
				t.Skipf("filesystem did not create a named stream: %q", streams)
			}
			output := target
			if throughSymlink {
				output = filepath.Join(dir, "output.wago")
				if err := os.Symlink(filepath.Base(target), output); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
				requireTestSymlink(t, output)
			}

			child := exec.Command(os.Args[0], "-test.run=^TestBuildRejectsWindowsAlternateDataStreams$")
			child.Env = append(os.Environ(),
				"WAGO_BUILD_ADS_CHILD=1", "WAGO_BUILD_INPUT="+input, "WAGO_BUILD_OUTPUT="+output,
			)
			combined, err := child.CombinedOutput()
			if err == nil || !bytes.Contains(combined, []byte("alternate data stream")) {
				t.Fatalf("named-stream output error = %v: %s", err, combined)
			}
			if got, err := os.ReadFile(target); err != nil || !bytes.Equal(got, original) {
				t.Fatalf("rejected artifact = %q, %v", got, err)
			}
			if got, err := os.ReadFile(streamPath); err != nil || !bytes.Equal(got, streamData) {
				t.Fatalf("rejected named stream = %q, %v", got, err)
			}
		})
	}
}

func TestBuildRejectsWindowsOutputWithoutWriteData(t *testing.T) {
	if os.Getenv("WAGO_BUILD_WRITE_DATA_CHILD") == "1" {
		Command(testEnvironment{}).Run(command.NewContext(
			[]string{os.Getenv("WAGO_BUILD_INPUT")},
			map[string]string{"output": os.Getenv("WAGO_BUILD_OUTPUT")}, nil,
		))
		return
	}
	dir := t.TempDir()
	input := filepath.Join(dir, "input.wasm")
	if err := os.WriteFile(input, []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0}, 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "artifact.wago")
	original := []byte("existing artifact")
	if err := os.WriteFile(target, original, 0o600); err != nil {
		t.Fatal(err)
	}
	setWindowsTestCurrentUserDACL(t, target,
		windows.FILE_GENERIC_READ|windows.READ_CONTROL|windows.WRITE_DAC|windows.DELETE)
	if windowsTestCanOpenForWriteData(target) {
		t.Skip("filesystem does not enforce the test DACL's missing FILE_WRITE_DATA right")
	}

	child := exec.Command(os.Args[0], "-test.run=^TestBuildRejectsWindowsOutputWithoutWriteData$")
	child.Env = append(os.Environ(),
		"WAGO_BUILD_WRITE_DATA_CHILD=1", "WAGO_BUILD_INPUT="+input, "WAGO_BUILD_OUTPUT="+target,
	)
	combined, err := child.CombinedOutput()
	if err == nil || !bytes.Contains(combined, []byte("write existing output")) {
		t.Fatalf("write-denied output error = %v: %s", err, combined)
	}
	if got, err := os.ReadFile(target); err != nil || !bytes.Equal(got, original) {
		t.Fatalf("rejected artifact = %q, %v", got, err)
	}
}

func TestBuildRejectsWindowsOutputWithoutGenericWrite(t *testing.T) {
	if os.Getenv("WAGO_BUILD_GENERIC_WRITE_CHILD") == "1" {
		Command(testEnvironment{}).Run(command.NewContext(
			[]string{os.Getenv("WAGO_BUILD_INPUT")},
			map[string]string{"output": os.Getenv("WAGO_BUILD_OUTPUT")}, nil,
		))
		return
	}
	dir := t.TempDir()
	input := filepath.Join(dir, "input.wasm")
	if err := os.WriteFile(input, []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0}, 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "artifact.wago")
	original := []byte("existing artifact")
	if err := os.WriteFile(target, original, 0o600); err != nil {
		t.Fatal(err)
	}
	// Grant content writes but deliberately omit FILE_WRITE_ATTRIBUTES, one of
	// the rights in GENERIC_WRITE requested by the former os.WriteFile path.
	setWindowsTestCurrentUserDACL(t, target,
		windows.FILE_GENERIC_READ|windows.FILE_WRITE_DATA|windows.FILE_APPEND_DATA|
			windows.FILE_WRITE_EA|windows.SYNCHRONIZE|windows.WRITE_DAC|windows.DELETE)
	if !windowsTestCanOpen(target, windows.FILE_WRITE_DATA) {
		t.Skip("filesystem did not retain the FILE_WRITE_DATA grant")
	}
	if windowsTestCanOpen(target, windows.GENERIC_WRITE) {
		t.Skip("filesystem does not enforce the test DACL's missing FILE_WRITE_ATTRIBUTES right")
	}

	child := exec.Command(os.Args[0], "-test.run=^TestBuildRejectsWindowsOutputWithoutGenericWrite$")
	child.Env = append(os.Environ(),
		"WAGO_BUILD_GENERIC_WRITE_CHILD=1", "WAGO_BUILD_INPUT="+input, "WAGO_BUILD_OUTPUT="+target,
	)
	combined, err := child.CombinedOutput()
	if err == nil || !bytes.Contains(combined, []byte("write existing output")) {
		t.Fatalf("generic-write-denied output error = %v: %s", err, combined)
	}
	if got, err := os.ReadFile(target); err != nil || !bytes.Equal(got, original) {
		t.Fatalf("rejected artifact = %q, %v", got, err)
	}
}

func TestBuildRejectsDistinctWindowsIntegrityLabel(t *testing.T) {
	if os.Getenv("WAGO_BUILD_INTEGRITY_CHILD") == "1" {
		Command(testEnvironment{}).Run(command.NewContext(
			[]string{os.Getenv("WAGO_BUILD_INPUT")},
			map[string]string{"output": os.Getenv("WAGO_BUILD_OUTPUT")}, nil,
		))
		return
	}
	dir := t.TempDir()
	input := filepath.Join(dir, "input.wasm")
	if err := os.WriteFile(input, []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0}, 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "artifact.wago")
	original := []byte("existing artifact")
	if err := os.WriteFile(target, original, 0o600); err != nil {
		t.Fatal(err)
	}
	setWindowsTestLowIntegrityLabel(t, target)
	wantLabel := windowsTestIntegrityDescriptor(t, target)

	child := exec.Command(os.Args[0], "-test.run=^TestBuildRejectsDistinctWindowsIntegrityLabel$")
	child.Env = append(os.Environ(),
		"WAGO_BUILD_INTEGRITY_CHILD=1", "WAGO_BUILD_INPUT="+input, "WAGO_BUILD_OUTPUT="+target,
	)
	combined, err := child.CombinedOutput()
	if err == nil || !bytes.Contains(combined, []byte("mandatory integrity label")) {
		t.Fatalf("integrity-labeled output error = %v: %s", err, combined)
	}
	if got, err := os.ReadFile(target); err != nil || !bytes.Equal(got, original) {
		t.Fatalf("rejected artifact = %q, %v", got, err)
	}
	if got := windowsTestIntegrityDescriptor(t, target); got != wantLabel {
		t.Fatalf("rejected integrity label = %q, want %q", got, wantLabel)
	}
}

func TestBuildRejectsDistinctWindowsAccessPolicy(t *testing.T) {
	if os.Getenv("WAGO_BUILD_ACCESS_POLICY_CHILD") == "1" {
		Command(testEnvironment{}).Run(command.NewContext(
			[]string{os.Getenv("WAGO_BUILD_INPUT")},
			map[string]string{"output": os.Getenv("WAGO_BUILD_OUTPUT")}, nil,
		))
		return
	}
	for _, test := range []struct {
		name        string
		sddl        string
		information windows.SECURITY_INFORMATION
	}{
		{
			name:        "resource-attribute",
			sddl:        `S:(RA;;;;;WD;("Project",TS,0,"Wago"))`,
			information: windows.ATTRIBUTE_SECURITY_INFORMATION,
		},
		{
			name:        "central-policy",
			sddl:        `S:(SP;;;;;S-1-17-1442530252-1178042555-1247349694-2318402534)`,
			information: windows.SCOPE_SECURITY_INFORMATION,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			input := filepath.Join(dir, "input.wasm")
			if err := os.WriteFile(input, []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0}, 0o600); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(dir, "artifact.wago")
			original := []byte("existing artifact")
			if err := os.WriteFile(target, original, 0o600); err != nil {
				t.Fatal(err)
			}
			setWindowsTestAccessPolicy(t, target, test.sddl, test.information)
			wantPolicy := windowsTestAccessPolicyDescriptor(t, target, test.information)

			child := exec.Command(os.Args[0], "-test.run=^TestBuildRejectsDistinctWindowsAccessPolicy$/^"+test.name+"$")
			child.Env = append(os.Environ(),
				"WAGO_BUILD_ACCESS_POLICY_CHILD=1", "WAGO_BUILD_INPUT="+input, "WAGO_BUILD_OUTPUT="+target,
			)
			combined, err := child.CombinedOutput()
			if err == nil || !bytes.Contains(combined, []byte("access policy")) {
				t.Fatalf("access-policy output error = %v: %s", err, combined)
			}
			if got, err := os.ReadFile(target); err != nil || !bytes.Equal(got, original) {
				t.Fatalf("rejected artifact = %q, %v", got, err)
			}
			if got := windowsTestAccessPolicyDescriptor(t, target, test.information); got != wantPolicy {
				t.Fatalf("rejected access policy = %q, want %q", got, wantPolicy)
			}
		})
	}
}

func setWindowsTestAccessPolicy(t *testing.T, path, sddl string, information windows.SECURITY_INFORMATION) {
	t.Helper()
	descriptor, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		t.Skipf("constructing Windows access-policy metadata is unavailable: %v", err)
	}
	sacl, _, err := descriptor.SACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		information, nil, nil, nil, sacl); err != nil {
		t.Skipf("setting Windows access-policy metadata is unavailable: %v", err)
	}
}

func windowsTestAccessPolicyDescriptor(t *testing.T, path string, information windows.SECURITY_INFORMATION) string {
	t.Helper()
	descriptor, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, information)
	if err != nil {
		t.Skipf("reading Windows access-policy metadata is unavailable: %v", err)
	}
	return descriptor.String()
}

func setWindowsTestCurrentUserDACL(t *testing.T, path string, permissions uint32) {
	t.Helper()
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	acl, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{{
		AccessPermissions: windows.ACCESS_MASK(permissions),
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
}

func windowsTestCanOpenForWriteData(path string) bool {
	return windowsTestCanOpen(path, windows.FILE_WRITE_DATA)
}

func windowsTestCanOpen(path string, access uint32) bool {
	pointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return false
	}
	handle, err := windows.CreateFile(pointer, access,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return false
	}
	_ = windows.CloseHandle(handle)
	return true
}

func setWindowsTestLowIntegrityLabel(t *testing.T, path string) {
	t.Helper()
	descriptor, err := windows.SecurityDescriptorFromString("S:(ML;;NW;;;LW)")
	if err != nil {
		t.Fatal(err)
	}
	sacl, _, err := descriptor.SACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.LABEL_SECURITY_INFORMATION, nil, nil, nil, sacl); err != nil {
		t.Skipf("setting a low mandatory-integrity label is unavailable: %v", err)
	}
}

func windowsTestIntegrityDescriptor(t *testing.T, path string) string {
	t.Helper()
	descriptor, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.LABEL_SECURITY_INFORMATION)
	if err != nil {
		t.Skipf("reading mandatory-integrity labels is unavailable: %v", err)
	}
	return descriptor.String()
}

func windowsTestStreamNames(path string) ([]string, error) {
	pointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(pointer, windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(handle)
	buffer := make([]byte, 64<<10)
	if err := windows.GetFileInformationByHandleEx(handle, windows.FileStreamInfo,
		&buffer[0], uint32(len(buffer))); err != nil {
		return nil, err
	}
	var names []string
	for offset := uint32(0); ; {
		if uint64(offset)+24 > uint64(len(buffer)) {
			return nil, errors.New("malformed FILE_STREAM_INFO")
		}
		next := *(*uint32)(unsafe.Pointer(&buffer[offset]))
		nameLength := *(*uint32)(unsafe.Pointer(&buffer[offset+4]))
		if nameLength%2 != 0 || uint64(offset)+24+uint64(nameLength) > uint64(len(buffer)) {
			return nil, errors.New("malformed FILE_STREAM_INFO name")
		}
		units := unsafe.Slice((*uint16)(unsafe.Pointer(&buffer[offset+24])), int(nameLength/2))
		names = append(names, string(utf16.Decode(units)))
		if next == 0 {
			return names, nil
		}
		if next < 24 || uint64(offset)+uint64(next) >= uint64(len(buffer)) {
			return nil, errors.New("malformed FILE_STREAM_INFO offset")
		}
		offset += next
	}
}

func containsWindowsTestStream(streams []string, name string) bool {
	for _, stream := range streams {
		if strings.EqualFold(stream, name) {
			return true
		}
	}
	return false
}
