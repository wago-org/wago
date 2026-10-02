//go:build unix

package build

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/wago-org/wago/cli/internal/command"
)

func TestBuildRejectsOutputWithSpecialUnixMode(t *testing.T) {
	if os.Getenv("WAGO_BUILD_SPECIAL_MODE_CHILD") == "1" {
		Command(testEnvironment{}).Run(command.NewContext(
			[]string{os.Getenv("WAGO_BUILD_INPUT")},
			map[string]string{"output": os.Getenv("WAGO_BUILD_OUTPUT")}, nil,
		))
		return
	}

	modes := []struct {
		name string
		bit  os.FileMode
	}{
		{name: "setuid", bit: os.ModeSetuid},
		{name: "setgid", bit: os.ModeSetgid},
		{name: "sticky", bit: os.ModeSticky},
	}
	for _, mode := range modes {
		for _, throughSymlink := range []bool{false, true} {
			name := mode.name + "/direct"
			if throughSymlink {
				name = mode.name + "/symlink-target"
			}
			t.Run(name, func(t *testing.T) {
				dir := t.TempDir()
				input := filepath.Join(dir, "input.wasm")
				if err := os.WriteFile(input, []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0}, 0o600); err != nil {
					t.Fatal(err)
				}
				target := filepath.Join(dir, "artifact.wago")
				original := []byte("existing artifact")
				if err := os.WriteFile(target, original, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(target, 0o700|mode.bit); err != nil {
					t.Skipf("%s mode unavailable: %v", mode.name, err)
				}
				before, err := os.Lstat(target)
				if err != nil {
					t.Fatal(err)
				}
				if before.Mode()&mode.bit == 0 {
					t.Skipf("filesystem did not retain %s mode", mode.name)
				}
				output := target
				if throughSymlink {
					output = filepath.Join(dir, "output.wago")
					if err := os.Symlink(filepath.Base(target), output); err != nil {
						t.Skipf("symlink unavailable: %v", err)
					}
					requireTestSymlink(t, output)
				}

				child := exec.Command(os.Args[0], "-test.run=^TestBuildRejectsOutputWithSpecialUnixMode$")
				child.Env = append(os.Environ(),
					"WAGO_BUILD_SPECIAL_MODE_CHILD=1",
					"WAGO_BUILD_INPUT="+input,
					"WAGO_BUILD_OUTPUT="+output,
				)
				combined, err := child.CombinedOutput()
				if err == nil || !bytes.Contains(combined, []byte("setuid, setgid, or sticky")) {
					t.Fatalf("special-mode output error = %v: %s", err, combined)
				}
				got, err := os.ReadFile(target)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(got, original) {
					t.Fatalf("rejected build changed artifact to %q", got)
				}
				after, err := os.Lstat(target)
				if err != nil {
					t.Fatal(err)
				}
				if after.Mode()&mode.bit == 0 {
					t.Fatalf("rejected build cleared %s mode", mode.name)
				}
				if throughSymlink {
					requireTestSymlink(t, output)
				}
			})
		}
	}
}
