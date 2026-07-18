package arm32

import (
	"fmt"

	a32 "github.com/wago-org/wago/src/core/encoder/arm32"
)

const embeddedTrapMemoryOutOfBounds = 1

// CompileI64LoadThunk emits a complete-width explicit-bounds load. ABI:
// R0=context, R1=dynamic address; result R0/R1. Failure writes the trap cell.
func CompileI64LoadThunk(staticOffset uint32) ([]byte, error) {
	var a a32.Asm
	if !a.MovImm32(a32.R3, staticOffset) || !a.Add(a32.R3, a32.R1, a32.R3) || !a.Cmp(a32.R3, a32.R1) {
		panic("arm32: load address")
	}
	branches := []int{a.FarBcond(a32.CondCC)}
	a.Ldr(a32.R2, a32.R0, 4)
	a.MovImm32(a32.R12, 8)
	a.Cmp(a32.R2, a32.R12)
	branches = append(branches, a.FarBcond(a32.CondCC))
	a.Sub(a32.R2, a32.R2, a32.R12)
	a.Cmp(a32.R2, a32.R3)
	branches = append(branches, a.FarBcond(a32.CondCC))
	a.Ldr(a32.R12, a32.R0, 0)
	a.Add(a32.R12, a32.R12, a32.R3)
	a.Ldr(a32.R0, a32.R12, 0)
	a.Ldr(a32.R1, a32.R12, 4)
	a.Ret()
	a.Align4()
	trap := a.Len()
	a.Ldr(a32.R12, a32.R0, 8)
	a.MovImm32(a32.R2, embeddedTrapMemoryOutOfBounds)
	a.Str(a32.R2, a32.R12, 0)
	a.MovImm32(a32.R0, 0)
	a.MovImm32(a32.R1, 0)
	a.Ret()
	a.Align4()
	for _, at := range branches {
		if !a.PatchFarBranch(at, trap) {
			return nil, fmt.Errorf("arm32: memory trap branch out of range")
		}
	}
	return a.B, nil
}

// CompileI64StoreThunk uses R0=context, R1=address, R2=lo, R3=hi. It returns
// R0=0 on success or R0=1 after trapping. Both words are preflighted together.
func CompileI64StoreThunk(staticOffset uint32) ([]byte, error) {
	var a a32.Asm
	a.MovImm32(a32.R12, 16)
	a.Sub(a32.SP, a32.SP, a32.R12)
	a.Str(a32.R4, a32.SP, 0)
	a.Str(a32.R5, a32.SP, 4)
	a.Str(a32.LR, a32.SP, 8)
	a.MovImm32(a32.R4, staticOffset)
	a.Add(a32.R4, a32.R1, a32.R4)
	a.Cmp(a32.R4, a32.R1)
	branches := []int{a.FarBcond(a32.CondCC)}
	a.Ldr(a32.R5, a32.R0, 4)
	a.MovImm32(a32.R12, 8)
	a.Cmp(a32.R5, a32.R12)
	branches = append(branches, a.FarBcond(a32.CondCC))
	a.Sub(a32.R5, a32.R5, a32.R12)
	a.Cmp(a32.R5, a32.R4)
	branches = append(branches, a.FarBcond(a32.CondCC))
	a.Ldr(a32.R12, a32.R0, 0)
	a.Add(a32.R12, a32.R12, a32.R4)
	a.Str(a32.R2, a32.R12, 0)
	a.Str(a32.R3, a32.R12, 4)
	a.MovImm32(a32.R0, 0)
	done := a.Branch()
	trap := a.Len()
	a.Ldr(a32.R12, a32.R0, 8)
	a.MovImm32(a32.R5, embeddedTrapMemoryOutOfBounds)
	a.Str(a32.R5, a32.R12, 0)
	a.MovImm32(a32.R0, 1)
	finish := a.Len()
	a.Ldr(a32.R4, a32.SP, 0)
	a.Ldr(a32.R5, a32.SP, 4)
	a.Ldr(a32.LR, a32.SP, 8)
	a.MovImm32(a32.R12, 16)
	a.Add(a32.SP, a32.SP, a32.R12)
	a.Ret()
	a.Align4()
	if !a.PatchBranch(done, finish) {
		return nil, fmt.Errorf("arm32: memory done branch")
	}
	for _, at := range branches {
		if !a.PatchFarBranch(at, trap) {
			return nil, fmt.Errorf("arm32: memory trap branch")
		}
	}
	return a.B, nil
}
