//go:build !windows

package profile

import (
	"os"
	"syscall"
)

func openInput(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
}
