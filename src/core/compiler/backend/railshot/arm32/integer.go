package arm32

import (
	"fmt"

	a32 "github.com/wago-org/wago/src/core/encoder/arm32"
	"github.com/wago-org/wago/src/core/runtime/embedded32"
)

// CompileI32DivRemThunk emits trapping i32 division/remainder. ABI:
// R0=context, R1=left, R2=right; result R0. Divide-by-zero and signed division
// overflow write the canonical trap cell and return zero.
func CompileI32DivRemThunk(op embedded32.I32DivRemOp) ([]byte, error) {
	signed, remainder, ok := embedded32.I32DivRemInfo(op)
	if !ok {
		return nil, fmt.Errorf("arm32: invalid i32 div/rem opcode %d", op)
	}
	var a a32.Asm
	mustMemory(a.MovReg(a32.R3, a32.R0), "integer context")
	mustMemory(a.MovImm32(a32.R12, 0), "integer zero")
	mustMemory(a.Cmp(a32.R2, a32.R12), "integer divisor zero")
	zeroBranch := a.FarBcond(a32.CondEQ)
	overflowBranch := -1
	if signed && !remainder {
		mustMemory(a.MovImm32(a32.R12, 0x80000000), "integer minimum")
		mustMemory(a.Cmp(a32.R1, a32.R12), "integer minimum compare")
		notMin := a.FarBcond(a32.CondNE)
		mustMemory(a.MovImm32(a32.R12, 0xffffffff), "integer minus one")
		mustMemory(a.Cmp(a32.R2, a32.R12), "integer overflow compare")
		overflowBranch = a.FarBcond(a32.CondEQ)
		if !a.PatchFarBranch(notMin, a.Len()) {
			return nil, fmt.Errorf("arm32: integer overflow skip out of range")
		}
	}
	if signed {
		mustMemory(a.Sdiv(a32.R0, a32.R1, a32.R2), "signed division")
	} else {
		mustMemory(a.Udiv(a32.R0, a32.R1, a32.R2), "unsigned division")
	}
	if remainder {
		mustMemory(a.Mul(a32.R0, a32.R0, a32.R2), "remainder product")
		mustMemory(a.Sub(a32.R0, a32.R1, a32.R0), "remainder subtract")
	}
	a.Ret()
	a.Align4()
	zeroTrap := a.Len()
	emitI32Trap(&a, a32.R3, embedded32.TrapIntegerDivideByZero)
	overflowTrap := a.Len()
	if overflowBranch >= 0 {
		emitI32Trap(&a, a32.R3, embedded32.TrapIntegerOverflow)
	}
	if !a.PatchFarBranch(zeroBranch, zeroTrap) {
		return nil, fmt.Errorf("arm32: divide-by-zero branch out of range")
	}
	if overflowBranch >= 0 && !a.PatchFarBranch(overflowBranch, overflowTrap) {
		return nil, fmt.Errorf("arm32: integer-overflow branch out of range")
	}
	return a.B, nil
}

func emitI32Trap(a *a32.Asm, context a32.Reg, trap embedded32.Trap) {
	mustMemory(a.Ldr(a32.R12, context, embedded32.ContextTrapCellOffset), "integer trap cell")
	mustMemory(a.MovImm32(a32.R0, uint32(trap)), "integer trap code")
	mustMemory(a.Str(a32.R0, a32.R12, 0), "integer trap write")
	mustMemory(a.MovImm32(a32.R0, 0), "integer trap result")
	a.Ret()
	a.Align4()
}
