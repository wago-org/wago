//go:build darwin

package build

import (
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/command"
	"golang.org/x/sys/unix"
)

func TestParseDarwinAccessACL(t *testing.T) {
	fileSecurity := func(entries uint32) []byte {
		size := darwinFileSecuritySize
		if entries != darwinFileSecurityNoACL {
			size += int(entries) * darwinAccessACLEntrySize
		}
		data := make([]byte, size)
		binary.NativeEndian.PutUint32(data[:4], darwinFileSecurityMagic)
		binary.NativeEndian.PutUint32(data[36:40], entries)
		return data
	}
	packed := func(data []byte, returned bool) []byte {
		buffer := make([]byte, 32+len(data))
		binary.NativeEndian.PutUint32(buffer[:4], uint32(len(buffer)))
		if returned {
			binary.NativeEndian.PutUint32(buffer[4:8], unix.ATTR_CMN_EXTENDED_SECURITY)
		}
		binary.NativeEndian.PutUint32(buffer[24:28], 8)
		binary.NativeEndian.PutUint32(buffer[28:32], uint32(len(data)))
		copy(buffer[32:], data)
		return buffer
	}
	badMagic := packed(fileSecurity(0), true)
	binary.NativeEndian.PutUint32(badMagic[32:36], 0)
	unalignedOffset := packed(fileSecurity(0), true)
	binary.NativeEndian.PutUint32(unalignedOffset[24:28], 9)
	mismatchedSize := packed(fileSecurity(0), true)
	binary.NativeEndian.PutUint32(mismatchedSize[32+36:32+40], 1)

	for _, test := range []struct {
		name    string
		buffer  []byte
		present bool
		wantErr bool
	}{
		{name: "unsupported", buffer: packed(nil, false)},
		{name: "no-acl", buffer: packed(fileSecurity(darwinFileSecurityNoACL), true)},
		{name: "explicit-empty", buffer: packed(fileSecurity(0), true), present: true},
		{name: "one-entry", buffer: packed(fileSecurity(1), true), present: true},
		{name: "too-many-entries", buffer: packed(fileSecurity(darwinMaxAccessACLEntries+1), true), wantErr: true},
		{name: "bad-magic", buffer: badMagic, wantErr: true},
		{name: "unaligned-offset", buffer: unalignedOffset, wantErr: true},
		{name: "mismatched-size", buffer: mismatchedSize, wantErr: true},
		{name: "short-result", buffer: make([]byte, 12), wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, present, err := parseDarwinAccessACL(test.buffer)
			if (err != nil) != test.wantErr || present != test.present {
				t.Fatalf("parse ACL = present %v, error %v; want present %v, error %v", present, err, test.present, test.wantErr)
			}
		})
	}
}

func TestBuildPreservesDarwinOutputAccessACL(t *testing.T) {
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
			if output, err := exec.Command("/bin/chmod", "+a", "everyone allow read", target).CombinedOutput(); err != nil {
				t.Fatalf("install Darwin access ACL: %v: %s", err, output)
			}
			wantACL := readDarwinTestAccessACL(t, target)
			if wantACL == "" {
				t.Fatal("chmod installed no access ACL")
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
				t.Fatalf("ACL-protected target is not a compiled artifact: %x", artifact)
			}
			if got := readDarwinTestAccessACL(t, target); got != wantACL {
				t.Fatalf("access ACL = %q, want %q", got, wantACL)
			}
			if throughSymlink {
				requireTestSymlink(t, output)
			}
		})
	}
}

func readDarwinTestAccessACL(t *testing.T, path string) string {
	t.Helper()
	output, err := exec.Command("/bin/ls", "-led", path).Output()
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) < 2 {
		return ""
	}
	return strings.Join(lines[1:], "\n")
}
