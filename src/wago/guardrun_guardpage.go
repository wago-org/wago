//go:build wago_guardpage && ((linux && (amd64 || arm64)) || (darwin && arm64))

package wago

import wruntime "github.com/wago-org/wago/src/core/runtime"

// newGuardedJobMemory installs the guard-page trap handler (idempotent) and
// returns a reservation-backed linear memory for signals-based bounds checks.
func newGuardedJobMemory(linBytes, maxBytes int) (*wruntime.JobMemory, error) {
	if err := wruntime.InstallGuardTrapHandler(); err != nil {
		return nil, err
	}
	return wruntime.AcquireJobMemoryGuarded(linBytes, maxBytes)
}

func callNative(c *Compiled, eng *wruntime.Engine, jm *wruntime.JobMemory, refreshFence bool, entry uintptr, serArgs, trap, results []byte) error {
	// Refresh the stack fence for this engine (see the non-guardpage build).
	if refreshFence {
		jm.SetStackFence(eng.StackLimit())
	}
	if !refreshFence && preparedCallEnabled {
		return eng.CallPrepared(entry, serArgs, jm.LinMemBase(), trap, results)
	}
	if c.boundsMode == BoundsChecksSignalsBased {
		return eng.CallGuarded(entry, serArgs, jm.LinearMemory(), trap, results, jm)
	}
	return eng.Call(entry, serArgs, jm.LinearMemory(), trap, results)
}
