// Package filelock provides small cross-process advisory file locks.
package filelock

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const pollInterval = 10 * time.Millisecond

// Lock is an acquired advisory lock. Close releases it.
type Lock struct {
	file *os.File
}

// Acquire opens path with restrictive permissions and waits for an exclusive
// cross-process lock until ctx is canceled.
func Acquire(ctx context.Context, path string) (*Lock, error) {
	if ctx == nil {
		return nil, errors.New("nil lock context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	file, err := openCreateLock(path)
	if err != nil {
		return nil, err
	}
	if err := validateLockFile(file, path); err != nil {
		_ = file.Close()
		return nil, err
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return nil, err
	}
	return acquireOpened(ctx, file, path)
}

// acquireOpened owns file, including on cancellation or retirement.
func acquireOpened(ctx context.Context, file *os.File, path string) (*Lock, error) {
	var timer *time.Timer
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	for {
		if err := ctx.Err(); err != nil {
			_ = file.Close()
			return nil, err
		}
		locked, err := tryLock(file)
		if err != nil {
			_ = file.Close()
			return nil, fmt.Errorf("lock %s: %w", path, err)
		}
		if locked {
			lock := &Lock{file: file}
			// A waiter may have opened the inode before an uninstall retired it.
			if err := validateLockFile(file, path); err != nil {
				lock.Close()
				return nil, err
			}
			return lock, nil
		}
		if timer == nil {
			timer = time.NewTimer(pollInterval)
		} else {
			// The preceding retry consumed timer.C, including on Go 1.22.
			timer.Reset(pollInterval)
		}
		select {
		case <-ctx.Done():
			_ = file.Close()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

// Retire removes this lock's pathname while it is still held, then closes it.
// All protected destructive work must finish first: a new owner may create a
// fresh lock as soon as the path is removed. Waiters on this inode reject it.
func (lock *Lock) Retire(path string) error {
	retired, err := lock.retireName(path)
	if err != nil {
		return err
	}
	return errors.Join(os.Remove(retired), lock.Close())
}

// Rotate invalidates old waiters and returns ownership of a fresh coordinator.
// Keep the retired inode alive until the new one exists so its identity cannot
// immediately be recycled. Callers must retain their pending operation marker
// across this transition; another process can acquire the fresh name first.
func (lock *Lock) Rotate(ctx context.Context, path string) (*Lock, error) {
	retired, err := lock.retireName(path)
	if err != nil {
		return nil, err
	}
	next, err := Acquire(ctx, path)
	cleanupErr := errors.Join(os.Remove(retired), lock.Close())
	if err != nil || cleanupErr != nil {
		if next != nil {
			next.Close()
		}
		return nil, errors.Join(err, cleanupErr)
	}
	return next, nil
}

func (lock *Lock) retireName(path string) (string, error) {
	if lock == nil || lock.file == nil {
		return "", errors.New("cannot retire an unheld lock")
	}
	if err := validateLockFile(lock.file, path); err != nil {
		return "", err
	}
	// Rename first: Windows can leave a delete-pending pathname visible to
	// existing handles. Waiters must lose the coordinator name immediately.
	retired, err := os.CreateTemp(filepath.Dir(path), ".retired-lock-*")
	if err != nil {
		return "", err
	}
	retiredPath := retired.Name()
	if err := retired.Close(); err != nil {
		os.Remove(retiredPath)
		return "", err
	}
	if err := os.Rename(path, retiredPath); err != nil {
		os.Remove(retiredPath)
		return "", err
	}
	return retiredPath, nil
}

func validateLockFile(file *os.File, path string) error {
	opened, err := file.Stat()
	if err != nil {
		return err
	}
	linked, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !opened.Mode().IsRegular() || linked.Mode()&os.ModeSymlink != 0 || !linked.Mode().IsRegular() || !os.SameFile(opened, linked) {
		return fmt.Errorf("lock file %s is not a stable regular file", path)
	}
	return nil
}

// Close releases the lock and closes its file descriptor.
func (lock *Lock) Close() error {
	if lock == nil || lock.file == nil {
		return nil
	}
	file := lock.file
	lock.file = nil
	unlockErr := unlock(file)
	closeErr := file.Close()
	return errors.Join(unlockErr, closeErr)
}

// TryAcquireExisting takes an exclusive lock without waiting or creating files.
// A nil lock and nil error mean that another process holds the lock.
func TryAcquireExisting(path string) (*Lock, error) { return tryExisting(path, false) }

// TryAcquireSharedExisting takes a shared lock on a readable existing file.
// Validation after acquisition rejects a file unlinked while acquisition raced.
func TryAcquireSharedExisting(path string) (*Lock, error) { return tryExisting(path, true) }

func tryExisting(path string, shared bool) (*Lock, error) {
	file, err := openExistingLock(path, shared)
	if err != nil {
		return nil, err
	}
	if err := validateLockFile(file, path); err != nil {
		file.Close()
		return nil, err
	}
	var locked bool
	if shared {
		locked, err = trySharedLock(file)
	} else {
		locked, err = tryLock(file)
	}
	if err != nil || !locked {
		file.Close()
		return nil, err
	}
	lock := &Lock{file: file}
	if err := validateLockFile(file, path); err != nil {
		lock.Close()
		return nil, err
	}
	return lock, nil
}

// Descriptor returns the held descriptor for a platform exec boundary. The
// caller must keep the Lock live and must not close or unlock the descriptor.
func (lock *Lock) Descriptor() uintptr { return lock.file.Fd() }

// AdoptSharedExisting takes ownership of an inherited file descriptor. It
// validates the expected lock-file identity and establishes shared ownership on
// that same open file description, without opening a second lease. The caller
// must already have restored close-on-exec and must not retain another owner.
// The file is closed on every failure.
func AdoptSharedExisting(file *os.File, path string) (*Lock, error) {
	if file == nil {
		return nil, errors.New("nil inherited lock file")
	}
	if err := validateLockFile(file, path); err != nil {
		file.Close()
		return nil, err
	}
	locked, err := trySharedLock(file)
	if err != nil || !locked {
		file.Close()
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("inherited lease is being retired")
	}
	lock := &Lock{file: file}
	if err := validateLockFile(file, path); err != nil {
		lock.Close()
		return nil, err
	}
	return lock, nil
}
