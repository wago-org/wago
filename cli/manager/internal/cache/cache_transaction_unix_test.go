//go:build linux || darwin

package cache

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wago-org/wago/cli/internal/project"
	"github.com/wago-org/wago/internal/wagopaths"
)

func TestCleanWaitsForGlobalPluginTransaction(t *testing.T) {
	workingDir := t.TempDir()
	previousDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(workingDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previousDir) })

	dataDir := t.TempDir()
	dirs := wagopaths.Dirs{Data: dataDir, Versions: filepath.Join(dataDir, "versions")}
	buildDir := filepath.Join(dirs.Versions, "v1", "standard", "normal")
	plugins := filepath.Join(buildDir, "plugins")
	if err := os.MkdirAll(plugins, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(plugins, "prior.wago"), []byte("rollback data"), 0o600); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(buildDir, ".wago-plugin-previous-test")
	type cleanResult struct {
		result Result
		err    error
	}
	done := make(chan cleanResult, 1)
	returnedWhileLocked := false
	err = project.WithMutation(context.Background(), dataDir, func(*project.Mutation) error {
		go func() {
			result, cleanErr := Clean(dirs, Selection{Builds: true})
			done <- cleanResult{result: result, err: cleanErr}
		}()
		// The publisher holds this same lock while it renames the previous
		// plugin build to a rollback backup. Cleanup must wait before opening
		// that build, otherwise its descriptor walker can erase the backup.
		select {
		case <-done:
			returnedWhileLocked = true
		case <-time.After(250 * time.Millisecond):
		}
		if returnedWhileLocked {
			return nil
		}
		return os.Rename(plugins, backup)
	})
	if err != nil {
		t.Fatal(err)
	}
	if returnedWhileLocked {
		t.Fatal("cleanup completed while a global plugin transaction held the mutation lock")
	}
	select {
	case cleaned := <-done:
		if cleaned.err != nil {
			t.Fatal(cleaned.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cleanup did not finish after the mutation lock was released")
	}
	if data, err := os.ReadFile(filepath.Join(backup, "prior.wago")); err != nil || string(data) != "rollback data" {
		t.Fatalf("transaction backup was modified by cleanup: %q, %v", data, err)
	}
}

func TestDownloadsOnlyCleanDoesNotCreateProjectLockState(t *testing.T) {
	root := t.TempDir()
	dirs := wagopaths.Dirs{
		Data:  filepath.Join(root, "data"),
		Cache: filepath.Join(root, "downloads", "v1"),
	}
	if err := os.MkdirAll(filepath.Dir(dirs.Cache), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(dirs.Cache), "archive"), []byte("download"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Clean(dirs, Selection{Downloads: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(dirs.Data, ".wago")); !os.IsNotExist(err) {
		t.Fatalf("downloads-only cleanup created project lock state: %v", err)
	}
}
