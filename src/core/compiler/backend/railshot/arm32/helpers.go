package arm32

import (
	"fmt"

	a32 "github.com/wago-org/wago/src/core/encoder/arm32"
	"github.com/wago-org/wago/src/core/runtime/embedded32"
)

// Helper thunk ABI:
//
//	R0 = pointer to the fixed helper frame
//	R1 = pointer to the fixed helper table
//
// The helper pointer stored by the board runtime must carry the Thumb state bit.
// The thunk tail-calls it so the caller's LR survives without a native frame.
func CompileF32HelperThunk(op embedded32.F32Op) ([]byte, error) {
	if !embedded32.F32HelperValid(op) {
		return nil, fmt.Errorf("arm32: invalid f32 helper opcode %d", op)
	}
	return compileHelperThunk(uint32(op), embedded32.HelperF32Offset), nil
}

func CompileF64HelperThunk(op embedded32.F64Op) ([]byte, error) {
	if !embedded32.F64HelperValid(op) {
		return nil, fmt.Errorf("arm32: invalid f64 helper opcode %d", op)
	}
	return compileHelperThunk(uint32(op), embedded32.HelperF64Offset), nil
}

func CompileI64HelperThunk(op embedded32.I64Op) ([]byte, error) {
	if !embedded32.I64HelperValid(op) {
		return nil, fmt.Errorf("arm32: invalid i64 helper opcode %d", op)
	}
	return compileHelperThunk(uint32(op), embedded32.HelperI64Offset), nil
}

func CompileSIMDHelperThunk(op uint32) ([]byte, error) {
	if !embedded32.SIMDHelperValid(op) {
		return nil, fmt.Errorf("arm32: invalid SIMD helper opcode %d", op)
	}
	return compileHelperThunk(op, embedded32.HelperSIMDOffset), nil
}

func compileHelperThunk(op uint32, tableOff uint16) []byte {
	var a a32.Asm
	if !a.MovImm32(a32.R2, op) || !a.Str(a32.R2, a32.R0, embedded32.F64FrameOpOffset) ||
		!a.Ldr(a32.R12, a32.R1, tableOff) || !a.Bx(a32.R12) {
		panic("arm32: helper thunk encoding failed")
	}
	a.Align4()
	return a.B
}
