//go:build !windows

package regularfile

import (
	"os"
	"syscall"
)

func open(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
}

func transientSnapshotError(error) bool { return false }
