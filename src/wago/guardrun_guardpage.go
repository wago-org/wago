//go:build wago_guardpage

package wago

import wruntime "github.com/wago-org/wago/src/core/runtime"

// newGuardedJobMemory installs the guard-page trap handler (idempotent) and
// returns a reservation-backed linear memory for signals-based bounds checks.
func newGuardedJobMemory(linBytes, maxBytes int) (*wruntime.JobMemory, error) {
	if err := wruntime.InstallGuardTrapHandler(); err != nil {
		return nil, err
	}
	return wruntime.NewJobMemoryGuarded(linBytes, maxBytes)
}
