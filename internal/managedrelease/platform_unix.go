//go:build !windows

package managedrelease

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/wago-org/wago/internal/filelock"
)

func syncDirectory(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(f.Sync(), f.Close())
}
func dispatch(path string, args, env []string) error { return syscall.Exec(path, args, env) }

func openForSync(path string) (*os.File, error) { return os.Open(path) }

// flock ownership follows the open descriptor across one launcher-to-payload
// exec. The payload restores close-on-exec before ordinary startup.
func inheritLease(lease *filelock.Lock) error {
	_, _, errno := syscall.Syscall(syscall.SYS_FCNTL, lease.Descriptor(), syscall.F_SETFD, 0)
	if errno != 0 {
		return errno
	}
	return nil
}

func renameRetiredDirectory(path, retired string) error { return os.Rename(path, retired) }

func leaseHandoff(lease *filelock.Lock) (string, error) {
	if lease == nil {
		return "", nil
	}
	if err := inheritLease(lease); err != nil {
		return "", err
	}
	return strconv.FormatUint(uint64(lease.Descriptor()), 10), nil
}

func adoptProcessLease(executable string) (*filelock.Lock, bool, error) {
	value, found := os.LookupEnv(leaseDescriptorEnv)
	if !found {
		return nil, false, nil
	}
	if err := os.Unsetenv(leaseDescriptorEnv); err != nil {
		return nil, true, err
	}
	fd, err := strconv.ParseUint(value, 10, strconv.IntSize)
	if err != nil || fd < 3 {
		return nil, true, fmt.Errorf("invalid inherited release lease descriptor")
	}
	// Restore close-on-exec before any user or plugin subprocess can start.
	_, _, errno := syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), syscall.F_SETFD, syscall.FD_CLOEXEC)
	if errno != 0 {
		return nil, true, errno
	}
	path := filepath.Join(filepath.Dir(executable), leaseFile)
	file := os.NewFile(uintptr(fd), path)
	lease, err := filelock.AdoptSharedExisting(file, path)
	return lease, true, err
}
