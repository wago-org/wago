//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func appendI32Const(body []byte, v int32, op byte) []byte {
	body = append(body, 0x41)
	body = append(body, wasmtest.SLEB32(v)...)
	return append(body, op)
}

func TestLoopIntConstHintsOnlyRegisterConsumersArm64(t *testing.T) {
	body := []byte{0x03, 0x40}                    // loop
	body = appendI32Const(body, 0x12345678, 0x6c) // mul: no immediate form
	body = appendI32Const(body, 4095, 0x6a)       // add: encodable immediate
	body = appendI32Const(body, 4096, 0x6a)       // add: needs a register
	body = appendI32Const(body, 0x00ff00ff, 0x71) // and: logical immediate
	body = appendI32Const(body, 0x12345678, 0x71) // and: needs a register, same candidate
	body = append(body, 0x0b, 0x0b)
	h, err := scanBodyBytes(body, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if h.loopIntConstCount != 2 {
		t.Fatalf("loop integer constants = %d, want 2: %#v", h.loopIntConstCount, h.loopIntConst)
	}
	if !h.hasLoopIntConsts() {
		t.Fatal("loop integer constant sidecar marker is absent")
	}
	got := map[int64]bool{}
	for i := 0; i < int(h.loopIntConstCount); i++ {
		got[h.loopIntConst[i]] = true
		if typ := h.loopIntConstTypes >> (2 * i) & 3; typ != 1 {
			t.Fatalf("constant %d type = %d, want i32", i, typ)
		}
	}
	if !got[0x12345678] || !got[4096] || got[4095] || got[0x00ff00ff] {
		t.Fatalf("selected constants = %#v", got)
	}
}

func TestLoopIntConstUsesOnlyIdleRegistersArm64(t *testing.T) {
	h := funcHintView{loopIntConstCount: 4, loopIntConstTypes: 0x55, loopIntConst: [4]int64{1, 2, 3, 4}}
	f := fn{a: &a64.Asm{}, reserved: maskOf(X25, X23), pinnedLocalMask: maskOf(X24)}
	f.preloadLoopIntConsts(&h)
	if f.iconstN != 1 || f.iconsts[0].reg != X27 {
		t.Fatalf("cached constants = %#v, want only X27", f.iconsts[:f.iconstN])
	}

	called := fn{usesCalls: true}
	called.preloadLoopIntConsts(&h)
	if called.iconstN != 0 {
		t.Fatalf("call-making function cached %d constants", called.iconstN)
	}
}

func TestLoopIntConstCandidateScoreSaturatesArm64(t *testing.T) {
	c := newLoopIntConstCandidate(7, loopIntConstScoreMask-1, 2)
	c.addScore(10)
	if got := c.score(); got != loopIntConstScoreMask {
		t.Fatalf("saturated score = %d, want %d", got, loopIntConstScoreMask)
	}
	if got := c.typ(); got != 2 {
		t.Fatalf("type after saturation = %d, want i64", got)
	}
}

func loopIntConstModuleArm64(t testing.TB) *wasm.Module {
	body := []byte{0x01, 0x01, 0x7f, 0x02, 0x40, 0x03, 0x40} // one declared local; block; loop
	for _, c := range []int32{0x1234567, 0x2345671, 0x3456712, 0x4567123} {
		for range 2 {
			body = append(body, 0x20, 0x00) // local.get accumulator
			body = appendI32Const(body, c, 0x6c)
			body = append(body, 0x21, 0x00) // local.set accumulator
		}
	}
	body = append(body,
		0x20, 0x01, // local.get iterations
		0x41, 0x01, 0x6b, // i32.const 1; i32.sub
		0x22, 0x01, // local.tee iterations
		0x0d, 0x00, // br_if loop
		0x0b,       // end loop
		0x0b,       // end block
		0x20, 0x00, // local.get accumulator
		0x0b,
	)
	return mod1(t, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
}

func TestLoopIntConstCompileSwitchArm64(t *testing.T) {
	m := loopIntConstModuleArm64(t)
	compile := func(on bool) *CodegenStats {
		var stats ModuleStats
		cm, err := CompileModuleWith(m, CompileOptions{Stats: &stats, Optimizations: map[string]bool{"loop-int-const": on}})
		if err != nil {
			t.Fatal(err)
		}
		if cm.CodeImage != nil {
			defer cm.CodeImage.Close()
		}
		return stats.Funcs[0]
	}
	on, off := compile(true), compile(false)
	if got := on.Peephole["loop-int-const"]; got != 4 {
		t.Fatalf("cached constants = %d, want 4 (all: %v)", got, on.Peephole)
	}
	if got := off.Peephole["loop-int-const"]; got != 0 {
		t.Fatalf("disabled cache count = %d", got)
	}
	if on.CodeBytes >= off.CodeBytes {
		t.Fatalf("cached code = %d bytes, rollback = %d; want smaller", on.CodeBytes, off.CodeBytes)
	}
}
