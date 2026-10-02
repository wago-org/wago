package build

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/automation"
	"github.com/wago-org/wago/cli/internal/command"
)

type testEnvironment struct{}

func (testEnvironment) ProfileFlags() []command.Flag { return nil }
func (testEnvironment) LoadRuntime(config *wago.RuntimeConfig, guestArgs []string) *wago.Runtime {
	return wago.NewRuntime(wago.WithRuntimeConfig(config), wago.WithGuestArguments(guestArgs))
}

func TestCommandDryRunDoesNotReadOrWriteArtifact(t *testing.T) {
	automation.Reset()
	automation.Configure(automation.Options{DryRun: true})
	t.Cleanup(automation.Reset)
	dir := t.TempDir()
	input := filepath.Join(dir, "missing.wasm")
	output := filepath.Join(dir, "planned.wago")

	old := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writer
	Command(testEnvironment{}).Run(command.NewContext(
		[]string{input}, map[string]string{"output": output}, nil,
	))
	_ = writer.Close()
	os.Stdout = old
	data, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Dry run: build artifact") || !strings.Contains(string(data), output) {
		t.Fatalf("dry-run output = %q", data)
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("dry run wrote output: %v", err)
	}
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
