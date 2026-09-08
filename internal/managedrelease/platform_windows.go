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
	var err error
	for attempt := 0; attempt < 32; attempt++ {
		err = os.Rename(path, retired)
		if err == nil || (!errors.Is(err, syscall.ERROR_ACCESS_DENIED) && !errors.Is(err, syscall.Errno(32))) {
			return err
		}
		if attempt != 31 {
			time.Sleep(10 * time.Millisecond)
		}
	}
	return err
}

func leaseHandoff(*filelock.Lock) (string, error) { return "", nil }
func adoptProcessLease(string) (*filelock.Lock, bool, error) {
	// Windows keeps the launcher lease in its waiting parent, without inheritance.
	_ = os.Unsetenv(leaseDescriptorEnv)
	return nil, false, nil
}
