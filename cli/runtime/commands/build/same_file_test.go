package build

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wago-org/wago/cli/internal/command"
)

func TestBuildRejectsSameFile(t *testing.T) {
	if os.Getenv("WAGO_BUILD_ALIAS_CHILD") == "1" {
		Command(testEnvironment{}).Run(command.NewContext([]string{os.Getenv("WAGO_BUILD_INPUT")}, map[string]string{"output": os.Getenv("WAGO_BUILD_OUTPUT")}, nil))
		return
	}
	for _, kind := range []string{"absolute", "relative", "symlink", "hardlink"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			source := []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0}
			input := filepath.Join(dir, "input.wasm")
			if err := os.WriteFile(input, source, 0600); err != nil {
				t.Fatal(err)
			}
			output := input
			inputArg := "input.wasm"
			switch kind {
			case "relative":
				inputArg, output = input, "input.wasm"
			case "symlink", "hardlink":
				output = filepath.Join(dir, "alias.wago")
				link := os.Link
				if kind == "symlink" {
					link = os.Symlink
				}
				if err := link(input, output); err != nil {
					t.Skipf("link unavailable: %v", err)
				}
			}
			cmd := exec.Command(os.Args[0], "-test.run=^TestBuildRejectsSameFile$")
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "WAGO_BUILD_ALIAS_CHILD=1", "WAGO_BUILD_INPUT="+inputArg, "WAGO_BUILD_OUTPUT="+output)
			result, err := cmd.CombinedOutput()
			if err == nil || !strings.Contains(string(result), "output path must differ from input") {
				t.Errorf("expected same-file rejection, got %v: %s", err, result)
			}
			got, err := os.ReadFile(input)
			if err != nil || !bytes.Equal(got, source) {
				t.Errorf("source changed: %x, %v", got, err)
			}
		})
	}
}

func BenchmarkBuildArtifact(b *testing.B) {
	dir := b.TempDir()
	input, output := filepath.Join(dir, "input.wasm"), filepath.Join(dir, "output.wago")
	if err := os.WriteFile(input, []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0}, 0600); err != nil {
		b.Fatal(err)
	}
	sink, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		b.Fatal(err)
	}
	previous := os.Stdout
	os.Stdout = sink
	defer func() { os.Stdout = previous; sink.Close() }()
	cmd := Command(testEnvironment{})
	ctx := command.NewContext([]string{input}, map[string]string{"output": output}, nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cmd.Run(ctx)
	}
}
