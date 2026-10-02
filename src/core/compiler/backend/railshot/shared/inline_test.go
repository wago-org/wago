package shared

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestInstructionNeedsInlineBoundary(t *testing.T) {
	for _, op := range []byte{0x00, 0x02, 0x03, 0x04, 0x05, 0x0c, 0x0d, 0x0e, 0x0f, 0x1f, 0xd5, 0xd6} {
		if !InstructionNeedsInlineBoundary(op, wasm.InstrInvalid) {
			t.Fatalf("opcode %#x does not require an inline boundary", op)
		}
	}
	for _, kind := range []wasm.InstrKind{
		wasm.InstrUnreachable, wasm.InstrBlock, wasm.InstrLoop, wasm.InstrIf,
		wasm.InstrBr, wasm.InstrBrIf, wasm.InstrBrTable, wasm.InstrReturn,
		wasm.InstrTryTable, wasm.InstrBrOnNull, wasm.InstrBrOnNonNull,
		wasm.InstrBrOnCast, wasm.InstrBrOnCastFail,
	} {
		if !InstructionNeedsInlineBoundary(0, kind) {
			t.Fatalf("instruction %v does not require an inline boundary", kind)
		}
	}
	if InstructionNeedsInlineBoundary(0x6a, wasm.InstrI32Add) {
		t.Fatal("i32.add unexpectedly requires an inline boundary")
	}
}

func TestInstructionNeedsEHFrame(t *testing.T) {
	for _, op := range []byte{0x08, 0x0a, 0x1f} {
		if !InstructionNeedsEHFrame(op, wasm.InstrInvalid) {
			t.Fatalf("opcode %#x does not require an EH frame", op)
		}
	}
	for _, kind := range []wasm.InstrKind{wasm.InstrThrow, wasm.InstrThrowRef, wasm.InstrTryTable} {
		if !InstructionNeedsEHFrame(0, kind) {
			t.Fatalf("instruction %v does not require an EH frame", kind)
		}
	}
}
