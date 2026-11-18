//go:build amd64 && !tinygo && !wago_profile

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestStreamHintFunnelPredicate(t *testing.T) {
	old := streamingHintFunnelEnabled
	streamingHintFunnelEnabled = true
	t.Cleanup(func() { streamingHintFunnelEnabled = old })
	if streamHintsReject(nil) {
		t.Fatal("missing hints rejected")
	}
	for _, flag := range []funcHintFlags{hintHasFloatConst, hintHasSIMD, hintMutatesTable} {
		h := funcHintView{}
		h.flags.set(flag)
		if !streamHintsReject(&h) {
			t.Fatalf("missing negative %x", flag)
		}
	}
	for _, flag := range []funcHintFlags{0, hintHasTailCall, hintModuleEH, hintHasCall, hintUsesBulkMem, hintTouchesMemory, hintHasControlFlow} {
		h := funcHintView{}
		h.flags.set(flag)
		if streamHintsReject(&h) {
			t.Fatalf("unsafe negative %x", flag)
		}
	}
	h := funcHintView{}
	h.markNonDirectCall()
	if !streamHintsReject(&h) {
		t.Fatal("non-direct missed")
	}
	streamingHintFunnelEnabled = false
	if streamHintsReject(&h) {
		t.Fatal("disabled filter rejected")
	}
}

func TestStreamHintFunnelSyntacticFeatures(t *testing.T) {
	old := streamingHintFunnelEnabled
	streamingHintFunnelEnabled = true
	t.Cleanup(func() { streamingHintFunnelEnabled = old })
	simd := append([]byte{0xfd, 12}, make([]byte, 16)...)
	for _, op := range [][]byte{{0x43, 0, 0, 0, 0}, {0x44, 0, 0, 0, 0, 0, 0, 0, 0}, simd, {0x26, 0}, {0x11, 0, 0}} {
		for _, dead := range []bool{false, true} {
			body := []byte{}
			if dead {
				body = append(body, 0)
			}
			body = append(body, op...)
			body = append(body, 0x0b)
			h, err := scanBodyBytes(body, 0, 0, 0)
			if err != nil {
				t.Fatal(err)
			}
			if !streamHintsReject(&h) {
				t.Fatalf("missed op=%x dead=%v", op, dead)
			}
			if admitStreamBody(&wasm.Func{BodyBytes: body}, &wasm.CompType{}).Eligible {
				t.Fatal("negative admitted")
			}
		}
	}
	for _, op := range [][]byte{{0x10, 0}, {0xfc, 10, 0, 0}, {0xfc, 11, 0}} {
		h, err := scanBodyBytes(append(op, 0x0b), 0, 0, 1)
		if err != nil {
			t.Fatal(err)
		}
		if streamHintsReject(&h) {
			t.Fatalf("supported call/bulk rejected %x", op)
		}
	}
	for _, kind := range []wasm.InstrKind{wasm.InstrF32Const, wasm.InstrV128Const, wasm.InstrTableSet, wasm.InstrCallIndirect} {
		h := scanBody(wasm.Expr{Instrs: []wasm.Instruction{{Kind: kind}}}, 0, 0, 0)
		if !streamHintsReject(&h) {
			t.Fatalf("AST negative missed %v", kind)
		}
	}
}
