//go:build darwin

package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/command"
)

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
