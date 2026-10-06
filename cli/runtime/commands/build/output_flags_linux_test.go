//go:build linux

package build

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/wago-org/wago/cli/internal/command"
	"golang.org/x/sys/unix"
)

const linuxTestNodumpFlag = 0x40

func TestBuildRejectsPersistentLinuxOutputFlags(t *testing.T) {
	if os.Getenv("WAGO_BUILD_LINUX_FLAGS_CHILD") == "1" {
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
			setLinuxTestNodumpFlag(t, target)
			output := target
			if throughSymlink {
				output = filepath.Join(dir, "output.wago")
				if err := os.Symlink(filepath.Base(target), output); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
				requireTestSymlink(t, output)
			}

			child := exec.Command(os.Args[0], "-test.run=^TestBuildRejectsPersistentLinuxOutputFlags$")
			child.Env = append(os.Environ(),
				"WAGO_BUILD_LINUX_FLAGS_CHILD=1", "WAGO_BUILD_INPUT="+input, "WAGO_BUILD_OUTPUT="+output,
			)
			combined, err := child.CombinedOutput()
			if err == nil || !bytes.Contains(combined, []byte("persistent Linux file attributes")) {
				t.Fatalf("flagged output error = %v: %s", err, combined)
			}
			if got, err := os.ReadFile(target); err != nil || !bytes.Equal(got, original) {
				t.Fatalf("rejected artifact = %q, %v", got, err)
			}
			if flags, err := linuxTestFileFlags(target); err != nil || flags&linuxTestNodumpFlag == 0 {
				t.Fatalf("rejected artifact flags = %#x, %v", flags, err)
			}
		})
	}
}

func setLinuxTestNodumpFlag(t *testing.T, path string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	flags, err := unix.IoctlGetInt(int(file.Fd()), unix.FS_IOC_GETFLAGS)
	if err != nil {
		t.Skipf("Linux inode flags unavailable: %v", err)
	}
	if err := unix.IoctlSetPointerInt(int(file.Fd()), unix.FS_IOC_SETFLAGS, flags|linuxTestNodumpFlag); err != nil {
		if errors.Is(err, unix.ENOTTY) || errors.Is(err, unix.EOPNOTSUPP) || errors.Is(err, unix.EPERM) {
			t.Skipf("setting Linux inode flags unavailable: %v", err)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if current, getErr := unix.IoctlGetInt(int(file.Fd()), unix.FS_IOC_GETFLAGS); getErr == nil {
			_ = unix.IoctlSetPointerInt(int(file.Fd()), unix.FS_IOC_SETFLAGS, current&^linuxTestNodumpFlag)
		}
	})
}

func linuxTestFileFlags(path string) (int, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	return unix.IoctlGetInt(int(file.Fd()), unix.FS_IOC_GETFLAGS)
}
