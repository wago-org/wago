package riscv32

import (
	"fmt"

	rv "github.com/wago-org/wago/src/core/encoder/riscv32"
)

const embeddedTrapMemoryOutOfBounds = 1

// CompileI64LoadThunk emits an explicit-bounds little-endian load. ABI:
// A0=context, A1=dynamic address; result A0/A1. On failure it writes the trap
// cell and returns zero. The full eight-byte access is checked before loading.
func CompileI64LoadThunk(staticOffset uint32) ([]byte, error) {
	var a rv.Asm
	a.MovReg(rv.T4, rv.A0)
	a.MovImm32(rv.T2, staticOffset)
	a.Add(rv.T2, rv.A1, rv.T2)
	branches := []int{a.FarBcond(rv.T2, rv.A1, rv.CondLTU, rv.T6)}
	a.Lw(rv.T1, rv.T4, 4)
	a.MovImm32(rv.T3, 8)
	branches = append(branches, a.FarBcond(rv.T1, rv.T3, rv.CondLTU, rv.T6))
	a.Sub(rv.T1, rv.T1, rv.T3)
	branches = append(branches, a.FarBcond(rv.T1, rv.T2, rv.CondLTU, rv.T6))
	a.Lw(rv.T0, rv.T4, 0)
	a.Add(rv.T0, rv.T0, rv.T2)
	a.Lw(rv.A0, rv.T0, 0)
	a.Lw(rv.A1, rv.T0, 4)
	a.Ret()
	trap := a.Len()
	a.Lw(rv.T0, rv.T4, 8)
	a.MovImm32(rv.T1, embeddedTrapMemoryOutOfBounds)
	a.Sw(rv.T1, rv.T0, 0)
	a.MovImm32(rv.A0, 0)
	a.MovImm32(rv.A1, 0)
	a.Ret()
	for _, at := range branches {
		if !a.PatchFarBranch(at, trap) {
			return nil, fmt.Errorf("riscv32: memory trap branch out of range")
		}
	}
	return a.B, nil
}

// CompileI64StoreThunk uses A0=context, A1=address, A2=lo, A3=hi and returns
// A0=0 on success or A0=1 after writing the trap cell. No partial store occurs.
func CompileI64StoreThunk(staticOffset uint32) ([]byte, error) {
	var a rv.Asm
	a.MovReg(rv.T4, rv.A0)
	a.MovImm32(rv.T2, staticOffset)
	a.Add(rv.T2, rv.A1, rv.T2)
	branches := []int{a.FarBcond(rv.T2, rv.A1, rv.CondLTU, rv.T6)}
	a.Lw(rv.T1, rv.T4, 4)
	a.MovImm32(rv.T3, 8)
	branches = append(branches, a.FarBcond(rv.T1, rv.T3, rv.CondLTU, rv.T6))
	a.Sub(rv.T1, rv.T1, rv.T3)
	branches = append(branches, a.FarBcond(rv.T1, rv.T2, rv.CondLTU, rv.T6))
	a.Lw(rv.T0, rv.T4, 0)
	a.Add(rv.T0, rv.T0, rv.T2)
	a.Sw(rv.A2, rv.T0, 0)
	a.Sw(rv.A3, rv.T0, 4)
	a.MovImm32(rv.A0, 0)
	a.Ret()
	trap := a.Len()
	a.Lw(rv.T0, rv.T4, 8)
	a.MovImm32(rv.T1, embeddedTrapMemoryOutOfBounds)
	a.Sw(rv.T1, rv.T0, 0)
	a.MovImm32(rv.A0, 1)
	a.Ret()
	for _, at := range branches {
		if !a.PatchFarBranch(at, trap) {
			return nil, fmt.Errorf("riscv32: memory trap branch out of range")
		}
	}
	return a.B, nil
}
