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

func TestBuildCreatesMissingVolumeRootedSymlinkTarget(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.wasm")
	if err := os.WriteFile(input, []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0}, 0o600); err != nil {
		t.Fatal(err)
	}
	targetDir := filepath.Join(dir, "artifacts")
	if err := os.Mkdir(targetDir, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(targetDir, "current.wago")
	linkTarget := strings.TrimPrefix(target, filepath.VolumeName(target))
	if linkTarget == target || linkTarget == "" || !os.IsPathSeparator(linkTarget[0]) {
		t.Fatalf("test target %q is not volume-rooted", linkTarget)
	}
	output := filepath.Join(dir, "output.wago")
	if err := os.Symlink(linkTarget, output); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	requireTestSymlink(t, output)

	Command(testEnvironment{}).Run(command.NewContext(
		[]string{input}, map[string]string{"output": output}, nil,
	))

	artifact, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !wago.IsCompiled(artifact) {
		t.Fatalf("new volume-rooted symlink target is not a compiled artifact: %x", artifact)
	}
	if got, err := os.Readlink(output); err != nil || got != linkTarget {
		t.Fatalf("output symlink = %q, %v; want %q", got, err, linkTarget)
	}
}
