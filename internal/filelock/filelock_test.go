package filelock

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLockRejectsPreCanceledContextWithoutCreatingFiles(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "credentials")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Acquire(ctx, filepath.Join(directory, "credentials.lock")); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-canceled acquire = %v", err)
	}
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatalf("pre-canceled acquire created lock directory: %v", err)
	}
}

func TestLockSerializesAndHonorsCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.lock")
	first, err := Acquire(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := Acquire(ctx, path); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("contended acquire = %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := Acquire(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestLockRejectsNonRegularPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Acquire(context.Background(), path); err == nil {
		t.Fatal("Acquire accepted a directory as a lock file")
	}
}

func TestSharedLeasesBlockRetirement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lease")
	if err := os.WriteFile(path, nil, 0644); err != nil {
		t.Fatal(err)
	}
	first, err := TryAcquireSharedExisting(path)
	if err != nil || first == nil {
		t.Fatalf("first reader: %v", err)
	}
	defer first.Close()
	second, err := TryAcquireSharedExisting(path)
	if err != nil || second == nil {
		t.Fatalf("second reader: %v", err)
	}
	defer second.Close()
	for i := 0; i < 2; i++ {
		exclusive, err := TryAcquireExisting(path)
		if err != nil || exclusive != nil {
			if exclusive != nil {
				exclusive.Close()
			}
			t.Fatalf("retirement with live reader: %v", err)
		}
		if i == 0 {
			if err := first.Close(); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	exclusive, err := TryAcquireExisting(path)
	if err != nil || exclusive == nil {
		t.Fatalf("retirement after readers exit: %v", err)
	}
	defer exclusive.Close()
	reader, err := TryAcquireSharedExisting(path)
	if err != nil || reader != nil {
		if reader != nil {
			reader.Close()
		}
		t.Fatalf("reader entered retiring release: %v", err)
	}
}

func TestExistingLeaseDoesNotCreateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing")
	for _, acquire := range []func(string) (*Lock, error){TryAcquireExisting, TryAcquireSharedExisting} {
		lock, err := acquire(path)
		if lock != nil {
			lock.Close()
			t.Fatal("acquired missing file")
		}
		if !os.IsNotExist(err) {
			t.Fatalf("missing lease: %v", err)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("created lease: %v", err)
		}
	}
}

func TestRetiredLockRejectsAlreadyOpenedWaiter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "publication.lock")
	owner, err := Acquire(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	waiter, err := openCreateLock(path)
	if err != nil {
		t.Fatal(err)
	}
	// The descriptor predates retirement, even if the waiter is scheduled later.
	if err := owner.Retire(path); err != nil {
		waiter.Close()
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	stale, err := acquireOpened(ctx, waiter, path)
	if stale != nil {
		stale.Close()
		t.Fatal("waiter acquired retired coordinator")
	}
	if err == nil {
		t.Fatal("retirement was not reported")
	}
	fresh, err := Acquire(ctx, path)
	if err != nil {
		t.Fatalf("new installation: %v", err)
	}
	if err := fresh.Retire(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("retired pathname remains: %v", err)
	}
}
