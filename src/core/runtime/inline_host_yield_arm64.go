//go:build (linux || darwin) && arm64 && !tinygo

package runtime

import goruntime "runtime"

//lint:ignore U1000 Used by inline_host_yield_arm64.h through go_asm.h.
const inlineHostYieldInterval = 1024

// The live assembly owner can loop through tiny Go callbacks without reaching
// a stack-growth check. Yield only after publishing the callback results, while
// the native continuation is parked and the owner's Go argument map is active.
//
//lint:ignore U1000 Called by ARM64 assembly through inline_host_yield_arm64.h.
func inlineHostYield() { goruntime.Gosched() }
