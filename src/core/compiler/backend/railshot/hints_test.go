//go:build linux && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func TestScanBodyHints(t *testing.T) {
	callOnly := wasm.Expr{Instrs: []wasm.Instruction{
		{Kind: wasm.InstrLocalGet},
		{Kind: wasm.InstrCall, Index: 7},
	}}
	h := scanBody(callOnly, 1, 0, 7)
	if !h.hasCall || h.touchesMemory || !h.callsSelf {
		t.Fatalf("call-only body: hasCall=%v touchesMemory=%v callsSelf=%v", h.hasCall, h.touchesMemory, h.callsSelf)
	}
	if h.localScore[0] != 1 {
		t.Fatalf("local 0 score = %d, want 1", h.localScore[0])
	}
	if h2 := scanBody(callOnly, 1, 0, 8); h2.callsSelf {
		t.Fatal("call to 7 should not count as self for index 8")
	}

	callMemory := wasm.Expr{Instrs: []wasm.Instruction{
		{Kind: wasm.InstrLocalGet},
		{Kind: wasm.InstrI32Load},
		{Kind: wasm.InstrCall},
		{Kind: wasm.InstrMemoryFill},
	}}
	h = scanBody(callMemory, 1, 0, 99)
	if !h.hasCall || !h.touchesMemory || !h.usesBulkMem {
		t.Fatalf("call+memory body: %+v", h)
	}
}

func TestScanBodyDecodedModuleHasZeroScores(t *testing.T) {
	global := []byte{0x7f, 0x01, 0x41, 0x00, 0x0b} // (mut i32) = 0
	body := []byte{
		0x00,                                                 // no local decls
		0x03, 0x40, 0x20, 0x00, 0x1a, 0x23, 0x01, 0x1a, 0x0b, // loop1
		0x03, 0x40, 0x23, 0x02, 0x1a, 0x20, 0x00, 0x10, 0x00, 0x0b, // loop2 (self call)
		0x23, 0x00, 0x1a, // global.get 0; drop
		0x0b,
	}
	b := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(6, wasmtest.Vec(global, global, global)),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	m, err := wasm.DecodeModule(b)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	h := scanBody(m.Code[0].Body, 1, 3, 0)
	if h.hasCall || h.callsSelf || h.touchesMemory {
		t.Fatalf("decoded BodyBytes-only module should not produce instruction hints: %+v", h)
	}
	if h.localScore[0] != 0 || h.globalScore[0] != 0 || h.globalScore[1] != 0 || h.globalScore[2] != 0 {
		t.Fatalf("decoded BodyBytes-only module produced scores: locals=%v globals=%v", h.localScore, h.globalScore)
	}
	if h.globalElig[0] || h.globalElig[1] || h.globalElig[2] {
		t.Fatalf("decoded BodyBytes-only module produced global eligibility: %v", h.globalElig)
	}
}
