package riscv32

import (
	"fmt"

	rv "github.com/wago-org/wago/src/core/encoder/riscv32"
	"github.com/wago-org/wago/src/core/runtime/embedded32"
)

// CompileScalarLoadThunk emits an explicit-bounds little-endian scalar load.
// ABI: A0=context, A1=dynamic address; results use A0 for i32/f32 and A0/A1
// (lo/hi) for i64/f64. Failure writes TrapMemoryOutOfBounds and returns zero.
func CompileScalarLoadThunk(op embedded32.ScalarLoadOp, staticOffset uint32) ([]byte, error) {
	width, resultWords, signed, ok := embedded32.ScalarLoadInfo(op)
	if !ok {
		return nil, fmt.Errorf("riscv32: invalid scalar load opcode %d", op)
	}
	var a rv.Asm
	a.MovReg(rv.T4, rv.A0)
	a.MovImm32(rv.T2, staticOffset)
	a.Add(rv.T2, rv.A1, rv.T2)
	branches := []int{a.FarBcond(rv.T2, rv.A1, rv.CondLTU, rv.T6)}
	a.Lw(rv.T1, rv.T4, embedded32.ContextLinearMemoryLengthOffset)
	a.MovImm32(rv.T3, width)
	branches = append(branches, a.FarBcond(rv.T1, rv.T3, rv.CondLTU, rv.T6))
	a.Sub(rv.T1, rv.T1, rv.T3)
	branches = append(branches, a.FarBcond(rv.T1, rv.T2, rv.CondLTU, rv.T6))
	a.Lw(rv.T0, rv.T4, embedded32.ContextLinearMemoryBaseOffset)
	a.Add(rv.T0, rv.T0, rv.T2)
	switch width {
	case 1:
		if signed {
			a.Lb(rv.A0, rv.T0, 0)
		} else {
			a.Lbu(rv.A0, rv.T0, 0)
		}
	case 2:
		if signed {
			a.Lh(rv.A0, rv.T0, 0)
		} else {
			a.Lhu(rv.A0, rv.T0, 0)
		}
	case 4:
		a.Lw(rv.A0, rv.T0, 0)
	case 8:
		a.Lw(rv.A0, rv.T0, 0)
		a.Lw(rv.A1, rv.T0, 4)
	default:
		panic("riscv32: invalid scalar load width")
	}
	if resultWords == 2 && width < 8 {
		if signed {
			a.Srai(rv.A1, rv.A0, 31)
		} else {
			a.MovImm32(rv.A1, 0)
		}
	}
	a.Ret()
	trap := a.Len()
	a.Lw(rv.T0, rv.T4, embedded32.ContextTrapCellOffset)
	a.MovImm32(rv.T1, uint32(embedded32.TrapMemoryOutOfBounds))
	a.Sw(rv.T1, rv.T0, 0)
	a.MovImm32(rv.A0, 0)
	if resultWords == 2 {
		a.MovImm32(rv.A1, 0)
	}
	a.Ret()
	for _, at := range branches {
		if !a.PatchFarBranch(at, trap) {
			return nil, fmt.Errorf("riscv32: memory trap branch out of range")
		}
	}
	return a.B, nil
}

// CompileScalarStoreThunk emits an explicit-bounds scalar store. ABI:
// A0=context, A1=address, A2=lo, A3=hi. It returns A0=0 on success or A0=1
// after trapping. The complete access is checked before any split store.
func CompileScalarStoreThunk(op embedded32.ScalarStoreOp, staticOffset uint32) ([]byte, error) {
	width, _, ok := embedded32.ScalarStoreInfo(op)
	if !ok {
		return nil, fmt.Errorf("riscv32: invalid scalar store opcode %d", op)
	}
	var a rv.Asm
	a.MovReg(rv.T4, rv.A0)
	a.MovImm32(rv.T2, staticOffset)
	a.Add(rv.T2, rv.A1, rv.T2)
	branches := []int{a.FarBcond(rv.T2, rv.A1, rv.CondLTU, rv.T6)}
	a.Lw(rv.T1, rv.T4, embedded32.ContextLinearMemoryLengthOffset)
	a.MovImm32(rv.T3, width)
	branches = append(branches, a.FarBcond(rv.T1, rv.T3, rv.CondLTU, rv.T6))
	a.Sub(rv.T1, rv.T1, rv.T3)
	branches = append(branches, a.FarBcond(rv.T1, rv.T2, rv.CondLTU, rv.T6))
	a.Lw(rv.T0, rv.T4, embedded32.ContextLinearMemoryBaseOffset)
	a.Add(rv.T0, rv.T0, rv.T2)
	switch width {
	case 1:
		a.Sb(rv.A2, rv.T0, 0)
	case 2:
		a.Sh(rv.A2, rv.T0, 0)
	case 4:
		a.Sw(rv.A2, rv.T0, 0)
	case 8:
		a.Sw(rv.A2, rv.T0, 0)
		a.Sw(rv.A3, rv.T0, 4)
	default:
		panic("riscv32: invalid scalar store width")
	}
	a.MovImm32(rv.A0, 0)
	a.Ret()
	trap := a.Len()
	a.Lw(rv.T0, rv.T4, embedded32.ContextTrapCellOffset)
	a.MovImm32(rv.T1, uint32(embedded32.TrapMemoryOutOfBounds))
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

func CompileI64LoadThunk(staticOffset uint32) ([]byte, error) {
	return CompileScalarLoadThunk(embedded32.ScalarI64Load, staticOffset)
}

func CompileI64StoreThunk(staticOffset uint32) ([]byte, error) {
	return CompileScalarStoreThunk(embedded32.ScalarI64Store, staticOffset)
}
