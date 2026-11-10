//go:build linux && arm64 && !tinygo

package runtime

import a64 "github.com/wago-org/wago/src/core/encoder/arm64"

const loopSentinel = 0x5A5A5A5A

var stubLoop = arm64LoopStub(false)
var stubLoopHeartbeat = arm64LoopStub(true)

func arm64LoopStub(heartbeat bool) []byte {
	var a a64.Asm
	mustArm64Stress(a.Load32(a64.X4, a64.X0, 0)) // iterations

	loop := a.Len()
	if heartbeat {
		mustArm64Stress(a.Store32(a64.X4, a64.X1, 0)) // linMem[0] = counter
	}
	a.CmpImm32(a64.X4, 0)
	jdone := a.Bcond(a64.CondEQ)
	a.SubImm32(a64.X4, a64.X4, 1)
	jloop := a.Branch()

	done := a.Len()
	a.MovImm64(a64.X5, loopSentinel)
	mustArm64Stress(a.Store32(a64.X5, a64.X1, 0)) // linMem[0]
	mustArm64Stress(a.Store32(a64.X5, a64.X3, 0)) // results[0]
	mustArm64Stress(a.Store32(a64.ZR, a64.X2, 0)) // trap = none
	a.Ret()

	if !a.PatchBranch19(jdone, done) {
		panic("arm64 stress stub conditional branch out of range")
	}
	if !a.PatchBranch26(jloop, loop) {
		panic("arm64 stress stub loop branch out of range")
	}
	return a.B
}

func mustArm64Stress(ok bool) {
	if !ok {
		panic("arm64 stress stub encoding failed")
	}
}
