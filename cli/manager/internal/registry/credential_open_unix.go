//go:build linux || darwin

package registry

import (
	"os"

	"golang.org/x/sys/unix"
)

func openCredentialFile(path string) (*os.File, error) {
	descriptor, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NONBLOCK|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(descriptor), path)
	if file == nil {
		_ = unix.Close(descriptor)
		return nil, os.ErrInvalid
	}
	return file, nil
}
