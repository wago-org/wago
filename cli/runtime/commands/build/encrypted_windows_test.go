//go:build windows

package build

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/cli/internal/command"
	"golang.org/x/sys/windows"
)

var encryptFileW = windows.NewLazySystemDLL("advapi32.dll").NewProc("EncryptFileW")

func TestBuildRejectsEFSOutput(t *testing.T) {
	if os.Getenv("WAGO_BUILD_EFS_CHILD") == "1" {
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
			encryptWindowsTestFile(t, target)
			output := target
			if throughSymlink {
				output = filepath.Join(dir, "output.wago")
				if err := os.Symlink(filepath.Base(target), output); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
				requireTestSymlink(t, output)
			}

			child := exec.Command(os.Args[0], "-test.run=^TestBuildRejectsEFSOutput$")
			child.Env = append(os.Environ(),
				"WAGO_BUILD_EFS_CHILD=1",
				"WAGO_BUILD_INPUT="+input,
				"WAGO_BUILD_OUTPUT="+output,
			)
			combined, err := child.CombinedOutput()
			if err == nil || !bytes.Contains(combined, []byte("EFS-encrypted")) {
				t.Fatalf("encrypted output error = %v: %s", err, combined)
			}
			got, err := os.ReadFile(target)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, original) {
				t.Fatalf("rejected build changed artifact to %q", got)
			}
			if attributes := windowsTestFileAttributes(t, target); attributes&windows.FILE_ATTRIBUTE_ENCRYPTED == 0 {
				t.Fatal("rejected build cleared EFS encryption")
			}
			if throughSymlink {
				requireTestSymlink(t, output)
			}
		})
	}
}

func encryptWindowsTestFile(t *testing.T, path string) {
	t.Helper()
	if err := encryptFileW.Find(); err != nil {
		t.Skipf("EFS unavailable: %v", err)
	}
	pathPointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	result, _, callErr := encryptFileW.Call(uintptr(unsafe.Pointer(pathPointer)))
	if result == 0 {
		t.Skipf("EFS unavailable: %v", callErr)
	}
	if attributes := windowsTestFileAttributes(t, path); attributes&windows.FILE_ATTRIBUTE_ENCRYPTED == 0 {
		t.Skip("EFS did not mark the test file encrypted")
	}
}

func windowsTestFileAttributes(t *testing.T, path string) uint32 {
	t.Helper()
	pathPointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	attributes, err := windows.GetFileAttributes(pathPointer)
	if err != nil {
		t.Fatal(err)
	}
	return attributes
}
