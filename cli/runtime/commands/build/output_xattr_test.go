//go:build linux || darwin

package build

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wago-org/wago/cli/internal/command"
	"golang.org/x/sys/unix"
)

func TestBuildRejectsUnpreservedOutputXattr(t *testing.T) {
	const childEnv = "WAGO_BUILD_XATTR_CHILD"
	if os.Getenv(childEnv) == "1" {
		Command(testEnvironment{}).Run(command.NewContext(
			[]string{os.Getenv("WAGO_BUILD_XATTR_INPUT")},
			map[string]string{"output": os.Getenv("WAGO_BUILD_XATTR_OUTPUT")}, nil,
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
			wantXattr := []byte("persistent metadata")
			if err := setTestUnpreservedXattr(target, wantXattr); err != nil {
				if errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.EOPNOTSUPP) || errors.Is(err, unix.EPERM) {
					t.Skipf("extended attributes unavailable: %v", err)
				}
				t.Fatal(err)
			}
			output := target
			if throughSymlink {
				output = filepath.Join(dir, "output.wago")
				if err := os.Symlink(filepath.Base(target), output); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
				requireTestSymlink(t, output)
			}

			child := exec.Command(os.Args[0], "-test.run=^TestBuildRejectsUnpreservedOutputXattr$")
			child.Env = append(os.Environ(), childEnv+"=1",
				"WAGO_BUILD_XATTR_INPUT="+input, "WAGO_BUILD_XATTR_OUTPUT="+output)
			combined, err := child.CombinedOutput()
			if err == nil || !strings.Contains(string(combined), "unpreserved or invalid extended attributes") {
				t.Fatalf("unpreserved-xattr output error = %v: %s", err, combined)
			}
			if got, err := os.ReadFile(target); err != nil || !bytes.Equal(got, original) {
				t.Fatalf("rejected build changed output to %q, %v", got, err)
			}
			if got, err := getTestUnpreservedXattr(target); err != nil || !bytes.Equal(got, wantXattr) {
				t.Fatalf("rejected build changed xattr to %q, %v", got, err)
			}
			if throughSymlink {
				requireTestSymlink(t, output)
			}
			assertNoAtomicBuildTemps(t, dir)
		})
	}
}
