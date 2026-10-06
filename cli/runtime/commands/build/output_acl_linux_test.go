//go:build linux

package build

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/internal/atomicfile"
	"golang.org/x/sys/unix"
)

const testLinuxAccessACLName = "system.posix_acl_access"

func TestBuildPreservesLinuxOutputAccessACL(t *testing.T) {
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
			setLinuxTestAccessACL(t, target)
			wantACL := readLinuxTestAccessACL(t, target)
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
				t.Fatalf("ACL-protected target is not a compiled artifact: %x", artifact)
			}
			if got := readLinuxTestAccessACL(t, target); !bytes.Equal(got, wantACL) {
				t.Fatalf("access ACL = %x, want %x", got, wantACL)
			}
			if throughSymlink {
				requireTestSymlink(t, output)
			}
		})
	}
}

func TestBuildPublicationBoundaryRejectsAccessACLChange(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "artifact.wago")
	if err := os.WriteFile(target, []byte("original artifact"), 0o600); err != nil {
		t.Fatal(err)
	}
	setLinuxTestAccessACL(t, target)
	expectedACL := readLinuxTestAccessACL(t, target)
	changedACL := append([]byte(nil), expectedACL...)
	// Change only the named user's permissions. The ACL mask and therefore the
	// ordinary mode bits stay constant, ensuring the regression exercises the ACL.
	binary.LittleEndian.PutUint16(changedACL[14:], 2)

	options := atomicfile.Options{Mode: 0o600, Sync: true, Hooks: &atomicfile.Hooks{
		Sync: func(file *os.File) error {
			if err := file.Sync(); err != nil {
				return err
			}
			return unix.Setxattr(target, testLinuxAccessACLName, changedACL, 0)
		},
	}}
	setOptionalAtomicfileOption(&options, "BeforeReplace", func(path string) error {
		if current := readLinuxTestAccessACL(t, path); !bytes.Equal(current, expectedACL) {
			return errors.New("output metadata changed before publication")
		}
		return nil
	})

	err := atomicfile.ReplaceFile(target, options, func(writer io.Writer) error {
		_, err := io.WriteString(writer, "new compiled artifact")
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "metadata changed before publication") {
		t.Fatalf("publication after ACL change = %v", err)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "original artifact" {
		t.Fatalf("target after rejected publication = %q, %v", got, err)
	}
	if got := readLinuxTestAccessACL(t, target); !bytes.Equal(got, changedACL) {
		t.Fatalf("concurrent ACL = %x, want %x", got, changedACL)
	}
	assertNoAtomicBuildTemps(t, dir)
}

func TestBuildAccessACLFailureLeavesOutputIntact(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "artifact.wago")
	original := []byte("existing runnable artifact")
	if err := os.WriteFile(target, original, 0o600); err != nil {
		t.Fatal(err)
	}
	setLinuxTestAccessACL(t, target)
	wantACL := readLinuxTestAccessACL(t, target)
	info, err := os.Lstat(target)
	if err != nil {
		t.Fatal(err)
	}
	stat := info.Sys().(*syscall.Stat_t)
	metadata := buildOutputMetadata{
		set: true, uid: int(stat.Uid), gid: int(stat.Gid),
		accessACL: []byte("invalid POSIX ACL"), accessACLPresent: true,
	}
	err = atomicfile.ReplaceFile(target, atomicfile.Options{Mode: info.Mode().Perm(), ModeSet: true}, func(writer io.Writer) error {
		if _, err := writer.Write([]byte("replacement")); err != nil {
			return err
		}
		return applyBuildOutputMetadata(writer, metadata)
	})
	if err == nil || !strings.Contains(err.Error(), "preserve output access metadata") {
		t.Fatalf("replace with invalid ACL = %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("failed replacement changed artifact to %q", got)
	}
	if gotACL := readLinuxTestAccessACL(t, target); !bytes.Equal(gotACL, wantACL) {
		t.Fatalf("failed replacement changed access ACL to %x, want %x", gotACL, wantACL)
	}
	temporary, err := filepath.Glob(filepath.Join(dir, ".wago-atomic-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(temporary) != 0 {
		t.Fatalf("failed replacement left temporary files: %v", temporary)
	}
}

func TestBuildAccessACLCaptureRejectsTargetReplacement(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "artifact.wago")
	if err := os.WriteFile(target, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := inspectBuildOutput(target)
	if err != nil {
		t.Fatal(err)
	}
	publicationPath, _, _, info, err := snapshot.revalidate()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(target, filepath.Join(dir, "original.wago")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := captureBuildOutputMetadata(publicationPath, info, snapshot.targetIdentity); err == nil || !strings.Contains(err.Error(), "changed during build") {
		t.Fatalf("capture metadata for replaced output = %v", err)
	}
}

func TestBuildAccessACLCaptureUsesOpenedDescriptor(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "artifact.wago")
	if err := os.WriteFile(target, []byte("old artifact"), 0o600); err != nil {
		t.Fatal(err)
	}
	setLinuxTestAccessACL(t, target)
	snapshot, err := inspectBuildOutput(target)
	if err != nil {
		t.Fatal(err)
	}
	publicationPath, _, _, info, err := snapshot.revalidate()
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := captureBuildOutputMetadata(publicationPath, info, snapshot.targetIdentity)
	if err != nil {
		t.Fatal(err)
	}
	if !metadata.accessACLPresent || len(metadata.accessACL) == 0 {
		t.Fatalf("captured ACL = %x, present=%t", metadata.accessACL, metadata.accessACLPresent)
	}
}

func setLinuxTestAccessACL(t *testing.T, path string) {
	t.Helper()
	uid := uint32(65534)
	if uint32(os.Getuid()) == uid {
		uid = 1
	}
	// Linux stores POSIX ACLs as a versioned little-endian header followed by
	// owner, named-user, group, mask, and other entries. The named user receives
	// read access solely through the ACL, so losing it changes effective access.
	acl := make([]byte, 4+5*8)
	binary.LittleEndian.PutUint32(acl, 2)
	entries := []struct {
		tag, permissions uint16
		id               uint32
	}{
		{1, 6, ^uint32(0)},
		{2, 4, uid},
		{4, 0, ^uint32(0)},
		{16, 4, ^uint32(0)},
		{32, 0, ^uint32(0)},
	}
	for index, entry := range entries {
		offset := 4 + index*8
		binary.LittleEndian.PutUint16(acl[offset:], entry.tag)
		binary.LittleEndian.PutUint16(acl[offset+2:], entry.permissions)
		binary.LittleEndian.PutUint32(acl[offset+4:], entry.id)
	}
	if err := unix.Setxattr(path, testLinuxAccessACLName, acl, 0); err != nil {
		if errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.EOPNOTSUPP) {
			t.Skipf("POSIX access ACLs unavailable: %v", err)
		}
		t.Fatal(err)
	}
}

func readLinuxTestAccessACL(t *testing.T, path string) []byte {
	t.Helper()
	size, err := unix.Getxattr(path, testLinuxAccessACLName, nil)
	if err != nil {
		t.Fatal(err)
	}
	acl := make([]byte, size)
	size, err = unix.Getxattr(path, testLinuxAccessACLName, acl)
	if err != nil {
		t.Fatal(err)
	}
	return acl[:size]
}
