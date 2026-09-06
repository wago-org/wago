package managedrelease

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/wago-org/wago/internal/atomicfile"
	"github.com/wago-org/wago/internal/filelock"
	"github.com/wago-org/wago/internal/jsonstrict"
	"github.com/wago-org/wago/internal/regularfile"
)

const uninstallPendingFile = ".wago-uninstalling"

type pendingUninstall struct {
	Format    int    `json:"format"`
	Operation string `json:"operation"`
	Phase     string `json:"phase"`
	PID       int    `json:"pid"`
	Created   uint64 `json:"created"`
}

// Process creation identity prevents a reused PID from keeping stale work alive.
var uninstallProcessIdentity = processIdentity

func UninstallPendingPath(lockPath string) string {
	return filepath.Join(filepath.Dir(lockPath), uninstallPendingFile)
}

// BeginUninstallPending records the scheduling owner under the publication lock.
// BindUninstallWorker transfers that operation to the started worker before the
// caller releases the lock. Undo affects only this operation, never a successor.
func BeginUninstallPending(lockPath string) (operation string, parentCreated uint64, undo func() error, err error) {
	created, alive, err := uninstallProcessIdentity(os.Getpid())
	if err != nil {
		return "", 0, nil, err
	}
	if !alive {
		return "", 0, nil, errors.New("cleanup owner has exited")
	}
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", 0, nil, err
	}
	record := pendingUninstall{Format: 1, Operation: hex.EncodeToString(token[:]), Phase: "preparing", PID: os.Getpid(), Created: created}
	path := UninstallPendingPath(lockPath)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", 0, nil, err
	}
	writeErr := json.NewEncoder(file).Encode(record)
	if err := errors.Join(writeErr, file.Sync(), file.Close()); err != nil {
		return "", 0, nil, errors.Join(err, os.Remove(path))
	}
	if err := syncDirectory(filepath.Dir(path)); err != nil {
		return "", 0, nil, errors.Join(err, os.Remove(path))
	}
	undo = func() error {
		current, _, err := readPendingUninstall(path)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if current.Operation != record.Operation {
			return nil
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return syncDirectory(filepath.Dir(path))
	}
	return record.Operation, created, undo, nil
}

// MarkUninstallPending preserves the older marker API. A second failed schedule
// cannot cancel an earlier operation. New schedulers use BeginUninstallPending.
func MarkUninstallPending(lockPath string) (func() error, error) {
	_, _, undo, err := BeginUninstallPending(lockPath)
	if os.IsExist(err) {
		return func() error { return nil }, nil
	}
	return undo, err
}

func BindUninstallWorker(lockPath, operation string, pid int) error {
	path := UninstallPendingPath(lockPath)
	record, _, err := readPendingUninstall(path)
	if err != nil {
		return err
	}
	if record.Operation != operation || record.Phase != "preparing" {
		return errors.New("cleanup operation changed before worker handoff")
	}
	created, alive, err := uninstallProcessIdentity(pid)
	if err != nil {
		return err
	}
	if !alive {
		return errors.New("cleanup worker exited before handoff")
	}
	record.Phase, record.PID, record.Created = "scheduled", pid, created
	if err := atomicfile.ReplaceFile(path, atomicfile.Options{Mode: 0600, Sync: true}, func(w io.Writer) error { return json.NewEncoder(w).Encode(record) }); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

func readPendingUninstall(path string) (pendingUninstall, []byte, error) {
	var record pendingUninstall
	data, err := regularfile.ReadAtomicSnapshot(path, 4096)
	if err != nil {
		return record, nil, err
	}
	if len(data) == 0 {
		return record, data, nil
	} // legacy marker; retire its coordinator before recovery
	if err := jsonstrict.ValidateTypedJSON(data, record); err != nil {
		return record, nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&record); err != nil {
		return record, nil, err
	}
	token, err := hex.DecodeString(record.Operation)
	if err != nil || len(token) != 16 || record.Format != 1 || record.PID <= 0 || uint64(record.PID) > uint64(^uint32(0)) || record.Created == 0 || (record.Phase != "preparing" && record.Phase != "scheduled") {
		return record, nil, errors.New("invalid pending manager cleanup record")
	}
	return record, data, nil
}

func lockForPublication(root string) (*filelock.Lock, error) {
	lockPath := filepath.Join(root, publicationLockFile)
	pendingPath := UninstallPendingPath(lockPath)
	var retiredData []byte
	retired := false
	lock, err := filelock.Acquire(context.Background(), lockPath)
	if err != nil {
		return nil, err
	}
	for attempt := 0; attempt < 16; attempt++ {
		record, data, err := readPendingUninstall(pendingPath)
		if os.IsNotExist(err) {
			return lock, nil
		}
		if err != nil {
			lock.Close()
			return nil, fmt.Errorf("inspect pending manager cleanup: %w", err)
		}
		if retired && bytes.Equal(data, retiredData) {
			// Reacquired the new coordinator. Never remove a newer operation's record.
			if err := os.Remove(pendingPath); err != nil {
				lock.Close()
				return nil, err
			}
			if err := syncDirectory(root); err != nil {
				lock.Close()
				return nil, err
			}
			return lock, nil
		}
		if len(data) != 0 {
			created, alive, err := uninstallProcessIdentity(record.PID)
			if err != nil {
				lock.Close()
				return nil, fmt.Errorf("verify pending cleanup owner: %w", err)
			}
			if alive && created == record.Created {
				lock.Close()
				return nil, fmt.Errorf("manager uninstall is pending at %s (operation %s, %s)", pendingPath, record.Operation, record.Phase)
			}
		}
		// Retiring the coordinator invalidates even old workers that know only its
		// file identity. The marker remains until we hold the new coordinator, so no
		// publisher can enter the gap and no delayed worker can delete a fresh install.
		next, err := lock.Rotate(context.Background(), lockPath)
		if err != nil {
			lock.Close()
			return nil, err
		}
		lock = next
		retired, retiredData = true, data
	}
	lock.Close()
	return nil, errors.New("manager cleanup changed repeatedly during recovery; retry installation")
}
