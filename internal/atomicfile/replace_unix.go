//go:build !windows

package atomicfile

import "os"

func replaceExisting(source, destination string) error {
	return os.Rename(source, destination)
}
