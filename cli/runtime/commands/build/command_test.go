package build

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/command"
)

type testEnvironment struct{}

func (testEnvironment) ProfileFlags() []command.Flag { return nil }
func (testEnvironment) LoadRuntime(config *wago.RuntimeConfig, _ string) *wago.Runtime {
	return wago.NewRuntime(wago.WithRuntimeConfig(config))
}

func TestCommandWritesRunnableArtifact(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "empty.wasm")
	if err := os.WriteFile(input, []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0}, 0o600); err != nil {
		t.Fatal(err)
	}
	Command(testEnvironment{}).Run(command.NewContext([]string{input}, nil, nil))
	output := filepath.Join(dir, "empty.wago")
	artifact, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if !wago.IsCompiled(artifact) {
		t.Fatalf("build output is not a .wago artifact: %x", artifact)
	}
	if _, err := wago.Load(artifact); err != nil {
		t.Fatalf("load build output: %v", err)
	}
}
