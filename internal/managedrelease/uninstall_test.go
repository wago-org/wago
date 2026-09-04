package managedrelease

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wago-org/wago/internal/filelock"
)

func TestPendingUninstallBlocksPreparationAndPublication(t *testing.T) {
	launcher := filepath.Join(t.TempDir(), executableName())
	old := testRelease(t, launcher, "old")
	if err := Publish(old, nil, nil); err != nil {
		t.Fatal(err)
	}
	next := testRelease(t, launcher, "next")
	lockPath := PublicationLockPath(launcher)
	owner, err := filelock.Acquire(context.Background(), lockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	undo, err := MarkUninstallPending(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer undo()
	// Simulate the gap after scheduling, before the worker can take the lock.
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	wrote := false
	_, err = Prepare(launcher, "during cleanup", func(string, string) error { wrote = true; return errors.New("unexpected write") }, nil)
	if wrote || err == nil || !strings.Contains(err.Error(), "uninstall is pending") {
		t.Errorf("prepare during handoff: wrote=%v, err=%v", wrote, err)
	}
	bootstrapped := false
	err = Publish(next, func() (func() error, error) { bootstrapped = true; return nil, nil }, nil)
	if bootstrapped || err == nil || !strings.Contains(err.Error(), "uninstall is pending") {
		t.Errorf("publish during handoff: bootstrap=%v, err=%v", bootstrapped, err)
	}
	if selected, err := SelectedBinary(launcher); err != nil || selected != old.Binary() {
		t.Fatalf("selection changed during handoff: %q, %v", selected, err)
	}
	if err := undo(); err != nil {
		t.Fatal(err)
	}
	if err := Publish(next, nil, nil); err != nil {
		t.Fatalf("publish after canceled scheduling: %v", err)
	}
}

func TestPendingUninstallRollbackPreservesEarlierCleanup(t *testing.T) {
	lockPath := PublicationLockPath(filepath.Join(t.TempDir(), executableName()))
	owner, err := filelock.Acquire(context.Background(), lockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	firstUndo, err := MarkUninstallPending(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer firstUndo()
	secondUndo, err := MarkUninstallPending(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := secondUndo(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(UninstallPendingPath(lockPath)); err != nil {
		t.Fatalf("second scheduling failure canceled prior cleanup: %v", err)
	}
}
