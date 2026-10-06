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
	"github.com/wago-org/wago/internal/atomicfile"
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
	if _, err := wago.LoadTrustedArtifact(artifact); err != nil {
		t.Fatalf("load build output: %v", err)
	}
}

func TestBuildOutputSnapshotRejectsSymlinkChanges(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.wago")
	second := filepath.Join(dir, "second.wago")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, []byte("artifact"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	output := filepath.Join(dir, "output.wago")
	if err := os.Symlink(filepath.Base(first), output); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	requireTestSymlink(t, output)
	snapshot, err := inspectBuildOutput(output)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(output); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Base(second), output); err != nil {
		t.Fatal(err)
	}
	requireTestSymlink(t, output)
	if _, _, _, _, err := snapshot.revalidate(); err == nil || !strings.Contains(err.Error(), "changed during build") {
		t.Fatalf("revalidate changed symlink = %v", err)
	}
}

func TestBuildOutputSnapshotRejectsTargetReplacement(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.wago")
	if err := os.WriteFile(target, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "output.wago")
	if err := os.Symlink(filepath.Base(target), output); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	requireTestSymlink(t, output)
	snapshot, err := inspectBuildOutput(output)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(target, filepath.Join(dir, "old-target.wago")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := snapshot.revalidate(); err == nil || !strings.Contains(err.Error(), "changed during build") {
		t.Fatalf("revalidate replaced target = %v", err)
	}
}

func TestBuildOutputSnapshotRejectsTargetDisappearance(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.wago")
	const original = "existing artifact"
	if err := os.WriteFile(target, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "output.wago")
	if err := os.Symlink(filepath.Base(target), output); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	requireTestSymlink(t, output)
	snapshot, err := inspectBuildOutput(output)
	if err != nil {
		t.Fatal(err)
	}
	previous := filepath.Join(dir, "previous.wago")
	if err := os.Rename(target, previous); err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := snapshot.revalidate(); err == nil || !strings.Contains(err.Error(), "output symlink changed during build") {
		t.Fatalf("revalidate disappeared target = %v", err)
	}
	if link, err := os.Readlink(output); err != nil || link != filepath.Base(target) {
		t.Fatalf("output symlink changed: %q, %v", link, err)
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatalf("missing target was recreated: %v", err)
	}
	if got, err := os.ReadFile(previous); err != nil || string(got) != original {
		t.Fatalf("previous artifact = %q, %v", got, err)
	}
}

func TestBuildOutputSnapshotTracksMissingSymlinkTarget(t *testing.T) {
	dir := t.TempDir()
	targetDir := filepath.Join(dir, "artifacts")
	if err := os.Mkdir(targetDir, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(targetDir, "new.wago")
	intermediate := filepath.Join(dir, "current")
	if err := os.Symlink(filepath.Join("artifacts", filepath.Base(target)), intermediate); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	requireTestSymlink(t, intermediate)
	output := filepath.Join(dir, "output.wago")
	if err := os.Symlink(filepath.Base(intermediate), output); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	requireTestSymlink(t, output)

	snapshot, err := inspectBuildOutput(output)
	if err != nil {
		t.Fatal(err)
	}
	expectedPath, err := resolveBuildOutputPublicationPath(target)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.linkInfo == nil || snapshot.targetInfo != nil || filepath.Clean(snapshot.publicationPath) != filepath.Clean(expectedPath) {
		t.Fatalf("missing-target snapshot = %#v, want link and publication path %s", snapshot, expectedPath)
	}
	path, mode, exists, info, err := snapshot.revalidate()
	if err != nil || filepath.Clean(path) != filepath.Clean(expectedPath) || mode != 0o644 || exists || info != nil {
		t.Fatalf("missing-target revalidation = %q, %o, %v, %v, %v", path, mode, exists, info, err)
	}

	if err := os.WriteFile(target, []byte("concurrent artifact"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := snapshot.revalidate(); err == nil || !strings.Contains(err.Error(), "changed during build") {
		t.Fatalf("revalidate appeared symlink target = %v", err)
	}
}

func TestBuildOutputSnapshotRejectsMissingSymlinkRetarget(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "artifacts"), 0o700); err != nil {
		t.Fatal(err)
	}
	intermediate := filepath.Join(dir, "current")
	if err := os.Symlink(filepath.Join("artifacts", "first.wago"), intermediate); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	requireTestSymlink(t, intermediate)
	output := filepath.Join(dir, "output.wago")
	if err := os.Symlink(filepath.Base(intermediate), output); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	requireTestSymlink(t, output)
	snapshot, err := inspectBuildOutput(output)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(intermediate); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("artifacts", "second.wago"), intermediate); err != nil {
		t.Fatal(err)
	}
	requireTestSymlink(t, intermediate)
	if _, _, _, _, err := snapshot.revalidate(); err == nil || !strings.Contains(err.Error(), "changed during build") {
		t.Fatalf("revalidate retargeted missing symlink = %v", err)
	}
}

func TestBuildOutputSnapshotRejectsRegularReplacement(t *testing.T) {
	dir := t.TempDir()
	output := filepath.Join(dir, "output.wago")
	if err := os.WriteFile(output, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := inspectBuildOutput(output)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(output, filepath.Join(dir, "old-output.wago")); err != nil {
		t.Fatal(err)
	}
	// Keep size and mode identical so the regression relies on stable file
	// identity instead of incidental metadata differences.
	if err := os.WriteFile(output, []byte("other"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := snapshot.revalidate(); err == nil || !strings.Contains(err.Error(), "changed during build") {
		t.Fatalf("revalidate replaced regular output = %v", err)
	}
}

func TestBuildSnapshotCallbackRejectsPublicationChanges(t *testing.T) {
	for _, initiallyExists := range []bool{true, false} {
		name := "missing-to-created"
		if initiallyExists {
			name = "existing-replaced"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			output := filepath.Join(dir, "output.wago")
			preserved := filepath.Join(dir, "preserved-output.wago")
			if initiallyExists {
				if err := os.WriteFile(output, []byte("original artifact"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			snapshot, err := inspectBuildOutput(output)
			if err != nil {
				t.Fatal(err)
			}
			publicationPath, _, _, outputInfo, err := snapshot.revalidate()
			if err != nil {
				t.Fatal(err)
			}
			outputMetadata, err := captureBuildOutputMetadata(publicationPath, outputInfo, snapshot.targetIdentity)
			if err != nil {
				t.Fatal(err)
			}
			if outputInfo == nil {
				outputMetadata, err = captureNewBuildOutputMetadata(publicationPath)
				if err != nil {
					t.Fatal(err)
				}
			}
			options := atomicfile.Options{
				Mode: 0o600, ModeSet: true, Sync: true,
				BeforeReplace: func(destination string) error {
					return snapshot.validateBeforeReplace(destination, outputInfo, outputMetadata)
				},
				Hooks: &atomicfile.Hooks{Sync: func(file *os.File) error {
					if err := file.Sync(); err != nil {
						return err
					}
					if initiallyExists {
						if err := os.Rename(output, preserved); err != nil {
							return err
						}
					}
					return os.WriteFile(output, []byte("concurrent artifact"), 0o600)
				}},
			}
			err = atomicfile.ReplaceFile(publicationPath, options, func(writer io.Writer) error {
				_, err := io.WriteString(writer, "new compiled artifact")
				return err
			})
			if err == nil || !strings.Contains(err.Error(), "before publication") {
				t.Fatalf("snapshot callback after destination change = %v", err)
			}
			if got, err := os.ReadFile(output); err != nil || string(got) != "concurrent artifact" {
				t.Fatalf("concurrent output = %q, %v", got, err)
			}
			if initiallyExists {
				if got, err := os.ReadFile(preserved); err != nil || string(got) != "original artifact" {
					t.Fatalf("preserved original = %q, %v", got, err)
				}
			}
			assertNoAtomicBuildTemps(t, dir)
		})
	}
}

func TestBuildSnapshotCallbackPinsSymlinkedParent(t *testing.T) {
	for _, initiallyExists := range []bool{true, false} {
		name := "missing"
		if initiallyExists {
			name = "existing"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			first := filepath.Join(dir, "first")
			second := filepath.Join(dir, "second")
			for _, path := range []string{first, second} {
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			parent := filepath.Join(dir, "published")
			if err := os.Symlink(filepath.Base(first), parent); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}
			requireTestSymlink(t, parent)
			requested := filepath.Join(parent, "artifact.wago")
			physical := filepath.Join(first, "artifact.wago")
			expectedPhysical, err := resolveBuildOutputPublicationPath(physical)
			if err != nil {
				t.Fatal(err)
			}
			if initiallyExists {
				if err := os.WriteFile(physical, []byte("original artifact"), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			snapshot, err := inspectBuildOutput(requested)
			if err != nil {
				t.Fatal(err)
			}
			publicationPath, mode, _, outputInfo, err := snapshot.revalidate()
			if err != nil {
				t.Fatal(err)
			}
			if filepath.Clean(publicationPath) != filepath.Clean(expectedPhysical) {
				t.Fatalf("physical publication path = %q, want %q", publicationPath, expectedPhysical)
			}
			outputMetadata, err := captureBuildOutputMetadata(publicationPath, outputInfo, snapshot.targetIdentity)
			if err != nil {
				t.Fatal(err)
			}
			options := atomicfile.Options{
				Mode: mode, ModeSet: true, ApplyUmask: !initiallyExists, Sync: true,
				RequireExistingParent: true, RetainReplaceHandle: true,
				BeforeReplace: func(destination string) error {
					return snapshot.validateBeforeReplace(destination, outputInfo, outputMetadata)
				},
				Hooks: &atomicfile.Hooks{Sync: func(file *os.File) error {
					if err := file.Sync(); err != nil {
						return err
					}
					if err := os.Remove(parent); err != nil {
						return err
					}
					return os.Symlink(filepath.Base(second), parent)
				}},
			}
			err = atomicfile.ReplaceFile(publicationPath, options, func(writer io.Writer) error {
				_, err := io.WriteString(writer, "new compiled artifact")
				return err
			})
			if err == nil || !strings.Contains(err.Error(), "changed") {
				t.Fatalf("publication after parent symlink swap = %v", err)
			}
			if initiallyExists {
				if got, err := os.ReadFile(physical); err != nil || string(got) != "original artifact" {
					t.Fatalf("preserved physical output = %q, %v", got, err)
				}
			} else if _, err := os.Stat(physical); !os.IsNotExist(err) {
				t.Fatalf("rejected publication created physical output: %v", err)
			}
			for _, path := range []string{first, second} {
				assertNoAtomicBuildTemps(t, path)
			}
		})
	}
}
