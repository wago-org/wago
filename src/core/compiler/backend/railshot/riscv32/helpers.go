package riscv32

import (
	"fmt"

	rv "github.com/wago-org/wago/src/core/encoder/riscv32"
	"github.com/wago-org/wago/src/core/runtime/embedded32"
)

// Helper thunk ABI:
//
//	A0 = pointer to the fixed helper frame
//	A1 = pointer to the fixed helper table
//
// The thunk writes its operation id, loads the target function, and tail-calls
// it so the caller's RA is preserved without a native stack frame.
func CompileF32HelperThunk(op embedded32.F32Op) ([]byte, error) {
	if !embedded32.F32HelperValid(op) {
		return nil, fmt.Errorf("riscv32: invalid f32 helper opcode %d", op)
	}
	return compileHelperThunk(uint32(op), embedded32.HelperF32Offset), nil
}

func CompileF64HelperThunk(op embedded32.F64Op) ([]byte, error) {
	if !embedded32.F64HelperValid(op) {
		return nil, fmt.Errorf("riscv32: invalid f64 helper opcode %d", op)
	}
	return compileHelperThunk(uint32(op), embedded32.HelperF64Offset), nil
}

func CompileI64HelperThunk(op embedded32.I64Op) ([]byte, error) {
	if !embedded32.I64HelperValid(op) {
		return nil, fmt.Errorf("riscv32: invalid i64 helper opcode %d", op)
	}
	return compileHelperThunk(uint32(op), embedded32.HelperI64Offset), nil
}

func CompileSIMDHelperThunk(op uint32) ([]byte, error) {
	if !embedded32.SIMDHelperValid(op) {
		return nil, fmt.Errorf("riscv32: invalid SIMD helper opcode %d", op)
	}
	return compileHelperThunk(op, embedded32.HelperSIMDOffset), nil
}

func compileHelperThunk(op uint32, tableOff int32) []byte {
	var a rv.Asm
	a.MovImm32(rv.A2, op)
	if !a.Sw(rv.A2, rv.A0, embedded32.F64FrameOpOffset) {
		panic("riscv32: helper op frame offset out of range")
	}
	if !a.Lw(rv.T6, rv.A1, tableOff) {
		panic("riscv32: helper table offset out of range")
	}
	a.Br(rv.T6)
	return a.B
}
