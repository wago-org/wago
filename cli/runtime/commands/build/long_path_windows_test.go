//go:build windows

package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/command"
)

func TestBuildPublishesToLongWindowsPaths(t *testing.T) {
	dir := t.TempDir()
	input := writeLongPathTestModule(t, dir)
	longDirectory := dir
	for len(filepath.Join(longDirectory, "output.wago")) <= 300 {
		longDirectory = filepath.Join(longDirectory, strings.Repeat("long-path-component-", 4))
	}
	if err := os.MkdirAll(longDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(longDirectory, "output.wago")

	// Exercise both creation and replacement. They use separate raw Windows
	// handles internally and both must retain os.WriteFile's long-path behavior.
	for _, name := range []string{"missing", "existing"} {
		t.Run(name, func(t *testing.T) {
			if name == "existing" {
				if err := os.WriteFile(target, []byte("previous artifact"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			Command(testEnvironment{}).Run(command.NewContext(
				[]string{input}, map[string]string{"output": target}, nil,
			))
			assertLongPathBuildArtifact(t, target)
		})
	}
}

func TestBuildPublishesThroughSymlinkToLongWindowsPath(t *testing.T) {
	dir := t.TempDir()
	input := writeLongPathTestModule(t, dir)
	longDirectory := dir
	for len(filepath.Join(longDirectory, "target.wago")) <= 300 {
		longDirectory = filepath.Join(longDirectory, strings.Repeat("long-symlink-target-", 4))
	}
	if err := os.MkdirAll(longDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(longDirectory, "target.wago")
	if err := os.WriteFile(target, []byte("previous artifact"), 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "output.wago")
	if err := os.Symlink(target, output); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	requireTestSymlink(t, output)

	Command(testEnvironment{}).Run(command.NewContext(
		[]string{input}, map[string]string{"output": output}, nil,
	))
	assertLongPathBuildArtifact(t, target)
	if got, err := os.Readlink(output); err != nil || got != target {
		t.Fatalf("output symlink = %q, %v; want %q", got, err, target)
	}
}

func writeLongPathTestModule(t *testing.T, dir string) string {
	t.Helper()
	input := filepath.Join(dir, "input.wasm")
	if err := os.WriteFile(input, []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0}, 0o600); err != nil {
		t.Fatal(err)
	}
	return input
}

func assertLongPathBuildArtifact(t *testing.T, path string) {
	t.Helper()
	artifact, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !wago.IsCompiled(artifact) {
		t.Fatalf("long-path output is not a compiled artifact: %x", artifact)
	}
	if _, err := wago.LoadTrustedArtifact(artifact); err != nil {
		t.Fatalf("load long-path output: %v", err)
	}
}
