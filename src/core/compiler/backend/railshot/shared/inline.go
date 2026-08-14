package shared

import "github.com/wago-org/wago/src/core/compiler/wasm"

// InstructionNeedsInlineBoundary reports whether an instruction consumes or
// creates a control label that must remain inside a synthetic callee frame when
// its body is spliced into a caller. The opcode covers byte-backed scanners;
// kind covers decoded AST instructions and prefixed instructions.
func InstructionNeedsInlineBoundary(op byte, kind wasm.InstrKind) bool {
	if kind == wasm.InstrInvalid {
		switch op {
		case 0x00, // unreachable
			0x02, // block
			0x03, // loop
			0x04, // if
			0x05, // else
			0x0c, // br
			0x0d, // br_if
			0x0e, // br_table
			0x0f, // return
			0x1f, // try_table
			0xd5, // br_on_null
			0xd6: // br_on_non_null
			return true
		}
	}
	switch kind {
	case wasm.InstrUnreachable, wasm.InstrBlock, wasm.InstrLoop, wasm.InstrIf,
		wasm.InstrBr, wasm.InstrBrIf, wasm.InstrBrTable, wasm.InstrReturn,
		wasm.InstrTryTable, wasm.InstrBrOnNull, wasm.InstrBrOnNonNull,
		wasm.InstrBrOnCast, wasm.InstrBrOnCastFail:
		return true
	default:
		return false
	}
}

// InstructionNeedsEHFrame reports whether an instruction requires the
// exception-handling frame plan that inline targets do not yet transfer.
func InstructionNeedsEHFrame(op byte, kind wasm.InstrKind) bool {
	if kind == wasm.InstrInvalid {
		switch op {
		case 0x08, 0x0a, 0x1f: // throw, throw_ref, try_table
			return true
		}
	}
	switch kind {
	case wasm.InstrThrow, wasm.InstrThrowRef, wasm.InstrTryTable:
		return true
	default:
		return false
	}
}
