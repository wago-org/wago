//go:build linux

package build

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/internal/atomicfile"
	"golang.org/x/sys/unix"
)

const testLinuxSecurityLabelName = "user.wago-build-security-label"

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

func TestLinuxSecurityLabelMetadataRoundTrip(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	if err := os.WriteFile(source, []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	want := []byte("access-domain:v1")
	if err := unix.Setxattr(source, testLinuxSecurityLabelName, want, 0); err != nil {
		if errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.EOPNOTSUPP) {
			t.Skipf("extended attributes unavailable: %v", err)
		}
		t.Fatal(err)
	}

	fd, err := unix.Open(source, unix.O_WRONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { unix.Close(fd) })
	labels, err := captureLinuxSecurityLabels(fd, []string{testLinuxSecurityLabelName})
	if err != nil {
		t.Fatal(err)
	}
	if len(labels) != 1 || !labels[0].present || !bytes.Equal(labels[0].value, want) {
		t.Fatalf("captured labels = %+v, want %q", labels, want)
	}

	stagedPath := filepath.Join(dir, "staged")
	staged, err := os.OpenFile(stagedPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer staged.Close()
	if err := applyBuildAccessMetadata(staged, nil, false, labels); err != nil {
		t.Fatal(err)
	}
	if got := readOptionalLinuxTestXattr(t, stagedPath, testLinuxSecurityLabelName); !bytes.Equal(got, want) {
		t.Fatalf("applied label = %q, want %q", got, want)
	}

	// A staged inode can inherit access metadata from its directory. Explicitly
	// removing a label that was absent on the old output preserves that absence.
	if err := applyBuildAccessMetadata(staged, nil, false, []buildOutputSecurityLabel{{name: testLinuxSecurityLabelName}}); err != nil {
		t.Fatal(err)
	}
	if _, err := unix.Getxattr(stagedPath, testLinuxSecurityLabelName, nil); !errors.Is(err, unix.ENODATA) {
		t.Fatalf("removed label error = %v, want ENODATA", err)
	}
}

func TestBuildSecurityLabelFailureLeavesOutputIntact(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "artifact.wago")
	original := []byte("existing runnable artifact")
	if err := os.WriteFile(target, original, 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(target)
	if err != nil {
		t.Fatal(err)
	}
	metadata := buildOutputMetadata{
		set: true, uid: os.Getuid(), gid: os.Getgid(),
		securityLabels: []buildOutputSecurityLabel{{name: "invalid\x00label", value: []byte("value"), present: true}},
	}
	err = atomicfile.ReplaceFile(target, atomicfile.Options{Mode: info.Mode().Perm(), ModeSet: true}, func(writer io.Writer) error {
		if _, err := writer.Write([]byte("replacement")); err != nil {
			return err
		}
		return applyBuildOutputMetadata(writer, metadata)
	})
	if err == nil || !strings.Contains(err.Error(), "preserve output access metadata") {
		t.Fatalf("replace with invalid security label = %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("failed replacement changed artifact to %q", got)
	}
	temporary, err := filepath.Glob(filepath.Join(dir, ".wago-atomic-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(temporary) != 0 {
		t.Fatalf("failed replacement left temporary files: %v", temporary)
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
