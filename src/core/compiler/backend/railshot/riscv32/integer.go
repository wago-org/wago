package riscv32

import (
	"fmt"

	rv "github.com/wago-org/wago/src/core/encoder/riscv32"
	"github.com/wago-org/wago/src/core/runtime/embedded32"
)

// CompileI32DivRemThunk emits trapping i32 division/remainder. ABI:
// A0=context, A1=left, A2=right; result A0. Divide-by-zero and signed division
// overflow write the canonical trap cell and return zero.
func CompileI32DivRemThunk(op embedded32.I32DivRemOp) ([]byte, error) {
	signed, remainder, ok := embedded32.I32DivRemInfo(op)
	if !ok {
		return nil, fmt.Errorf("riscv32: invalid i32 div/rem opcode %d", op)
	}
	var a rv.Asm
	a.MovReg(rv.T4, rv.A0)
	zeroBranch := a.FarBcond(rv.A2, rv.Zero, rv.CondEQ, rv.T6)
	overflowBranch := -1
	if signed && !remainder {
		a.MovImm32(rv.T0, 0x80000000)
		notMin := a.FarBcond(rv.A1, rv.T0, rv.CondNE, rv.T6)
		a.MovImm32(rv.T0, 0xffffffff)
		overflowBranch = a.FarBcond(rv.A2, rv.T0, rv.CondEQ, rv.T6)
		if !a.PatchFarBranch(notMin, a.Len()) {
			return nil, fmt.Errorf("riscv32: integer overflow skip out of range")
		}
	}
	if signed {
		if remainder {
			a.Rem(rv.A0, rv.A1, rv.A2)
		} else {
			a.Div(rv.A0, rv.A1, rv.A2)
		}
	} else if remainder {
		a.Remu(rv.A0, rv.A1, rv.A2)
	} else {
		a.Divu(rv.A0, rv.A1, rv.A2)
	}
	a.Ret()
	zeroTrap := a.Len()
	emitI32Trap(&a, rv.T4, embedded32.TrapIntegerDivideByZero)
	overflowTrap := a.Len()
	if overflowBranch >= 0 {
		emitI32Trap(&a, rv.T4, embedded32.TrapIntegerOverflow)
	}
	if !a.PatchFarBranch(zeroBranch, zeroTrap) {
		return nil, fmt.Errorf("riscv32: divide-by-zero branch out of range")
	}
	if overflowBranch >= 0 && !a.PatchFarBranch(overflowBranch, overflowTrap) {
		return nil, fmt.Errorf("riscv32: integer-overflow branch out of range")
	}
	return a.B, nil
}

func emitI32Trap(a *rv.Asm, context rv.Reg, trap embedded32.Trap) {
	a.Lw(rv.T0, context, embedded32.ContextTrapCellOffset)
	a.MovImm32(rv.T1, uint32(trap))
	a.Sw(rv.T1, rv.T0, 0)
	a.MovImm32(rv.A0, 0)
	a.Ret()
}
