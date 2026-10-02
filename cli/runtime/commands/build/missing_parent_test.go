package build

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/wago-org/wago/cli/internal/command"
)

func TestBuildRejectsMissingOutputParent(t *testing.T) {
	if os.Getenv("WAGO_BUILD_MISSING_PARENT_CHILD") == "1" {
		Command(testEnvironment{}).Run(command.NewContext(
			[]string{os.Getenv("WAGO_BUILD_INPUT")},
			map[string]string{"output": os.Getenv("WAGO_BUILD_OUTPUT")}, nil,
		))
		return
	}

	dir := t.TempDir()
	input := filepath.Join(dir, "input.wasm")
	if err := os.WriteFile(input, []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0}, 0o600); err != nil {
		t.Fatal(err)
	}
	missingParent := filepath.Join(dir, "misspelled-output-directory")
	output := filepath.Join(missingParent, "artifact.wago")
	child := exec.Command(os.Args[0], "-test.run=^TestBuildRejectsMissingOutputParent$")
	child.Env = append(os.Environ(),
		"WAGO_BUILD_MISSING_PARENT_CHILD=1",
		"WAGO_BUILD_INPUT="+input,
		"WAGO_BUILD_OUTPUT="+output,
	)
	combined, err := child.CombinedOutput()
	if err == nil || !bytes.Contains(combined, []byte("output directory")) {
		t.Fatalf("missing output parent error = %v: %s", err, combined)
	}
	if _, err := os.Stat(missingParent); !os.IsNotExist(err) {
		t.Fatalf("missing output parent was created: %v", err)
	}
}
