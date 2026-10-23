package build

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestBuildLockContextStopsWaitingWithoutEntering(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "plugins")
	if err := os.Mkdir(dir+".lock", 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- withBuildLockContext(ctx, dir, func() error {
			return errors.New("canceled build entered a locked module")
		})
	}()
	time.Sleep(2 * buildLockPoll)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("waiting on build lock returned %v, want cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled builder remained blocked on a build lock")
	}
	if _, err := os.Stat(dir + ".lock"); err != nil {
		t.Fatalf("canceled waiter removed another builder's lock: %v", err)
	}
}

func TestBuildLockTreatsInaccessibleExistingDirectoryAsContention(t *testing.T) {
	lockDir := filepath.Join(t.TempDir(), "plugins.lock")
	if err := os.Mkdir(lockDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if !buildLockContended(lockDir, os.ErrPermission) {
		t.Fatal("existing inaccessible lock was not treated as contention")
	}
	if buildLockContended(filepath.Join(t.TempDir(), "missing.lock"), os.ErrPermission) {
		t.Fatal("missing inaccessible lock was treated as contention")
	}
}

func TestBuildLockSerializesModuleAccess(t *testing.T) {
	dir := t.TempDir() + "/plugins"
	entered := make(chan struct{})
	release := make(chan struct{})
	var active atomic.Int32
	var overlap atomic.Bool
	var wg sync.WaitGroup

	locked := func(first bool) {
		defer wg.Done()
		if err := withBuildLock(dir, func() error {
			if active.Add(1) != 1 {
				overlap.Store(true)
			}
			defer active.Add(-1)
			if first {
				close(entered)
				<-release
			}
			return nil
		}); err != nil {
			t.Errorf("withBuildLock: %v", err)
		}
	}

	wg.Add(2)
	go locked(true)
	<-entered
	go locked(false)
	time.Sleep(3 * buildLockPoll)
	if got := active.Load(); got != 1 {
		t.Fatalf("active operations = %d, want 1", got)
	}
	close(release)
	wg.Wait()
	if overlap.Load() {
		t.Fatal("plugin build module operations overlapped")
	}
}

func TestBuildLockRetriesRemovalRace(t *testing.T) {
	lockDir := filepath.Join(t.TempDir(), "plugins.lock")
	attempts := 0
	err := acquireBuildLock(lockDir, func(path string, mode os.FileMode) error {
		attempts++
		if attempts <= 4 {
			// Windows can deny mkdir for several polls while removal is pending.
			return &os.PathError{Op: "mkdir", Path: path, Err: os.ErrPermission}
		}
		return os.Mkdir(path, mode)
	})
	if err != nil {
		t.Fatalf("acquire after removal: %v", err)
	}
	defer os.Remove(lockDir)
	if attempts != 5 {
		t.Fatalf("mkdir attempts = %d, want 5", attempts)
	}
	if info, err := os.Stat(lockDir); err != nil || !info.IsDir() {
		t.Fatalf("lock was not acquired: %v", err)
	}
}

func TestBuildLockReportsPersistentPermissionFailure(t *testing.T) {
	lockDir := filepath.Join(t.TempDir(), "plugins.lock")
	ctx, cancel := context.WithTimeout(context.Background(), 3*buildLockPermissionGrace)
	defer cancel()
	attempts := 0
	err := acquireBuildLockContext(ctx, lockDir, func(path string, _ os.FileMode) error {
		attempts++
		return &os.PathError{Op: "mkdir", Path: path, Err: os.ErrPermission}
	})
	if !errors.Is(err, os.ErrPermission) {
		t.Fatalf("acquire error = %v, want permission error before context deadline", err)
	}
	if attempts < 3 {
		t.Fatalf("mkdir attempts = %d, want repeated retries during removal", attempts)
	}
}

func TestBuildLockPermissionRetryHonorsCancellation(t *testing.T) {
	lockDir := filepath.Join(t.TempDir(), "plugins.lock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	attempts := 0
	err := acquireBuildLockContext(ctx, lockDir, func(path string, _ os.FileMode) error {
		attempts++
		cancel()
		return &os.PathError{Op: "mkdir", Path: path, Err: os.ErrPermission}
	})
	if !errors.Is(err, context.Canceled) || attempts != 1 {
		t.Fatalf("acquire error = %v, attempts = %d; want cancellation after first denial", err, attempts)
	}
}

func TestBuildLockReportsOtherErrorsWithoutRetry(t *testing.T) {
	lockDir := filepath.Join(t.TempDir(), "plugins.lock")
	attempts := 0
	err := acquireBuildLock(lockDir, func(path string, _ os.FileMode) error {
		attempts++
		return &os.PathError{Op: "mkdir", Path: path, Err: os.ErrInvalid}
	})
	if !errors.Is(err, os.ErrInvalid) || attempts != 1 {
		t.Fatalf("acquire error = %v, attempts = %d; want original error without retry", err, attempts)
	}
}
