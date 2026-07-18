//go:build !wago_guardpage || (!linux && !darwin) || (linux && !amd64 && !arm64 && !riscv64) || (darwin && !arm64)

package wago

import (
	"fmt"

	wruntime "github.com/wago-org/wago/src/core/runtime"
)

// newGuardedJobMemory is unavailable without the wago_guardpage build; the config
// layer rejects signals-based bounds checks before reaching here, so this only
// guards against a deserialized signals-based module.
func newGuardedJobMemory(int, int) (*wruntime.JobMemory, error) {
	return nil, fmt.Errorf("signals-based bounds checks require a binary built with -tags wago_guardpage")
}

func callNative(_ *Compiled, eng *wruntime.Engine, jm *wruntime.JobMemory, refreshControl bool, entry uintptr, serArgs, trap, results []byte) error {
	if err := refreshNativeControl(refreshControl, eng, jm, trap); err != nil {
		return err
	}
	if preparedCallEnabled {
		return eng.CallPrepared(entry, serArgs, jm.LinMemBase(), trap, results)
	}
	return eng.Call(entry, serArgs, jm.LinearMemory(), trap, results)
}
