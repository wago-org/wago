//go:build !darwin && !linux && !windows

package sourcearchive

import (
	"fmt"
	"runtime"
)

func publishDirectoryNoReplace(_, _ string) error {
	return fmt.Errorf("atomic no-replace source publication is unsupported on %s", runtime.GOOS)
}
