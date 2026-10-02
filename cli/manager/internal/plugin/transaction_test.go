package plugin

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/wago-org/wago/cli/internal/project"
	"github.com/wago-org/wago/internal/atomicfile"
)

func TestPublishPluginTransactionRollsBackManifestLockAndArtifact(t *testing.T) {
	root := t.TempDir()
	manifestDir := filepath.Join(root, "project")
	buildDir := filepath.Join(root, "build")
	stagedDir := filepath.Join(root, "stage")
	for _, dir := range []string{manifestDir, buildDir, stagedDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	oldManifest, oldLock := []byte("old manifest\n"), []byte("old lock\n")
	if err := os.WriteFile(project.Path(manifestDir), oldManifest, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(project.LockPath(manifestDir), oldLock, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(buildDir, "artifact"), []byte("old artifact"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stagedDir, "artifact"), []byte("new artifact"), 0o755); err != nil {
		t.Fatal(err)
	}
	previous := replacePluginFile
	replacePluginFile = func(destination string, options atomicfile.Options, write func(io.Writer) error) error {
		if destination == project.LockPath(manifestDir) {
			return errors.New("injected lock publication failure")
		}
		return atomicfile.ReplaceFile(destination, options, write)
	}
	t.Cleanup(func() { replacePluginFile = previous })

	err := publishPluginTransaction(manifestDir, buildDir, stagedDir, []byte("new manifest\n"), []byte("new lock\n"))
	if err == nil {
		t.Fatal("publication failure was ignored")
	}
	assertFileBytes(t, project.Path(manifestDir), oldManifest)
	assertFileBytes(t, project.LockPath(manifestDir), oldLock)
	assertFileBytes(t, filepath.Join(buildDir, "artifact"), []byte("old artifact"))
}

func TestPublishPluginTransactionPublishesCompleteStagedState(t *testing.T) {
	root := t.TempDir()
	manifestDir := filepath.Join(root, "project")
	buildDir := filepath.Join(root, "build")
	stagedDir := filepath.Join(root, "stage")
	for _, dir := range []string{manifestDir, stagedDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(stagedDir, "artifact"), []byte("new artifact"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := publishPluginTransaction(manifestDir, buildDir, stagedDir, []byte("new manifest\n"), []byte("new lock\n")); err != nil {
		t.Fatal(err)
	}
	assertFileBytes(t, project.Path(manifestDir), []byte("new manifest\n"))
	assertFileBytes(t, project.LockPath(manifestDir), []byte("new lock\n"))
	assertFileBytes(t, filepath.Join(buildDir, "artifact"), []byte("new artifact"))
}

func assertFileBytes(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("%s = %q, want %q", path, got, want)
	}
}
