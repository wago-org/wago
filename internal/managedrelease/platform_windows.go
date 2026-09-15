//go:build windows

package managedrelease

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/wago-org/wago/internal/filelock"
)

const (
	windowsFilesystemRetryDelay   = 10 * time.Millisecond
	windowsFilesystemRetryTimeout = 5 * time.Second
)

// Files and selection use MOVEFILE_WRITE_THROUGH; Go does not expose a portable
// directory fsync on Windows.
func syncDirectory(string) error { return nil }
func dispatch(path string, args, env []string) error {
	c := exec.Command(path, args[1:]...)
	c.Env = env
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

func openForSync(path string) (*os.File, error) { return os.OpenFile(path, os.O_RDWR, 0) }

// The Windows dispatcher waits for the child and holds its lease until return.
func inheritLease(*filelock.Lock) error { return nil }

// A racing nonblocking lease opener closes its child handle after observing
// retirement. Bound the wait for that Windows directory-sharing restriction.
func renameRetiredDirectory(path, retired string) error {
	return retryWindowsFilesystemOperation(func() error { return os.Rename(path, retired) })
}

func removeRetiredDirectory(path string) error {
	return retryWindowsFilesystemOperation(func() error { return os.RemoveAll(path) })
}

func retryWindowsFilesystemOperation(operation func() error) error {
	deadline := time.Now().Add(windowsFilesystemRetryTimeout)
	for {
		err := operation()
		if err == nil || os.IsNotExist(err) {
			return nil
		}
		if !errors.Is(err, syscall.ERROR_ACCESS_DENIED) &&
			!errors.Is(err, syscall.Errno(32)) &&
			!errors.Is(err, syscall.Errno(33)) {
			return err
		}
		if !time.Now().Before(deadline) {
			return err
		}
		time.Sleep(windowsFilesystemRetryDelay)
	}
}

func leaseHandoff(*filelock.Lock) (string, error) { return "", nil }
func adoptProcessLease(string) (*filelock.Lock, bool, error) {
	// Windows keeps the launcher lease in its waiting parent, without inheritance.
	_ = os.Unsetenv(leaseDescriptorEnv)
	return nil, false, nil
}
