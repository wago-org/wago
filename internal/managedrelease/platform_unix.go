//go:build !windows

package managedrelease

import (
	"errors"
	"os"
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

// flock ownership follows the open descriptor across exec. Keeping it open also
// conservatively pins source for child processes that outlive their manager.
func inheritLease(lease *filelock.Lock) error {
	_, _, errno := syscall.Syscall(syscall.SYS_FCNTL, lease.Descriptor(), syscall.F_SETFD, 0)
	if errno != 0 {
		return errno
	}
	return nil
}

func renameRetiredDirectory(path, retired string) error { return os.Rename(path, retired) }
