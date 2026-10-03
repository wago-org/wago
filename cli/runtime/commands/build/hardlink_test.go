package build

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/wago-org/wago/cli/internal/command"
)

func TestBuildRejectsMultiplyLinkedOutput(t *testing.T) {
	if os.Getenv("WAGO_BUILD_HARDLINK_CHILD") == "1" {
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
			input := filepath.Join(dir, "input.wasm")
			if err := os.WriteFile(input, []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0}, 0o600); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(dir, "artifact.wago")
			original := []byte("existing artifact")
			if err := os.WriteFile(target, original, 0o600); err != nil {
				t.Fatal(err)
			}
			alias := filepath.Join(dir, "artifact-alias.wago")
			if err := os.Link(target, alias); err != nil {
				t.Skipf("hard links unavailable: %v", err)
			}
			output := target
			if throughSymlink {
				output = filepath.Join(dir, "output.wago")
				if err := os.Symlink(filepath.Base(target), output); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
				requireTestSymlink(t, output)
			}

			child := exec.Command(os.Args[0], "-test.run=^TestBuildRejectsMultiplyLinkedOutput$")
			child.Env = append(os.Environ(),
				"WAGO_BUILD_HARDLINK_CHILD=1",
				"WAGO_BUILD_INPUT="+input,
				"WAGO_BUILD_OUTPUT="+output,
			)
			combined, err := child.CombinedOutput()
			if err == nil || !bytes.Contains(combined, []byte("multiple hard links")) {
				t.Fatalf("multiply linked output error = %v: %s", err, combined)
			}
			for _, path := range []string{target, alias} {
				got, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(got, original) {
					t.Fatalf("rejected build changed %s to %q", path, got)
				}
			}
			if throughSymlink {
				requireTestSymlink(t, output)
			}
		})
	}
}
