//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import coreruntime "github.com/wago-org/wago/src/core/runtime"

// One process-lifetime exit identity keeps alternating instance tails flat.
var nativeTailExitState nativeTailExitCache

func nativeTailExitPointer() (uintptr, error) {
	return nativeTailExitState.get(func() ([]byte, error) { return tailRAExitCodeInvocation(true) }, coreruntime.NewCodeBuffer, (*coreruntime.CodeBuffer).Seal, (*coreruntime.CodeBuffer).Close)
}
