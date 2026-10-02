//go:build darwin

package build

import (
	"fmt"
	"os"
	"syscall"
)

func validateBuildOutputPlatformMetadata(path string, info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("inspect output file flags %s: unexpected file info %T", path, info.Sys())
	}
	if stat.Flags != 0 {
		// Darwin file flags belong to the inode. Atomic replacement would silently
		// discard flags that an in-place write preserved, while restoring immutable
		// or restricted flags may need privileges the old content write did not.
		return fmt.Errorf("output %s has persistent Darwin file flags %#x", path, stat.Flags)
	}
	return nil
}
