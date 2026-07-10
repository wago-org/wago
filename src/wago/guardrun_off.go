//go:build !wago_guardpage || (!linux && !darwin) || (linux && !amd64 && !arm64) || (darwin && !arm64)

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

func callNative(_ *Compiled, eng *wruntime.Engine, jm *wruntime.JobMemory, refreshFence bool, entry uintptr, serArgs, trap, results []byte) error {
	// Refresh the stack fence for this engine: a shared (cross-instance) memory's
	// fence slot is overwritten by whichever instance last touched it, so each
	// entry re-establishes its own engine's foreign-stack bound.
	if refreshFence {
		jm.SetStackFence(eng.StackLimit())
	}
	if !refreshFence && preparedCallEnabled {
		return eng.CallPrepared(entry, serArgs, jm.LinMemBase(), trap, results)
	}
	return eng.Call(entry, serArgs, jm.LinearMemory(), trap, results)
}
