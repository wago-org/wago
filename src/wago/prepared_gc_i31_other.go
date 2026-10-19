//go:build (!linux && !darwin && !windows) || (!amd64 && !arm64) || tinygo

package wago

import rt "github.com/wago-org/wago/src/core/runtime"

const gcI31InductionSupported = false

//go:noinline
func clearPreparedInductionTrap(in *Instance) error {
	rt.PreparePreparedIntTrap(in.trap)
	if rt.PreparedIntTrapCode(in.trap) != rt.TrapNone {
		return rt.ConsumePreparedIntTrap(in.trap)
	}
	return nil
}
