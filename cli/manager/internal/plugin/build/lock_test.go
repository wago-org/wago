package build

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

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
