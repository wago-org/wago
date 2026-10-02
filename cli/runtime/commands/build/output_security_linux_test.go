//go:build linux

package build

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/command"
	"golang.org/x/sys/unix"
)

func TestBuildPreservesLinuxSELinuxLabel(t *testing.T) {
	label := readOptionalLinuxTestXattr(t, "/bin/sh", "security.selinux")
	if label == nil {
		t.Skip("host has no readable SELinux label fixture")
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
			if err := os.WriteFile(target, []byte("old artifact"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := unix.Setxattr(target, "security.selinux", label, 0); err != nil {
				t.Skipf("custom SELinux labels unavailable: %v", err)
			}
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
				t.Fatalf("SELinux-labeled target is not a compiled artifact: %x", artifact)
			}
			if got := readOptionalLinuxTestXattr(t, target, "security.selinux"); !bytes.Equal(got, label) {
				t.Fatalf("SELinux label = %q, want %q", got, label)
			}
			if throughSymlink {
				requireTestSymlink(t, output)
			}
		})
	}
}

func readOptionalLinuxTestXattr(t *testing.T, path, name string) []byte {
	t.Helper()
	size, err := unix.Getxattr(path, name, nil)
	if errors.Is(err, unix.ENODATA) || errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.EOPNOTSUPP) || errors.Is(err, unix.EPERM) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	value := make([]byte, size)
	size, err = unix.Getxattr(path, name, value)
	if err != nil {
		t.Fatal(err)
	}
	return value[:size]
}
