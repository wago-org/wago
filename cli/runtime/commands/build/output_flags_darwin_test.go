//go:build darwin

package build

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/wago-org/wago/cli/internal/command"
)

func TestBuildRejectsDarwinOutputFlags(t *testing.T) {
	if os.Getenv("WAGO_BUILD_DARWIN_FLAGS_CHILD") == "1" {
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
			input := writeAtomicTestModule(t, dir)
			target := filepath.Join(dir, "artifact.wago")
			original := []byte("existing artifact")
			if err := os.WriteFile(target, original, 0o600); err != nil {
				t.Fatal(err)
			}
			if output, err := exec.Command("/bin/chflags", "nodump", target).CombinedOutput(); err != nil {
				t.Skipf("setting Darwin output flags unavailable: %v: %s", err, output)
			}
			wantFlags := darwinTestFileFlags(t, target)
			if wantFlags == 0 {
				t.Skip("filesystem did not retain the nodump flag")
			}
			output := target
			if throughSymlink {
				output = filepath.Join(dir, "output.wago")
				if err := os.Symlink(filepath.Base(target), output); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
				requireTestSymlink(t, output)
			}

			child := exec.Command(os.Args[0], "-test.run=^TestBuildRejectsDarwinOutputFlags$/^"+name+"$")
			child.Env = append(os.Environ(),
				"WAGO_BUILD_DARWIN_FLAGS_CHILD=1", "WAGO_BUILD_INPUT="+input, "WAGO_BUILD_OUTPUT="+output,
			)
			combined, err := child.CombinedOutput()
			if err == nil || !bytes.Contains(combined, []byte("persistent Darwin file flags")) {
				t.Fatalf("flagged output error = %v: %s", err, combined)
			}
			if got, err := os.ReadFile(target); err != nil || !bytes.Equal(got, original) {
				t.Fatalf("rejected artifact = %q, %v", got, err)
			}
			if got := darwinTestFileFlags(t, target); got != wantFlags {
				t.Fatalf("rejected artifact flags = %#x, want %#x", got, wantFlags)
			}
			if throughSymlink {
				requireTestSymlink(t, output)
			}
		})
	}
}

func darwinTestFileFlags(t *testing.T, path string) uint32 {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatalf("unexpected Darwin stat type %T", info.Sys())
	}
	return stat.Flags
}
