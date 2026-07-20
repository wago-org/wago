package riscv32

import (
	"fmt"

	rv "github.com/wago-org/wago/src/core/encoder/riscv32"
	"github.com/wago-org/wago/src/core/runtime/embedded32"
)

// emitScalarLoad emits a naturally aligned fast path and a bytewise fallback.
// Base RV32 execution environments may trap misaligned lh/lw instructions, and
// Hazard3 always does, while WebAssembly permits every scalar memory access at
// every byte address. base, lo, hi, and scratch must be distinct registers.
func emitScalarLoad(a *rv.Asm, width uint32, base, lo, hi, scratch rv.Reg) error {
	if width == 1 {
		if !a.Lbu(lo, base, 0) {
			return fmt.Errorf("riscv32: encode byte load")
		}
		return nil
	}
	alignment := width
	if alignment > 4 {
		alignment = 4
	}
	if !a.Andi(scratch, base, int32(alignment-1)) {
		return fmt.Errorf("riscv32: encode scalar-load alignment check")
	}
	aligned := a.Bcond(scratch, rv.Zero, rv.CondEQ)

	emitWord := func(dst rv.Reg, offset int32) error {
		if !a.Lbu(dst, base, offset) {
			return fmt.Errorf("riscv32: encode unaligned load byte 0")
		}
		for byteIndex := int32(1); byteIndex < 4; byteIndex++ {
			if !a.Lbu(scratch, base, offset+byteIndex) ||
				!a.Slli(scratch, scratch, uint8(byteIndex*8)) {
				return fmt.Errorf("riscv32: encode unaligned load byte %d", byteIndex)
			}
			a.Or(dst, dst, scratch)
		}
		return nil
	}
	if width == 2 {
		if !a.Lbu(lo, base, 0) || !a.Lbu(scratch, base, 1) || !a.Slli(scratch, scratch, 8) {
			return fmt.Errorf("riscv32: encode unaligned halfword load")
		}
		a.Or(lo, lo, scratch)
	} else {
		if err := emitWord(lo, 0); err != nil {
			return err
		}
		if width == 8 {
			if err := emitWord(hi, 4); err != nil {
				return err
			}
		}
	}
	done := a.Jal(rv.Zero)

	alignedTarget := a.Len()
	switch width {
	case 2:
		if !a.Lhu(lo, base, 0) {
			return fmt.Errorf("riscv32: encode aligned halfword load")
		}
	case 4:
		if !a.Lw(lo, base, 0) {
			return fmt.Errorf("riscv32: encode aligned word load")
		}
	case 8:
		if !a.Lw(lo, base, 0) || !a.Lw(hi, base, 4) {
			return fmt.Errorf("riscv32: encode aligned doubleword load")
		}
	default:
		return fmt.Errorf("riscv32: invalid scalar load width %d", width)
	}
	finish := a.Len()
	if !a.PatchBranch13(aligned, alignedTarget) || !a.PatchJAL21(done, finish) {
		return fmt.Errorf("riscv32: scalar load alignment branch out of range")
	}
	return nil
}

// emitScalarStore is the store counterpart to emitScalarLoad. Bounds checks
// happen before this helper, so the bytewise fallback never partially commits
// an out-of-bounds WebAssembly store.
func emitScalarStore(a *rv.Asm, width uint32, base, lo, hi, scratch rv.Reg) error {
	if width == 1 {
		if !a.Sb(lo, base, 0) {
			return fmt.Errorf("riscv32: encode byte store")
		}
		return nil
	}
	alignment := width
	if alignment > 4 {
		alignment = 4
	}
	if !a.Andi(scratch, base, int32(alignment-1)) {
		return fmt.Errorf("riscv32: encode scalar-store alignment check")
	}
	aligned := a.Bcond(scratch, rv.Zero, rv.CondEQ)

	emitWord := func(src rv.Reg, offset int32) error {
		if !a.Sb(src, base, offset) {
			return fmt.Errorf("riscv32: encode unaligned store byte 0")
		}
		for byteIndex := int32(1); byteIndex < 4; byteIndex++ {
			if !a.Srli(scratch, src, uint8(byteIndex*8)) || !a.Sb(scratch, base, offset+byteIndex) {
				return fmt.Errorf("riscv32: encode unaligned store byte %d", byteIndex)
			}
		}
		return nil
	}
	if width == 2 {
		if !a.Sb(lo, base, 0) || !a.Srli(scratch, lo, 8) || !a.Sb(scratch, base, 1) {
			return fmt.Errorf("riscv32: encode unaligned halfword store")
		}
	} else {
		if err := emitWord(lo, 0); err != nil {
			return err
		}
		if width == 8 {
			if err := emitWord(hi, 4); err != nil {
				return err
			}
		}
	}
	done := a.Jal(rv.Zero)

	alignedTarget := a.Len()
	switch width {
	case 2:
		if !a.Sh(lo, base, 0) {
			return fmt.Errorf("riscv32: encode aligned halfword store")
		}
	case 4:
		if !a.Sw(lo, base, 0) {
			return fmt.Errorf("riscv32: encode aligned word store")
		}
	case 8:
		if !a.Sw(lo, base, 0) || !a.Sw(hi, base, 4) {
			return fmt.Errorf("riscv32: encode aligned doubleword store")
		}
	default:
		return fmt.Errorf("riscv32: invalid scalar store width %d", width)
	}
	finish := a.Len()
	if !a.PatchBranch13(aligned, alignedTarget) || !a.PatchJAL21(done, finish) {
		return fmt.Errorf("riscv32: scalar store alignment branch out of range")
	}
	return nil
}

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
	if err := emitScalarLoad(&a, width, rv.T0, rv.A0, rv.A1, rv.T1); err != nil {
		return nil, err
	}
	if signed {
		switch width {
		case 1:
			a.Slli(rv.A0, rv.A0, 24)
			a.Srai(rv.A0, rv.A0, 24)
		case 2:
			a.Slli(rv.A0, rv.A0, 16)
			a.Srai(rv.A0, rv.A0, 16)
		}
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
	if err := emitScalarStore(&a, width, rv.T0, rv.A2, rv.A3, rv.T1); err != nil {
		return nil, err
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
