package arm32

import (
	"fmt"

	a32 "github.com/wago-org/wago/src/core/encoder/arm32"
	"github.com/wago-org/wago/src/core/runtime/embedded32"
)

// CompileScalarLoadThunk emits a complete-width explicit-bounds scalar load.
// ABI: R0=context, R1=dynamic address; results use R0 for i32/f32 and R0/R1
// (lo/hi) for i64/f64. Failure writes TrapMemoryOutOfBounds and returns zero.
func CompileScalarLoadThunk(op embedded32.ScalarLoadOp, staticOffset uint32) ([]byte, error) {
	width, resultWords, signed, ok := embedded32.ScalarLoadInfo(op)
	if !ok {
		return nil, fmt.Errorf("arm32: invalid scalar load opcode %d", op)
	}
	var a a32.Asm
	mustMemory(a.MovImm32(a32.R3, staticOffset), "load offset")
	mustMemory(a.Add(a32.R3, a32.R1, a32.R3), "load address")
	mustMemory(a.Cmp(a32.R3, a32.R1), "load overflow compare")
	branches := []int{a.FarBcond(a32.CondCC)}
	mustMemory(a.Ldr(a32.R2, a32.R0, embedded32.ContextLinearMemoryLengthOffset), "load memory length")
	mustMemory(a.MovImm32(a32.R12, width), "load width")
	mustMemory(a.Cmp(a32.R2, a32.R12), "load short-memory compare")
	branches = append(branches, a.FarBcond(a32.CondCC))
	mustMemory(a.Sub(a32.R2, a32.R2, a32.R12), "load bound")
	mustMemory(a.Cmp(a32.R2, a32.R3), "load bounds compare")
	branches = append(branches, a.FarBcond(a32.CondCC))
	mustMemory(a.Ldr(a32.R12, a32.R0, embedded32.ContextLinearMemoryBaseOffset), "load memory base")
	mustMemory(a.Add(a32.R12, a32.R12, a32.R3), "load effective pointer")
	switch width {
	case 1:
		if signed {
			mustMemory(a.Ldrsb(a32.R0, a32.R12, 0), "load8 signed")
		} else {
			mustMemory(a.Ldrb(a32.R0, a32.R12, 0), "load8 unsigned")
		}
	case 2:
		if signed {
			mustMemory(a.Ldrsh(a32.R0, a32.R12, 0), "load16 signed")
		} else {
			mustMemory(a.Ldrh(a32.R0, a32.R12, 0), "load16 unsigned")
		}
	case 4:
		mustMemory(a.Ldr(a32.R0, a32.R12, 0), "load32")
	case 8:
		mustMemory(a.Ldr(a32.R0, a32.R12, 0), "load64 low")
		mustMemory(a.Ldr(a32.R1, a32.R12, 4), "load64 high")
	default:
		panic("arm32: invalid scalar load width")
	}
	if resultWords == 2 && width < 8 {
		if signed {
			mustMemory(a.AsrImm(a32.R1, a32.R0, 31), "load sign high")
		} else {
			mustMemory(a.MovImm32(a32.R1, 0), "load zero high")
		}
	}
	a.Ret()
	a.Align4()
	trap := a.Len()
	mustMemory(a.Ldr(a32.R12, a32.R0, embedded32.ContextTrapCellOffset), "load trap cell")
	mustMemory(a.MovImm32(a32.R2, uint32(embedded32.TrapMemoryOutOfBounds)), "load trap code")
	mustMemory(a.Str(a32.R2, a32.R12, 0), "load trap write")
	mustMemory(a.MovImm32(a32.R0, 0), "load trap low result")
	if resultWords == 2 {
		mustMemory(a.MovImm32(a32.R1, 0), "load trap high result")
	}
	a.Ret()
	a.Align4()
	for _, at := range branches {
		if !a.PatchFarBranch(at, trap) {
			return nil, fmt.Errorf("arm32: memory trap branch out of range")
		}
	}
	return a.B, nil
}

// CompileScalarStoreThunk uses R0=context, R1=address, R2=lo, R3=hi and
// returns R0=0 on success or R0=1 after trapping. Every access, including an
// eight-byte split store, is preflighted completely before any byte is written.
func CompileScalarStoreThunk(op embedded32.ScalarStoreOp, staticOffset uint32) ([]byte, error) {
	width, _, ok := embedded32.ScalarStoreInfo(op)
	if !ok {
		return nil, fmt.Errorf("arm32: invalid scalar store opcode %d", op)
	}
	var a a32.Asm
	mustMemory(a.MovImm32(a32.R12, 16), "store frame size")
	mustMemory(a.Sub(a32.SP, a32.SP, a32.R12), "store frame allocate")
	mustMemory(a.Str(a32.R4, a32.SP, 0), "store save r4")
	mustMemory(a.Str(a32.R5, a32.SP, 4), "store save r5")
	mustMemory(a.Str(a32.LR, a32.SP, 8), "store save lr")
	mustMemory(a.MovImm32(a32.R4, staticOffset), "store offset")
	mustMemory(a.Add(a32.R4, a32.R1, a32.R4), "store address")
	mustMemory(a.Cmp(a32.R4, a32.R1), "store overflow compare")
	branches := []int{a.FarBcond(a32.CondCC)}
	mustMemory(a.Ldr(a32.R5, a32.R0, embedded32.ContextLinearMemoryLengthOffset), "store memory length")
	mustMemory(a.MovImm32(a32.R12, width), "store width")
	mustMemory(a.Cmp(a32.R5, a32.R12), "store short-memory compare")
	branches = append(branches, a.FarBcond(a32.CondCC))
	mustMemory(a.Sub(a32.R5, a32.R5, a32.R12), "store bound")
	mustMemory(a.Cmp(a32.R5, a32.R4), "store bounds compare")
	branches = append(branches, a.FarBcond(a32.CondCC))
	mustMemory(a.Ldr(a32.R12, a32.R0, embedded32.ContextLinearMemoryBaseOffset), "store memory base")
	mustMemory(a.Add(a32.R12, a32.R12, a32.R4), "store effective pointer")
	switch width {
	case 1:
		mustMemory(a.Strb(a32.R2, a32.R12, 0), "store8")
	case 2:
		mustMemory(a.Strh(a32.R2, a32.R12, 0), "store16")
	case 4:
		mustMemory(a.Str(a32.R2, a32.R12, 0), "store32")
	case 8:
		mustMemory(a.Str(a32.R2, a32.R12, 0), "store64 low")
		mustMemory(a.Str(a32.R3, a32.R12, 4), "store64 high")
	default:
		panic("arm32: invalid scalar store width")
	}
	mustMemory(a.MovImm32(a32.R0, 0), "store success")
	done := a.Branch()
	trap := a.Len()
	mustMemory(a.Ldr(a32.R12, a32.R0, embedded32.ContextTrapCellOffset), "store trap cell")
	mustMemory(a.MovImm32(a32.R5, uint32(embedded32.TrapMemoryOutOfBounds)), "store trap code")
	mustMemory(a.Str(a32.R5, a32.R12, 0), "store trap write")
	mustMemory(a.MovImm32(a32.R0, 1), "store failure")
	finish := a.Len()
	mustMemory(a.Ldr(a32.R4, a32.SP, 0), "store restore r4")
	mustMemory(a.Ldr(a32.R5, a32.SP, 4), "store restore r5")
	mustMemory(a.Ldr(a32.LR, a32.SP, 8), "store restore lr")
	mustMemory(a.MovImm32(a32.R12, 16), "store frame release size")
	mustMemory(a.Add(a32.SP, a32.SP, a32.R12), "store frame release")
	a.Ret()
	a.Align4()
	if !a.PatchBranch(done, finish) {
		return nil, fmt.Errorf("arm32: memory done branch out of range")
	}
	for _, at := range branches {
		if !a.PatchFarBranch(at, trap) {
			return nil, fmt.Errorf("arm32: memory trap branch out of range")
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

func mustMemory(ok bool, op string) {
	if !ok {
		panic("arm32: cannot encode " + op)
	}
}
