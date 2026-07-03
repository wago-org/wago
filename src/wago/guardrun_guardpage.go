//go:build wago_guardpage

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

func callNative(c *Compiled, eng *wruntime.Engine, jm *wruntime.JobMemory, entry uintptr, serArgs, trap, results []byte) error {
	// Refresh the stack fence for this engine (see the non-guardpage build).
	jm.SetStackFence(eng.StackLimit())
	if c.boundsMode == BoundsChecksSignalsBased {
		return eng.CallGuarded(entry, serArgs, jm.LinearMemory(), trap, results, jm)
	}
	return eng.Call(entry, serArgs, jm.LinearMemory(), trap, results)
}
