//go:build linux && (amd64 || arm64)

package atomicfile

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"syscall"
	"testing"
	"unsafe"

	"golang.org/x/sys/unix"
)

func TestReplaceFileFallsBackWhenOTmpfileIsUnsupported(t *testing.T) {
	if os.Getenv("WAGO_ATOMICFILE_NO_OTMPFILE_CHILD") == "1" {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		syscall.Umask(0o777)
		if err := rejectOTmpfileOnCurrentThread(); err != nil {
			t.Fatal(err)
		}
		destination := os.Getenv("WAGO_ATOMICFILE_FALLBACK_OUTPUT")
		control := os.Getenv("WAGO_ATOMICFILE_FALLBACK_CONTROL")
		file, err := os.OpenFile(control, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		options := Options{Mode: 0o644}
		value := reflect.ValueOf(&options).Elem()
		for name, setting := range map[string]bool{
			"ModeSet": true, "ApplyUmask": true, "RetainReplaceHandle": true,
		} {
			if field := value.FieldByName(name); field.IsValid() {
				field.SetBool(setting)
			}
		}
		err = ReplaceFile(destination, options, func(writer io.Writer) error {
			_, err := io.WriteString(writer, "complete artifact")
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		controlInfo, controlErr := os.Stat(control)
		outputInfo, outputErr := os.Stat(destination)
		controlACL, controlACLErr := linuxTestXattr(control, "system.posix_acl_access")
		outputACL, outputACLErr := linuxTestXattr(destination, "system.posix_acl_access")
		controlStat, controlStatOK := controlInfo.Sys().(*syscall.Stat_t)
		outputStat, outputStatOK := outputInfo.Sys().(*syscall.Stat_t)
		if controlErr != nil || outputErr != nil || controlACLErr != nil || outputACLErr != nil ||
			!controlStatOK || !outputStatOK || controlStat.Gid != outputStat.Gid ||
			controlInfo.Mode().Perm() != outputInfo.Mode().Perm() || !bytes.Equal(controlACL, outputACL) {
			t.Fatalf("fallback metadata differs from direct creation: control=%v/%x/%v output=%v/%x/%v",
				controlInfo, controlACL, errors.Join(controlErr, controlACLErr),
				outputInfo, outputACL, errors.Join(outputErr, outputACLErr))
		}
		return
	}

	for _, test := range []struct {
		name       string
		defaultACL bool
	}{{name: "umask-0777"}, {name: "posix-default-acl", defaultACL: true}} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Chmod(dir, os.ModeSetgid|0o770); err != nil {
				t.Fatal(err)
			}
			if test.defaultACL {
				if err := installLinuxTestDefaultACL(dir); err != nil {
					if errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.EOPNOTSUPP) || errors.Is(err, unix.EPERM) {
						t.Skipf("default POSIX ACL unavailable: %v", err)
					}
					t.Fatal(err)
				}
			}
			destination := filepath.Join(dir, "artifact.wago")
			control := filepath.Join(dir, "direct-control")
			child := exec.Command(os.Args[0], "-test.run=^"+stringsBeforeSlash(t.Name())+"$")
			child.Env = append(os.Environ(),
				"WAGO_ATOMICFILE_NO_OTMPFILE_CHILD=1",
				"WAGO_ATOMICFILE_FALLBACK_OUTPUT="+destination,
				"WAGO_ATOMICFILE_FALLBACK_CONTROL="+control,
			)
			if output, err := child.CombinedOutput(); err != nil {
				t.Fatalf("ReplaceFile without O_TMPFILE support: %v\n%s", err, output)
			}
			info, err := os.Stat(destination)
			if err != nil {
				t.Fatal(err)
			}
			if !test.defaultACL && info.Mode().Perm() != 0 {
				t.Fatalf("fallback output mode under umask 777 = %03o, want 000", info.Mode().Perm())
			}
			if err := os.Chmod(destination, 0o600); err != nil {
				t.Fatal(err)
			}
			if got, err := os.ReadFile(destination); err != nil || string(got) != "complete artifact" {
				t.Fatalf("fallback output = %q, %v", got, err)
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 2 {
				t.Fatalf("fallback left temporary entries: %v", entries)
			}
		})
	}
}

func stringsBeforeSlash(name string) string {
	for i, character := range name {
		if character == '/' {
			return name[:i]
		}
	}
	return name
}

func installLinuxTestDefaultACL(path string) error {
	const (
		aclVersion  = 2
		aclUserObj  = 0x01
		aclUser     = 0x02
		aclGroupObj = 0x04
		aclMask     = 0x10
		aclOther    = 0x20
	)
	uid := uint32(65534)
	if uid == uint32(os.Geteuid()) {
		uid--
	}
	data := make([]byte, 4+5*8)
	binary.LittleEndian.PutUint32(data[:4], aclVersion)
	entries := []struct {
		tag  uint16
		perm uint16
		id   uint32
	}{
		{tag: aclUserObj, perm: 7, id: ^uint32(0)},
		{tag: aclUser, perm: 4, id: uid},
		{tag: aclGroupObj, perm: 0, id: ^uint32(0)},
		{tag: aclMask, perm: 4, id: ^uint32(0)},
		{tag: aclOther, perm: 0, id: ^uint32(0)},
	}
	for i, entry := range entries {
		offset := 4 + i*8
		binary.LittleEndian.PutUint16(data[offset:offset+2], entry.tag)
		binary.LittleEndian.PutUint16(data[offset+2:offset+4], entry.perm)
		binary.LittleEndian.PutUint32(data[offset+4:offset+8], entry.id)
	}
	return unix.Setxattr(path, "system.posix_acl_default", data, 0)
}

func linuxTestXattr(path, name string) ([]byte, error) {
	size, err := unix.Getxattr(path, name, nil)
	if errors.Is(err, unix.ENODATA) || errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.EOPNOTSUPP) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	data := make([]byte, size)
	_, err = unix.Getxattr(path, name, data)
	return data, err
}

func rejectOTmpfileOnCurrentThread() error {
	// seccomp_data stores syscall arguments at offset 16. openat's flags are
	// argument 2, and both supported test architectures store their low word at
	// the start of the 64-bit slot.
	const openatFlagsOffset = 16 + 2*8
	filters := []unix.SockFilter{
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 0},
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, Jf: 3, K: uint32(unix.SYS_OPENAT)},
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: openatFlagsOffset},
		{Code: unix.BPF_JMP | unix.BPF_JSET | unix.BPF_K, Jf: 1, K: uint32(unix.O_TMPFILE &^ unix.O_DIRECTORY)},
		{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ERRNO | uint32(unix.EOPNOTSUPP)},
		{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ALLOW},
	}
	program := unix.SockFprog{Len: uint16(len(filters)), Filter: &filters[0]}
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return err
	}
	err := unix.Prctl(unix.PR_SET_SECCOMP, unix.SECCOMP_MODE_FILTER, uintptr(unsafe.Pointer(&program)), 0, 0)
	runtime.KeepAlive(filters)
	runtime.KeepAlive(&program)
	if errors.Is(err, unix.EINVAL) || errors.Is(err, unix.ENOSYS) {
		return errors.New("seccomp filter unavailable")
	}
	return err
}
