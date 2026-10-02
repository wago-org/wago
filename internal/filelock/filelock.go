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

// Lock is an acquired exclusive lock. Close releases it.
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
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return nil, err
	}
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
			return &Lock{file: file}, nil
		}
		timer := time.NewTimer(pollInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			_ = file.Close()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
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
