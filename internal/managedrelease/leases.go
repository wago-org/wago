package managedrelease

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/wago-org/wago/internal/filelock"
)

const leaseFile = ".lease"
const publishedFile = ".published"
const retiringLeaseFile = ".retiring-lease"

var processLease struct {
	sync.Once
	lock *filelock.Lock
	err  error
}

// A directly launched current-format payload pins its own source for its
// process lifetime. The launcher also pins legacy payloads across execution.
func pinProcess(executable string) error {
	processLease.Do(func() {
		processLease.lock, processLease.err = filelock.TryAcquireSharedExisting(filepath.Join(filepath.Dir(executable), leaseFile))
		if os.IsNotExist(processLease.err) && legacyRelease(executable) {
			// Releases predating leases are not eligible for automatic pruning.
			processLease.err = nil
			return
		}
		if processLease.err == nil && processLease.lock == nil {
			processLease.err = fmt.Errorf("manager release is being retired")
		}
		if processLease.err == nil {
			processLease.err = inheritLease(processLease.lock)
		}
	})
	return processLease.err
}

func legacyRelease(executable string) bool {
	_, err := os.Stat(filepath.Join(filepath.Dir(executable), publishedFile))
	return os.IsNotExist(err) && SourceForExecutable(executable) != ""
}

func selectedLease(executable string) (string, *filelock.Lock, error) {
	for attempt := 0; attempt < 16; attempt++ {
		target, err := SelectedBinary(executable)
		if err != nil {
			return "", nil, err
		}
		lease, err := filelock.TryAcquireSharedExisting(filepath.Join(filepath.Dir(target), leaseFile))
		if os.IsNotExist(err) {
			if legacyRelease(target) {
				// Migration: old immutable releases are never pruned automatically.
				return target, nil, nil
			}
			continue
		}
		if err != nil {
			return "", nil, err
		}
		if lease != nil {
			return target, lease, nil
		}
	}
	return "", nil, fmt.Errorf("manager release changed during startup; retry the command")
}

// pruneReleases runs under the publication lock. Only successfully published
// current-format releases are eligible; failed-publication recovery data stays.
func pruneReleases(root string, record Record) error {
	directory := filepath.Join(root, releasesDir)
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		path := filepath.Join(directory, name)
		if strings.HasPrefix(name, ".retired-release-") {
			if err := os.RemoveAll(path); err != nil {
				return err
			}
			continue
		}
		if !entry.IsDir() || !strings.HasPrefix(name, "release-") || name == record.Release || name == record.Previous {
			continue
		}
		if _, err := os.Stat(filepath.Join(path, publishedFile)); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return err
		}
		leasePath := filepath.Join(path, leaseFile)
		lease, err := filelock.TryAcquireExisting(leasePath)
		retiring := false
		if os.IsNotExist(err) {
			// A prior cleanup may have retired the lease before a Windows
			// sharing conflict prevented the directory rename.
			leasePath = filepath.Join(path, retiringLeaseFile)
			lease, err = filelock.TryAcquireExisting(leasePath)
			retiring = true
		}
		if err != nil {
			return err
		}
		if lease == nil {
			continue // A running payload or inherited child still owns this pair.
		}
		retired := filepath.Join(directory, ".retired-"+name)
		// Retire the lease name before releasing it. Windows cannot rename
		// the containing directory while this child handle remains open.
		if !retiring {
			err = os.Rename(leasePath, filepath.Join(path, retiringLeaseFile))
		}
		closeErr := lease.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		if err := renameRetiredDirectory(path, retired); err != nil {
			return err
		}
		// The lease name disappeared before unlock, so old waiters and fresh
		// launchers cannot acquire a valid lease on this retired directory.
		if err := os.RemoveAll(retired); err != nil {
			return err
		}
	}
	return syncDirectory(directory)
}
