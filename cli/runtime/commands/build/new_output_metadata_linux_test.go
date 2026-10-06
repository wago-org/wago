//go:build linux

package build

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/wago-org/wago/cli/internal/command"
	"golang.org/x/sys/unix"
)

func TestBuildNewLinuxOutputMatchesDirectCreationMetadata(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, os.ModeSetgid|0o770); err != nil {
		t.Fatal(err)
	}
	setLinuxTestDefaultACL(t, dir)
	control := filepath.Join(dir, "direct-control")
	controlFile, err := os.OpenFile(control, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if err := controlFile.Close(); err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(dir, "input.wasm")
	if err := os.WriteFile(input, []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0}, 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "output.wago")
	Command(testEnvironment{}).Run(command.NewContext(
		[]string{input}, map[string]string{"output": output}, nil,
	))

	controlInfo, controlErr := os.Stat(control)
	outputInfo, outputErr := os.Stat(output)
	if controlErr != nil || outputErr != nil {
		t.Fatalf("inspect direct/private metadata: control=%v output=%v", controlErr, outputErr)
	}
	controlStat, controlOK := controlInfo.Sys().(*syscall.Stat_t)
	outputStat, outputOK := outputInfo.Sys().(*syscall.Stat_t)
	if !controlOK || !outputOK || controlStat.Uid != outputStat.Uid || controlStat.Gid != outputStat.Gid ||
		controlInfo.Mode().Perm() != outputInfo.Mode().Perm() ||
		!bytes.Equal(readLinuxTestAccessACL(t, control), readLinuxTestAccessACL(t, output)) {
		t.Fatalf("private publication metadata differs from direct creation: control=%v output=%v", controlInfo, outputInfo)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if len(entry.Name()) >= len(".wago-") && entry.Name()[:len(".wago-")] == ".wago-" {
			t.Fatalf("temporary publication entry remains: %s", entry.Name())
		}
	}
}

func setLinuxTestDefaultACL(t *testing.T, path string) {
	t.Helper()
	uid := uint32(65534)
	if uid == uint32(os.Geteuid()) {
		uid--
	}
	data := make([]byte, 4+5*8)
	binary.LittleEndian.PutUint32(data[:4], 2)
	entries := []struct {
		tag, permissions uint16
		id               uint32
	}{
		{1, 7, ^uint32(0)}, {2, 4, uid}, {4, 0, ^uint32(0)},
		{16, 4, ^uint32(0)}, {32, 0, ^uint32(0)},
	}
	for index, entry := range entries {
		offset := 4 + index*8
		binary.LittleEndian.PutUint16(data[offset:], entry.tag)
		binary.LittleEndian.PutUint16(data[offset+2:], entry.permissions)
		binary.LittleEndian.PutUint32(data[offset+4:], entry.id)
	}
	if err := unix.Setxattr(path, "system.posix_acl_default", data, 0); err != nil {
		if errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.EOPNOTSUPP) || errors.Is(err, unix.EPERM) {
			t.Skipf("default POSIX ACL unavailable: %v", err)
		}
		t.Fatal(err)
	}
}
