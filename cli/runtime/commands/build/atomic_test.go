//go:build !windows

package build

import (
	"bytes"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/command"
)

func TestBuildWriteFailurePreservesExistingArtifact(t *testing.T) {
	if os.Getenv("WAGO_BUILD_WRITE_FAILURE_CHILD") == "1" {
		signal.Ignore(syscall.SIGXFSZ)
		if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &syscall.Rlimit{Cur: 1, Max: 1}); err != nil {
			t.Fatal(err)
		}
		Command(testEnvironment{}).Run(command.NewContext(
			[]string{os.Getenv("WAGO_BUILD_INPUT")},
			map[string]string{"output": os.Getenv("WAGO_BUILD_OUTPUT")}, nil,
		))
		return
	}

	dir := t.TempDir()
	input := filepath.Join(dir, "input.wasm")
	output := filepath.Join(dir, "output.wago")
	if err := os.WriteFile(input, []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0}, 0o600); err != nil {
		t.Fatal(err)
	}
	previous := []byte("previous valid artifact bytes")
	if err := os.WriteFile(output, previous, 0o600); err != nil {
		t.Fatal(err)
	}

	child := exec.Command(os.Args[0], "-test.run=^TestBuildWriteFailurePreservesExistingArtifact$")
	child.Env = append(os.Environ(),
		"WAGO_BUILD_WRITE_FAILURE_CHILD=1",
		"WAGO_BUILD_INPUT="+input,
		"WAGO_BUILD_OUTPUT="+output,
	)
	if combined, err := child.CombinedOutput(); err == nil {
		t.Fatalf("build unexpectedly succeeded under file-size limit: %s", combined)
	}
	got, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, previous) {
		t.Fatalf("failed build replaced prior artifact: got %q, want %q", got, previous)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".wago-") {
			t.Fatalf("failed build left temporary publication entry: %s", entry.Name())
		}
	}
}

func TestBuildNewOutputRespectsUmask(t *testing.T) {
	if os.Getenv("WAGO_BUILD_UMASK_CHILD") == "1" {
		syscall.Umask(0o077)
		Command(testEnvironment{}).Run(command.NewContext(
			[]string{os.Getenv("WAGO_BUILD_INPUT")},
			map[string]string{"output": os.Getenv("WAGO_BUILD_OUTPUT")}, nil,
		))
		return
	}

	dir := t.TempDir()
	input := writeAtomicTestModule(t, dir)
	output := filepath.Join(dir, "new-output.wago")
	runAtomicBuildChild(t, "WAGO_BUILD_UMASK_CHILD", input, output)
	info, err := os.Stat(output)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("new output mode under umask 077 = %03o, want 600", got)
	}
}

func TestBuildNewOutputRespectsFullyRestrictiveUmask(t *testing.T) {
	if os.Getenv("WAGO_BUILD_UMASK_ZERO_CHILD") == "1" {
		syscall.Umask(0o777)
		Command(testEnvironment{}).Run(command.NewContext(
			[]string{os.Getenv("WAGO_BUILD_INPUT")},
			map[string]string{"output": os.Getenv("WAGO_BUILD_OUTPUT")}, nil,
		))
		return
	}

	dir := t.TempDir()
	input := writeAtomicTestModule(t, dir)
	output := filepath.Join(dir, "zero-mode-output.wago")
	runAtomicBuildChild(t, "WAGO_BUILD_UMASK_ZERO_CHILD", input, output)
	info, err := os.Stat(output)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0 {
		t.Fatalf("new output mode under umask 777 = %03o, want 000", got)
	}
}

func TestBuildPreservesExactZeroOutputMode(t *testing.T) {
	if os.Getenv("WAGO_BUILD_ZERO_MODE_CHILD") == "1" {
		Command(testEnvironment{}).Run(command.NewContext(
			[]string{os.Getenv("WAGO_BUILD_INPUT")},
			map[string]string{"output": os.Getenv("WAGO_BUILD_OUTPUT")}, nil,
		))
		return
	}

	dir := t.TempDir()
	input := writeAtomicTestModule(t, dir)
	output := filepath.Join(dir, "zero-mode.wago")
	if err := os.WriteFile(output, []byte("previous artifact"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(output, 0); err != nil {
		t.Fatal(err)
	}
	probe, probeErr := os.OpenFile(output, os.O_WRONLY, 0)
	writeAllowed := probeErr == nil
	if probeErr == nil {
		if err := probe.Close(); err != nil {
			t.Fatal(err)
		}
	} else if !os.IsPermission(probeErr) {
		t.Fatalf("probe mode-000 write authorization: %v", probeErr)
	}
	if !writeAllowed {
		// Match the former os.WriteFile authorization: an unprivileged process
		// cannot replace a mode-000 artifact merely because its directory is writable.
		// Probe the kernel instead of assuming euid 0 is the only process with an
		// override capability.
		child := exec.Command(os.Args[0], "-test.run=^"+t.Name()+"$")
		child.Env = append(os.Environ(),
			"WAGO_BUILD_ZERO_MODE_CHILD=1", "WAGO_BUILD_INPUT="+input, "WAGO_BUILD_OUTPUT="+output,
		)
		combined, err := child.CombinedOutput()
		if err == nil || !bytes.Contains(combined, []byte("permission denied")) {
			t.Fatalf("zero-mode build error = %v: %s", err, combined)
		}
		if info, err := os.Stat(output); err != nil || info.Mode().Perm() != 0 {
			t.Fatalf("rejected zero-mode output mode = %v, %v; want exact 000", info, err)
		}
		assertNoAtomicBuildTemps(t, dir)
		if err := os.Chmod(output, 0o600); err != nil {
			t.Fatal(err)
		}
		if got, err := os.ReadFile(output); err != nil || string(got) != "previous artifact" {
			t.Fatalf("rejected zero-mode output = %q, %v", got, err)
		}
		return
	}
	runAtomicBuildChild(t, "WAGO_BUILD_ZERO_MODE_CHILD", input, output)
	info, err := os.Stat(output)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0 {
		t.Fatalf("replacement output mode = %03o, want exact 000", got)
	}
}

func TestBuildPublishesThroughRelativeOutputSymlinkChain(t *testing.T) {
	dir := t.TempDir()
	input := writeAtomicTestModule(t, dir)
	targetDir := filepath.Join(dir, "artifacts")
	if err := os.Mkdir(targetDir, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(targetDir, "current.wago")
	if err := os.WriteFile(target, []byte("previous artifact"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(target, 0o640); err != nil {
		t.Fatal(err)
	}
	intermediate := filepath.Join(dir, "current")
	intermediateTarget := filepath.Join("artifacts", filepath.Base(target))
	if err := os.Symlink(intermediateTarget, intermediate); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	requireTestSymlink(t, intermediate)
	output := filepath.Join(dir, "output.wago")
	outputTarget := filepath.Base(intermediate)
	if err := os.Symlink(outputTarget, output); err != nil {
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
		t.Fatalf("symlink target is not a compiled artifact: %x", artifact)
	}
	if _, err := wago.LoadTrustedArtifact(artifact); err != nil {
		t.Fatalf("load symlink target: %v", err)
	}
	if got, err := os.Readlink(output); err != nil || got != outputTarget {
		t.Fatalf("output symlink = %q, %v; want %q", got, err, outputTarget)
	}
	if got, err := os.Readlink(intermediate); err != nil || got != intermediateTarget {
		t.Fatalf("intermediate symlink = %q, %v; want %q", got, err, intermediateTarget)
	}
	if info, err := os.Stat(target); err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("target mode = %v, %v; want 640", info, err)
	}
}

func TestBuildCreatesMissingRelativeOutputSymlinkTarget(t *testing.T) {
	if os.Getenv("WAGO_BUILD_DANGLING_SYMLINK_CHILD") == "1" {
		syscall.Umask(0o077)
		Command(testEnvironment{}).Run(command.NewContext(
			[]string{os.Getenv("WAGO_BUILD_INPUT")},
			map[string]string{"output": os.Getenv("WAGO_BUILD_OUTPUT")}, nil,
		))
		return
	}
	dir := t.TempDir()
	input := writeAtomicTestModule(t, dir)
	targetDir := filepath.Join(dir, "artifacts")
	if err := os.Mkdir(targetDir, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(targetDir, "current.wago")
	intermediate := filepath.Join(dir, "current")
	intermediateTarget := filepath.Join("artifacts", filepath.Base(target))
	if err := os.Symlink(intermediateTarget, intermediate); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	requireTestSymlink(t, intermediate)
	output := filepath.Join(dir, "output.wago")
	outputTarget := filepath.Base(intermediate)
	if err := os.Symlink(outputTarget, output); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	requireTestSymlink(t, output)

	runAtomicBuildChild(t, "WAGO_BUILD_DANGLING_SYMLINK_CHILD", input, output)

	artifact, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !wago.IsCompiled(artifact) {
		t.Fatalf("new symlink target is not a compiled artifact: %x", artifact)
	}
	if _, err := wago.LoadTrustedArtifact(artifact); err != nil {
		t.Fatalf("load new symlink target: %v", err)
	}
	if info, err := os.Stat(target); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("new symlink target mode = %v, %v; want 600", info, err)
	}
	if got, err := os.Readlink(output); err != nil || got != outputTarget {
		t.Fatalf("output symlink = %q, %v; want %q", got, err, outputTarget)
	}
	if got, err := os.Readlink(intermediate); err != nil || got != intermediateTarget {
		t.Fatalf("intermediate symlink = %q, %v; want %q", got, err, intermediateTarget)
	}
}

func TestBuildRejectsInvalidOutputSymlinkTarget(t *testing.T) {
	if os.Getenv("WAGO_BUILD_INVALID_SYMLINK_CHILD") == "1" {
		Command(testEnvironment{}).Run(command.NewContext(
			[]string{os.Getenv("WAGO_BUILD_INPUT")},
			map[string]string{"output": os.Getenv("WAGO_BUILD_OUTPUT")}, nil,
		))
		return
	}

	for _, test := range []struct {
		name      string
		target    string
		wantError string
	}{
		{name: "missing-parent", target: filepath.Join("missing", "artifact.wago"), wantError: "resolve output symlink"},
		{name: "missing-before-dotdot", target: "missing" + string(os.PathSeparator) + ".." + string(os.PathSeparator) + "artifact.wago", wantError: "resolve output symlink"},
		{name: "directory-intent", target: "missing-directory" + string(os.PathSeparator), wantError: "trailing path separator"},
		{name: "directory", target: "directory", wantError: "output symlink target is not a regular file"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			input := writeAtomicTestModule(t, dir)
			if test.name == "directory" {
				if err := os.Mkdir(filepath.Join(dir, test.target), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			output := filepath.Join(dir, "output.wago")
			if err := os.Symlink(test.target, output); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}
			requireTestSymlink(t, output)
			child := exec.Command(os.Args[0], "-test.run=^TestBuildRejectsInvalidOutputSymlinkTarget$")
			child.Env = append(os.Environ(),
				"WAGO_BUILD_INVALID_SYMLINK_CHILD=1",
				"WAGO_BUILD_INPUT="+input,
				"WAGO_BUILD_OUTPUT="+output,
			)
			combined, err := child.CombinedOutput()
			if err == nil || !bytes.Contains(combined, []byte(test.wantError)) {
				t.Fatalf("invalid output symlink error = %v: %s; want %q", err, combined, test.wantError)
			}
			if got, err := os.Readlink(output); err != nil || got != test.target {
				t.Fatalf("output symlink changed to %q, %v; want %q", got, err, test.target)
			}
			if strings.HasPrefix(test.name, "missing-") {
				if _, err := os.Stat(filepath.Join(dir, "missing")); !os.IsNotExist(err) {
					t.Fatalf("build created missing symlink parent: %v", err)
				}
			}
		})
	}
}

func writeAtomicTestModule(t *testing.T, dir string) string {
	t.Helper()
	input := filepath.Join(dir, "input.wasm")
	if err := os.WriteFile(input, []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0}, 0o600); err != nil {
		t.Fatal(err)
	}
	return input
}

func runAtomicBuildChild(t *testing.T, marker, input, output string) {
	t.Helper()
	child := exec.Command(os.Args[0], "-test.run=^"+t.Name()+"$")
	child.Env = append(os.Environ(), marker+"=1", "WAGO_BUILD_INPUT="+input, "WAGO_BUILD_OUTPUT="+output)
	if combined, err := child.CombinedOutput(); err != nil {
		t.Fatalf("build child failed: %v\n%s", err, combined)
	}
}
