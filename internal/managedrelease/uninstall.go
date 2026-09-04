package managedrelease

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/wago-org/wago/internal/filelock"
)

const uninstallPendingFile = ".wago-uninstalling"

// UninstallPendingPath names the cleanup intent protected by the coordinator.
func UninstallPendingPath(lockPath string) string {
	return filepath.Join(filepath.Dir(lockPath), uninstallPendingFile)
}

// MarkUninstallPending runs while the caller holds the publication lock. Its
// marker transfers exclusive cleanup intent across a deferred worker handoff.
// The returned undo is only for a failed worker start; an existing marker is
// retained so that a second scheduling failure cannot cancel prior cleanup.
func MarkUninstallPending(lockPath string) (func() error, error) {
	path := UninstallPendingPath(lockPath)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if os.IsExist(err) {
		return func() error { return nil }, nil
	}
	if err != nil {
		return nil, err
	}
	if err := errors.Join(file.Sync(), file.Close()); err != nil {
		return nil, errors.Join(err, os.Remove(path))
	}
	if err := syncDirectory(filepath.Dir(path)); err != nil {
		return nil, errors.Join(err, os.Remove(path))
	}
	return func() error {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return syncDirectory(filepath.Dir(path))
	}, nil
}

func lockForPublication(root string) (*filelock.Lock, error) {
	lockPath := filepath.Join(root, publicationLockFile)
	lock, err := filelock.Acquire(context.Background(), lockPath)
	if err != nil {
		return nil, err
	}
	pendingPath := UninstallPendingPath(lockPath)
	if _, err := os.Lstat(pendingPath); !os.IsNotExist(err) {
		lock.Close()
		if err != nil {
			return nil, fmt.Errorf("inspect pending manager cleanup: %w", err)
		}
		return nil, fmt.Errorf("manager uninstall is pending at %s; finish cleanup before installing", pendingPath)
	}
	return lock, nil
}
