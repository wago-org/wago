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

func TestScanBodyBytesCallHints(t *testing.T) {
	h, err := scanBodyBytes([]byte{0x10, 0x07, 0x0b}, 0, 0, 7) // call 7; end
	if err != nil {
		t.Fatalf("scan self call: %v", err)
	}
	if !h.hasCall || !h.callsSelf {
		t.Fatalf("self call hints = %+v, want hasCall and callsSelf", h)
	}

	h, err = scanBodyBytes([]byte{0x11, 0x00, 0x00, 0x0b}, 0, 0, 0) // call_indirect type 0 table 0; end
	if err != nil {
		t.Fatalf("scan call_indirect: %v", err)
	}
	if !h.hasCall || h.callsSelf {
		t.Fatalf("call_indirect hints = %+v, want hasCall without callsSelf", h)
	}
}

func TestScanBodyBytesMemoryHints(t *testing.T) {
	body := []byte{
		0x28, 0x02, 0x00, // i32.load align=2 offset=0
		0x36, 0x02, 0x00, // i32.store align=2 offset=0
		0x3f, 0x00, // memory.size 0
		0x40, 0x00, // memory.grow 0
		0x0b,
	}
	h, err := scanBodyBytes(body, 0, 0, 0)
	if err != nil {
		t.Fatalf("scan memory body: %v", err)
	}
	if !h.touchesMemory || h.usesBulkMem {
		t.Fatalf("memory hints = %+v, want touchesMemory only", h)
	}
}

func TestScanBodyBytesBulkMemoryHints(t *testing.T) {
	body := []byte{
		0xfc, 0x0a, 0x00, 0x00, // memory.copy dstmem=0 srcmem=0
		0xfc, 0x0b, 0x00, // memory.fill mem=0
		0x0b,
	}
	h, err := scanBodyBytes(body, 0, 0, 0)
	if err != nil {
		t.Fatalf("scan bulk memory body: %v", err)
	}
	if !h.touchesMemory || !h.usesBulkMem {
		t.Fatalf("bulk memory hints = %+v, want touchesMemory and usesBulkMem", h)
	}
}

func TestScanBodyBytesLoopWeightedScoresAndEligibility(t *testing.T) {
	body := []byte{
		0x03, 0x40, // loop void
		0x20, 0x00, // local.get 0
		0x21, 0x01, // local.set 1
		0x23, 0x01, // global.get 1
		0x24, 0x02, // global.set 2
		0x0b, // end loop
		0x0b, // end function
	}
	h, err := scanBodyBytes(body, 2, 3, 0)
	if err != nil {
		t.Fatalf("scan loop body: %v", err)
	}
	if h.localScore[0] != 10 || h.localScore[1] != 20 {
		t.Fatalf("local scores = %v, want [10 20]", h.localScore)
	}
	if h.globalScore[1] != 10 || h.globalScore[2] != 20 {
		t.Fatalf("global scores = %v, want g1=10 g2=20", h.globalScore)
	}
	if !h.globalElig[1] || !h.globalElig[2] {
		t.Fatalf("global eligibility = %v, want globals 1 and 2 eligible", h.globalElig)
	}
}

func TestScanBodyBytesLoopWithCallDisablesGlobalEligibility(t *testing.T) {
	body := []byte{
		0x03, 0x40, // loop void
		0x23, 0x00, // global.get 0
		0x10, 0x01, // call 1
		0x0b, // end loop
		0x0b, // end function
	}
	h, err := scanBodyBytes(body, 0, 1, 0)
	if err != nil {
		t.Fatalf("scan loop call body: %v", err)
	}
	if !h.hasCall {
		t.Fatalf("hints = %+v, want hasCall", h)
	}
	if h.globalScore[0] == 0 {
		t.Fatalf("global scores = %v, want global 0 scored", h.globalScore)
	}
	if h.globalElig[0] {
		t.Fatalf("global eligibility = %v, call-containing loop should not be eligible", h.globalElig)
	}
}

func TestScanFuncBodyUsesDecodedBodyBytes(t *testing.T) {
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
	if len(m.Code[0].Body.Instrs) != 0 || len(m.Code[0].BodyBytes) == 0 {
		t.Fatalf("decoded module should be byte-backed: instrs=%d bytes=%x", len(m.Code[0].Body.Instrs), m.Code[0].BodyBytes)
	}
	h, err := scanFuncBody(m.Code[0], 1, 3, 0)
	if err != nil {
		t.Fatalf("scan decoded body: %v", err)
	}
	if !h.hasCall || !h.callsSelf {
		t.Fatalf("decoded recursive body hints = %+v, want call+self-call", h)
	}
	if h.localScore[0] == 0 || h.globalScore[0] == 0 || h.globalScore[1] == 0 || h.globalScore[2] == 0 {
		t.Fatalf("decoded byte-backed body produced missing scores: locals=%v globals=%v", h.localScore, h.globalScore)
	}
	if !h.globalElig[1] {
		t.Fatalf("decoded loop without call should mark global 1 eligible: %v", h.globalElig)
	}
	if h.globalElig[2] {
		t.Fatalf("decoded loop with self call should not mark global 2 eligible: %v", h.globalElig)
	}
}

func TestScanBodyBytesMalformedImmediateReturnsError(t *testing.T) {
	if _, err := scanBodyBytes([]byte{0x10, 0x80, 0x0b}, 0, 0, 0); err == nil {
		t.Fatal("scan malformed call immediate succeeded, want error")
	}
}
