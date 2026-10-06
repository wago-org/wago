//go:build windows

package build

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/command"
	"golang.org/x/sys/windows"
)

func TestBuildPreservesSimpleWindowsOutputAttributes(t *testing.T) {
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
			if err := os.WriteFile(target, []byte("existing artifact"), 0o600); err != nil {
				t.Fatal(err)
			}
			want := uint32(windows.FILE_ATTRIBUTE_HIDDEN | windows.FILE_ATTRIBUTE_SYSTEM |
				windows.FILE_ATTRIBUTE_TEMPORARY | windows.FILE_ATTRIBUTE_NOT_CONTENT_INDEXED)
			setWindowsTestFileAttributes(t, target, windowsTestFileAttributes(t, target)|want)
			// Some compatibility filesystems accept but do not retain every DOS
			// attribute. Assert exactly the subset the underlying filesystem retained.
			want &= windowsTestFileAttributes(t, target)
			if want == 0 {
				t.Skip("filesystem did not retain any persistent DOS test attributes")
			}
			t.Cleanup(func() {
				setWindowsTestFileAttributesBestEffort(target, windows.FILE_ATTRIBUTE_NORMAL)
			})

			output := target
			if throughSymlink {
				output = filepath.Join(dir, "output.wago")
				if err := os.Symlink(filepath.Base(target), output); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
				requireTestSymlink(t, output)
			}
			Command(testEnvironment{}).Run(command.NewContext(
				[]string{input}, map[string]string{"output": output}, nil,
			))

			artifact, err := os.ReadFile(target)
			if err != nil {
				t.Fatal(err)
			}
			if !wago.IsCompiled(artifact) {
				t.Fatalf("output is not a compiled artifact: %x", artifact)
			}
			if got := windowsTestFileAttributes(t, target) & want; got != want {
				t.Fatalf("persistent attributes = %#x, want %#x", got, want)
			}
			if throughSymlink {
				requireTestSymlink(t, output)
			}
		})
	}
}

func TestBuildRejectsReadOnlyWindowsOutputBeforeStaging(t *testing.T) {
	if os.Getenv("WAGO_BUILD_READONLY_CHILD") == "1" {
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
			setWindowsTestFileAttributes(t, target,
				windowsTestFileAttributes(t, target)|windows.FILE_ATTRIBUTE_READONLY)
			t.Cleanup(func() {
				setWindowsTestFileAttributesBestEffort(target,
					windowsTestFileAttributesBestEffort(target)&^windows.FILE_ATTRIBUTE_READONLY)
			})
			output := target
			if throughSymlink {
				output = filepath.Join(dir, "output.wago")
				if err := os.Symlink(filepath.Base(target), output); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
				requireTestSymlink(t, output)
			}

			child := exec.Command(os.Args[0], "-test.run=^TestBuildRejectsReadOnlyWindowsOutputBeforeStaging$")
			child.Env = append(os.Environ(),
				"WAGO_BUILD_READONLY_CHILD=1",
				"WAGO_BUILD_INPUT="+input,
				"WAGO_BUILD_OUTPUT="+output,
			)
			combined, err := child.CombinedOutput()
			if err == nil || !bytes.Contains(combined, []byte("read-only attribute")) {
				t.Fatalf("read-only output error = %v: %s", err, combined)
			}
			got, err := os.ReadFile(target)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, original) {
				t.Fatalf("rejected build changed artifact to %q", got)
			}
			if windowsTestFileAttributes(t, target)&windows.FILE_ATTRIBUTE_READONLY == 0 {
				t.Fatal("rejected build cleared the read-only attribute")
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), ".wago-atomic-") {
					t.Fatalf("read-only rejection left staging file %s", entry.Name())
				}
			}
			if throughSymlink {
				requireTestSymlink(t, output)
			}
		})
	}
}

func TestBuildRejectsCompressedWindowsOutput(t *testing.T) {
	if os.Getenv("WAGO_BUILD_COMPRESSED_CHILD") == "1" {
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
			compressWindowsTestFile(t, target)
			output := target
			if throughSymlink {
				output = filepath.Join(dir, "output.wago")
				if err := os.Symlink(filepath.Base(target), output); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
				requireTestSymlink(t, output)
			}

			child := exec.Command(os.Args[0], "-test.run=^TestBuildRejectsCompressedWindowsOutput$")
			child.Env = append(os.Environ(),
				"WAGO_BUILD_COMPRESSED_CHILD=1",
				"WAGO_BUILD_INPUT="+input,
				"WAGO_BUILD_OUTPUT="+output,
			)
			combined, err := child.CombinedOutput()
			if err == nil || !bytes.Contains(combined, []byte("unsupported Windows file attributes")) {
				t.Fatalf("compressed output error = %v: %s", err, combined)
			}
			got, err := os.ReadFile(target)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, original) {
				t.Fatalf("rejected build changed artifact to %q", got)
			}
			if windowsTestFileAttributes(t, target)&windows.FILE_ATTRIBUTE_COMPRESSED == 0 {
				t.Fatal("rejected build cleared compression")
			}
			if throughSymlink {
				requireTestSymlink(t, output)
			}
		})
	}
}

func compressWindowsTestFile(t *testing.T, path string) {
	t.Helper()
	pathPointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(pathPointer, windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Skipf("file compression unavailable: %v", err)
	}
	defer windows.CloseHandle(handle)
	compressionFormat := uint16(1) // COMPRESSION_FORMAT_DEFAULT
	var returned uint32
	if err := windows.DeviceIoControl(handle, windows.FSCTL_SET_COMPRESSION,
		(*byte)(unsafe.Pointer(&compressionFormat)), uint32(unsafe.Sizeof(compressionFormat)),
		nil, 0, &returned, nil); err != nil {
		t.Skipf("file compression unavailable: %v", err)
	}
	if windowsTestFileAttributes(t, path)&windows.FILE_ATTRIBUTE_COMPRESSED == 0 {
		t.Skip("filesystem did not mark the test file compressed")
	}
}

func setWindowsTestFileAttributes(t *testing.T, path string, attributes uint32) {
	t.Helper()
	pathPointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetFileAttributes(pathPointer, attributes); err != nil {
		t.Fatal(err)
	}
}

func setWindowsTestFileAttributesBestEffort(path string, attributes uint32) {
	pathPointer, err := windows.UTF16PtrFromString(path)
	if err == nil {
		_ = windows.SetFileAttributes(pathPointer, attributes)
	}
}

func windowsTestFileAttributesBestEffort(path string) uint32 {
	pathPointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return windows.FILE_ATTRIBUTE_NORMAL
	}
	attributes, err := windows.GetFileAttributes(pathPointer)
	if err != nil {
		return windows.FILE_ATTRIBUTE_NORMAL
	}
	return attributes
}
