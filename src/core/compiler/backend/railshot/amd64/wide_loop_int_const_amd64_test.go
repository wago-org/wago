//go:build amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoderamd64 "github.com/wago-org/wago/src/core/encoder/amd64"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func appendWideI64ConstAMD64(body []byte, v int64, op byte) []byte {
	body = append(body, 0x42)
	body = append(body, wasmtest.SLEB64(v)...)
	return append(body, op)
}

func wideLoopIntConstModuleAMD64(t testing.TB) *wasm.Module {
	t.Helper()
	body := []byte{0x00, 0x02, 0x40, 0x03, 0x40} // no declarations; block; loop
	for _, c := range []int64{0x123456789abcdef, 0x23456789abcdef1, 0x3456789abcdef12} {
		for range 2 {
			body = append(body, 0x20, 0x00) // local.get i64 accumulator
			body = appendWideI64ConstAMD64(body, c, 0x7e)
			body = append(body, 0x21, 0x00) // local.set accumulator
		}
	}
	body = append(body,
		0x20, 0x01, // local.get i32 iterations
		0x41, 0x01, 0x6b, // i32.const 1; i32.sub
		0x22, 0x01, // local.tee iterations
		0x0d, 0x00, // br_if loop
		0x0b,       // end loop
		0x0b,       // end block
		0x20, 0x00, // local.get accumulator
		0x0b,
	)
	return mod1(t, []wasm.ValType{wasm.I64, wasm.I32}, []wasm.ValType{wasm.I64}, body)
}

func wideLoopIntConstInterruptModuleAMD64(t testing.TB) *wasm.Module {
	t.Helper()
	body := []byte{0x00, 0x02, 0x40, 0x03, 0x40}
	for range 2 {
		body = append(body, 0x20, 0x00)
		body = appendWideI64ConstAMD64(body, 0x123456789abcdef, 0x7e)
		body = append(body, 0x20, 0x02, 0x7c)
		body = appendWideI64ConstAMD64(body, 0x23456789abcdef1, 0x7e)
		body = append(body, 0x21, 0x00)
	}
	body = append(body,
		0x20, 0x01,
		0x41, 0x01, 0x6b,
		0x22, 0x01,
		0x0d, 0x00,
		0x0b,
		0x0b,
		0x20, 0x00,
		0x0b,
	)
	return mod1(t, []wasm.ValType{wasm.I64, wasm.I32, wasm.I64}, []wasm.ValType{wasm.I64}, body)
}

func TestWideLoopIntConstUsesOnlyIdleRegistersAMD64(t *testing.T) {
	before := wideLoopIntConstEnabled
	wideLoopIntConstEnabled = true
	defer func() { wideLoopIntConstEnabled = before }()

	h := funcHintView{loopIntConsts: &loopIntConstHintEntry{bits: [2]int64{1, 2}, count: 2}}
	f := fn{
		a:               &encoderamd64.Asm{},
		reserved:        maskOf(R12, R13, R14, R15, R9, R10, R11),
		pinnedLocalMask: maskOf(RDI),
	}
	f.preloadLoopIntConsts(&h)
	if f.iconstN != 1 || f.iconsts[0].reg != RSI {
		t.Fatalf("cached constants = %#v, want one in RSI", f.iconsts[:f.iconstN])
	}

	called := fn{usesCalls: true}
	called.preloadLoopIntConsts(&h)
	if called.iconstN != 0 {
		t.Fatalf("call-making function cached %d constants", called.iconstN)
	}
}

func TestWideLoopIntConstCompileSwitchAMD64(t *testing.T) {
	m := wideLoopIntConstModuleAMD64(t)
	compile := func(on bool) (*encoderamd64.CompiledModule, *CodegenStats) {
		var stats ModuleStats
		cm, err := CompileModuleWith(m, CompileOptions{Stats: &stats, Optimizations: map[string]bool{"wide-loop-int-const": on}})
		if err != nil {
			t.Fatal(err)
		}
		return cm, stats.Funcs[0]
	}
	onModule, on := compile(true)
	offModule, off := compile(false)
	defer onModule.CodeImage.Close()
	defer offModule.CodeImage.Close()
	if got := on.Peephole["wide-loop-int-const"]; got != 2 {
		t.Fatalf("cached constants = %d, want 2 (all: %v)", got, on.Peephole)
	}
	if got := off.Peephole["wide-loop-int-const"]; got != 0 {
		t.Fatalf("disabled cache count = %d", got)
	}
	if on.CodeBytes >= off.CodeBytes {
		t.Fatalf("cached code = %d bytes, rollback = %d; want smaller", on.CodeBytes, off.CodeBytes)
	}
	for _, iterations := range []uint64{1, 2, 7, 31} {
		gotOn := runCompiledAmd64u(t, onModule, 3, iterations)
		gotOff := runCompiledAmd64u(t, offModule, 3, iterations)
		if gotOn != gotOff {
			t.Fatalf("iterations=%d: enabled=%#x disabled=%#x", iterations, gotOn, gotOff)
		}
	}
}

func TestWideLoopIntConstInterruptPollPreservesConstantsAMD64(t *testing.T) {
	m := wideLoopIntConstInterruptModuleAMD64(t)
	compile := func(on bool) (*encoderamd64.CompiledModule, *CodegenStats) {
		var stats ModuleStats
		cm, err := CompileModuleWith(m, CompileOptions{
			Stats:         &stats,
			Interruptible: true,
			Optimizations: map[string]bool{"wide-loop-int-const": on},
		})
		if err != nil {
			t.Fatal(err)
		}
		return cm, stats.Funcs[0]
	}
	onModule, on := compile(true)
	offModule, off := compile(false)
	defer onModule.CodeImage.Close()
	defer offModule.CodeImage.Close()
	if got := on.Peephole["wide-loop-int-const"]; got != 1 {
		t.Fatalf("interruptible cached constants = %d, want one without poll-clobbered RSI: %v", got, on.Peephole)
	}
	if got := off.Peephole["wide-loop-int-const"]; got != 0 {
		t.Fatalf("disabled cached constants = %d, want 0", got)
	}
	for _, iterations := range []uint64{1, 2, 7} {
		gotOn := runCompiledAmd64u(t, onModule, 3, iterations, 5)
		gotOff := runCompiledAmd64u(t, offModule, 3, iterations, 5)
		if gotOn != gotOff {
			t.Fatalf("iterations=%d: enabled=%#x disabled=%#x", iterations, gotOn, gotOff)
		}
	}
}

func TestWideLoopIntConstRejectsImm32AMD64(t *testing.T) {
	body := []byte{0x00, 0x03, 0x40, 0x20, 0x00}
	body = appendWideI64ConstAMD64(body, 0x12345678, 0x7c)
	body = append(body, 0x1a, 0x0b, 0x42, 0x00, 0x0b)
	m := mod1(t, []wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}, body)
	var stats ModuleStats
	cm, err := CompileModuleWith(m, CompileOptions{Stats: &stats})
	if err != nil {
		t.Fatal(err)
	}
	defer cm.CodeImage.Close()
	if got := stats.Funcs[0].Peephole["wide-loop-int-const"]; got != 0 {
		t.Fatalf("imm32 constant cache count = %d, want 0", got)
	}
}
