package shared

import (
	"testing"

	"github.com/wago-org/wago/internal/buildfeatures"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestInstructionNeedsInlineBoundary(t *testing.T) {
	for _, op := range []byte{0x00, 0x02, 0x03, 0x04, 0x05, 0x0c, 0x0d, 0x0e, 0x0f} {
		if !InstructionNeedsInlineBoundary(op, wasm.InstrInvalid) {
			t.Fatalf("opcode %#x does not require an inline boundary", op)
		}
	}
	for _, kind := range []wasm.InstrKind{
		wasm.InstrUnreachable, wasm.InstrBlock, wasm.InstrLoop, wasm.InstrIf,
		wasm.InstrBr, wasm.InstrBrIf, wasm.InstrBrTable, wasm.InstrReturn,
	} {
		if !InstructionNeedsInlineBoundary(0, kind) {
			t.Fatalf("instruction %v does not require an inline boundary", kind)
		}
	}
	if InstructionNeedsInlineBoundary(0x6a, wasm.InstrI32Add) {
		t.Fatal("i32.add unexpectedly requires an inline boundary")
	}
	for _, tc := range []struct {
		op      byte
		kind    wasm.InstrKind
		enabled bool
	}{
		{0x1f, wasm.InstrTryTable, buildfeatures.ExceptionHandling != 0},
		{0xd5, wasm.InstrBrOnNull, buildfeatures.TypedFunctionReferences != 0},
		{0xd6, wasm.InstrBrOnNonNull, buildfeatures.TypedFunctionReferences != 0},
		{0, wasm.InstrBrOnCast, buildfeatures.GC != 0},
		{0, wasm.InstrBrOnCastFail, buildfeatures.GC != 0},
	} {
		if tc.op != 0 && InstructionNeedsInlineBoundary(tc.op, wasm.InstrInvalid) != tc.enabled {
			t.Fatalf("unexpected inline boundary for opcode %#x", tc.op)
		}
		if InstructionNeedsInlineBoundary(0, tc.kind) != tc.enabled {
			t.Fatalf("unexpected inline boundary for %v", tc.kind)
		}
	}
}

func TestInstructionNeedsEHFrame(t *testing.T) {
	want := buildfeatures.ExceptionHandling != 0
	for _, op := range []byte{0x08, 0x0a, 0x1f} {
		if InstructionNeedsEHFrame(op, wasm.InstrInvalid) != want {
			t.Fatalf("unexpected EH frame requirement for opcode %#x", op)
		}
	}
	for _, kind := range []wasm.InstrKind{wasm.InstrThrow, wasm.InstrThrowRef, wasm.InstrTryTable} {
		if InstructionNeedsEHFrame(0, kind) != want {
			t.Fatalf("unexpected EH frame requirement for %v", kind)
		}
	}
}
