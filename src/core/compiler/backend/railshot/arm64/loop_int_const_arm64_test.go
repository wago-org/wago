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
	old := shiftedImmediateHintFilterEnabled
	shiftedImmediateHintFilterEnabled = false // retain the legacy scoring control
	t.Cleanup(func() { shiftedImmediateHintFilterEnabled = old })
	body := []byte{0x03, 0x40}                    // loop
	body = appendI32Const(body, 0x12345678, 0x6c) // mul: no immediate form
	body = appendI32Const(body, 4095, 0x6a)       // fused mul-add: selected MADD needs an accumulator register
	body = appendI32Const(body, 4096, 0x6a)       // add: needs a register
	body = appendI32Const(body, 0x00ff00ff, 0x71) // and: logical immediate
	body = appendI32Const(body, 0x12345678, 0x71) // and: needs a register, same candidate
	body = append(body, 0x0b, 0x0b)
	h, err := scanBodyBytes(body, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if h.loopIntConstCount != 3 {
		t.Fatalf("loop integer constants = %d, want 3: %#v", h.loopIntConstCount, h.loopIntConst)
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
	if !got[0x12345678] || !got[4096] || !got[4095] || got[0x00ff00ff] {
		t.Fatalf("selected constants = %#v", got)
	}
}

func TestLoopIntConstHintsFusedMultiplyAddendsArm64(t *testing.T) {
	body := []byte{0x03, 0x40} // loop
	body = append(body,
		0x20, 0x00, 0x20, 0x01, 0x6c, 0x41, 0x01, 0x6a, // i32: x*y + 1
		0x20, 0x02, 0x20, 0x03, 0x7e, 0x42, 0x01, 0x7c, // i64: x*y + 1
		0x0b, 0x0b,
	)
	h, err := scanBodyBytes(body, 4, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if h.loopIntConstCount != 0 || h.hasLoopIntConsts() {
		t.Fatalf("fused-one hints spilled to sidecar: count=%d values=%v types=%x", h.loopIntConstCount, h.loopIntConst, h.loopIntConstTypes)
	}
	if got, want := h.loopIntOneHints(), loopI32OneHintMask|loopI64OneHintMask; got != want {
		t.Fatalf("inline fused-one hints = %#x, want %#x", got, want)
	}
	if got := h.immediateFreeOpCount(); got != 4 {
		t.Fatalf("immediate-free count = %d, want 4", got)
	}
	for _, tc := range []struct {
		previous, next byte
		typ            uint8
	}{
		{0x6c, 0x6b, 1}, // subtraction is not the MADD accumulator form.
		{0x7e, 0x6a, 1}, // mismatched multiply width.
		{0x6c, 0x7c, 2}, // mismatched multiply width.
	} {
		if loopIntConstNeedsMulAddRegister(tc.previous, tc.next, tc.typ) {
			t.Fatalf("mismatched pattern previous=%x next=%x type=%d admitted", tc.previous, tc.next, tc.typ)
		}
	}
}

func TestLoopIntConstFusedOneInlineLeaseArm64(t *testing.T) {
	h := funcHintView{funcHints: funcHints{immediateFreeOps: defaultStackArenaCap | loopI32OneHintMask | loopI64OneHintMask}}
	if got := h.immediateFreeOpCount(); got != defaultStackArenaCap {
		t.Fatalf("immediate-free count with inline flags = %d, want %d", got, defaultStackArenaCap)
	}
	f := fn{a: &a64.Asm{}, policy: currentCodegenPolicy()}
	f.preloadLoopIntConsts(&h)
	if f.iconstN != 2 || f.iconsts[0].typ != mtI32 || f.iconsts[0].bits != 1 || f.iconsts[1].typ != mtI64 || f.iconsts[1].bits != 1 {
		t.Fatalf("inline cached constants = %#v, want i32/i64 one", f.iconsts[:f.iconstN])
	}
}

func TestLoopIntConstFusedOneCompileArm64(t *testing.T) {
	requireCompilerDiagnostics(t)
	body := []byte{1, 1, 0x7f, 0x02, 0x40, 0x03, 0x40} // local accumulator; block; loop
	for range 2 {
		body = append(body,
			0x20, 0x03, // local.get accumulator
			0x20, 0x00, // local.get x
			0x20, 0x01, // local.get y
			0x6c,       // i32.mul
			0x41, 0x01, // i32.const 1
			0x6a,       // i32.add (MADD)
			0x6a,       // i32.add accumulator
			0x21, 0x03, // local.set accumulator
		)
	}
	body = append(body,
		0x20, 0x02, // local.get iterations
		0x41, 0x01, 0x6b, // i32.const 1; i32.sub
		0x22, 0x02, // local.tee iterations
		0x0d, 0x00, // br_if loop
		0x0b,       // end loop
		0x0b,       // end block
		0x20, 0x03, // local.get accumulator
		0x0b,
	)
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
	hints, sidecar, _, err := computeModuleHints(m, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(sidecar.loopIntConsts); got != 0 {
		t.Fatalf("fused-one sidecar entries = %d, want 0", got)
	}
	if got := hints[0].loopIntOneHints(); got != loopI32OneHintMask {
		t.Fatalf("module fused-one hint = %#x, want %#x", got, loopI32OneHintMask)
	}
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
	if got := on.Peephole["loop-int-const"]; got != 1 {
		t.Fatalf("cached constants = %d, want 1 (all: %v)", got, on.Peephole)
	}
	if got := on.Peephole["mul-add-cached-constant"]; got < 2 {
		t.Fatalf("cached MADD accumulators = %d, want at least 2 (all: %v)", got, on.Peephole)
	}
	if on.CodeBytes >= off.CodeBytes {
		t.Fatalf("cached code = %d bytes, rollback = %d; want smaller", on.CodeBytes, off.CodeBytes)
	}
	got, err := runArm64WrapperWithOptions(t, m, CompileOptions{}, 7, 9, 5)
	if err != nil || uint32(got) != 2*(7*9+1)*5 {
		t.Fatalf("result = %d, %v; want %d", uint32(got), err, 2*(7*9+1)*5)
	}
}

func TestLoopIntConstUsesOnlyIdleRegistersArm64(t *testing.T) {
	h := funcHintView{loopIntConstCount: 4, loopIntConstTypes: 0x55, loopIntConst: [4]int64{1, 2, 3, 4}}
	f := fn{a: &a64.Asm{}, reserved: maskOf(X25, X23), pinnedLocalMask: maskOf(X24), policy: currentCodegenPolicy()}
	f.preloadLoopIntConsts(&h)
	if f.iconstN != 3 || f.iconsts[0].reg != X27 || f.iconsts[1].reg != X14 || f.iconsts[2].reg != X13 {
		t.Fatalf("cached constants = %#v, want X27/X14/X13", f.iconsts[:f.iconstN])
	}

	called := fn{makesCalls: true, usesCalls: true, policy: currentCodegenPolicy()}
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
	requireCompilerDiagnostics(t)
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

func TestLoopIntConstScratchAdmissionArm64(t *testing.T) {
	h := funcHintView{loopIntConstCount: 4, loopIntConstTypes: 0x55, loopIntConst: [4]int64{1, 2, 3, 4}}
	for _, flags := range []funcHintFlags{hintUsesBulkMem, hintMutatesTable, hintModuleEH} {
		hints := h
		hints.flags = flags
		f := fn{a: &a64.Asm{}, reserved: maskOf(X25, X23), pinnedLocalMask: maskOf(X24), policy: currentCodegenPolicy()}
		f.preloadLoopIntConsts(&hints)
		if f.iconstN != 1 || f.iconsts[0].reg != X27 {
			t.Fatalf("fixed-register helper flags %x admitted scratch constants", flags)
		}
	}
	f := fn{a: &a64.Asm{}, reserved: maskOf(X25, X24, X23, X27), policy: currentCodegenPolicy()}
	for _, r := range gpAlloc {
		if r != X9 && r != X10 && r != X11 && r != X12 && r != X13 && r != X14 && r != X7 {
			f.reserved = f.reserved.add(r)
		}
	}
	f.preloadLoopIntConsts(&h)
	if f.iconstN != 0 {
		t.Fatalf("transient register floor violated: cached %d constants", f.iconstN)
	}
}
