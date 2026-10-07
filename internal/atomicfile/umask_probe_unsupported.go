//go:build !linux && !darwin && !windows

package atomicfile

import (
	"errors"
	"io/fs"
)

func probeUmaskMode(string, fs.FileMode, bool) (fs.FileMode, error) {
	return 0, errors.New("secure atomic umask probing is unsupported on this platform")
}
