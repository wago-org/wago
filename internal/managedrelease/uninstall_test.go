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

func TestStaleCleanupRecoveryRetiresCoordinator(t *testing.T) {
	for _, scenario := range []string{"legacy", "dead scheduler", "dead worker", "reused pid"} {
		t.Run(scenario, func(t *testing.T) {
			oldIdentity := uninstallProcessIdentity
			defer func() { uninstallProcessIdentity = oldIdentity }()
			created := uint64(123)
			alive := true
			uninstallProcessIdentity = func(int) (uint64, bool, error) { return created, alive, nil }
			root := t.TempDir()
			lockPath := PublicationLockPath(filepath.Join(root, executableName()))
			owner, err := filelock.Acquire(context.Background(), lockPath)
			if err != nil {
				t.Fatal(err)
			}
			defer owner.Close()
			before, err := os.Stat(lockPath)
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "legacy" {
				if err := os.WriteFile(UninstallPendingPath(lockPath), nil, 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				operation, _, _, err := BeginUninstallPending(lockPath)
				if err != nil {
					t.Fatal(err)
				}
				if scenario == "dead worker" {
					if err := BindUninstallWorker(lockPath, operation, 12345); err != nil {
						t.Fatal(err)
					}
				}
			}
			if scenario == "reused pid" {
				created++
			} else {
				alive = false
			}
			if err := owner.Close(); err != nil {
				t.Fatal(err)
			}
			next, err := lockForPublication(root)
			if err != nil {
				t.Fatal(err)
			}
			defer next.Close()
			after, err := os.Stat(lockPath)
			if err != nil {
				t.Fatal(err)
			}
			if os.SameFile(before, after) {
				t.Fatal("stale worker coordinator identity survived recovery")
			}
			if _, err := os.Stat(UninstallPendingPath(lockPath)); !os.IsNotExist(err) {
				t.Fatalf("stale record survived recovery: %v", err)
			}
		})
	}
}

func TestLiveCleanupWorkerBlocksPublication(t *testing.T) {
	oldIdentity := uninstallProcessIdentity
	defer func() { uninstallProcessIdentity = oldIdentity }()
	uninstallProcessIdentity = func(pid int) (uint64, bool, error) { return uint64(pid) + 1, true, nil }
	root := t.TempDir()
	lockPath := PublicationLockPath(filepath.Join(root, executableName()))
	owner, err := filelock.Acquire(context.Background(), lockPath)
	if err != nil {
		t.Fatal(err)
	}
	operation, _, undo, err := BeginUninstallPending(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer undo()
	if err := BindUninstallWorker(lockPath, operation, 12345); err != nil {
		t.Fatal(err)
	}
	owner.Close()
	next, err := lockForPublication(root)
	if next != nil {
		next.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "uninstall is pending") {
		t.Fatalf("live worker was not preserved: %v", err)
	}
}

func TestCleanupUndoDoesNotRemoveSuccessor(t *testing.T) {
	root := t.TempDir()
	lockPath := PublicationLockPath(filepath.Join(root, executableName()))
	owner, err := filelock.Acquire(context.Background(), lockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	_, _, undo, err := BeginUninstallPending(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := undo(); err != nil {
		t.Fatal(err)
	}
	operation, _, nextUndo, err := BeginUninstallPending(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer nextUndo()
	if err := undo(); err != nil {
		t.Fatal(err)
	}
	record, _, err := readPendingUninstall(UninstallPendingPath(lockPath))
	if err != nil || record.Operation != operation {
		t.Fatalf("old rollback removed new cleanup: %+v, %v", record, err)
	}
}
