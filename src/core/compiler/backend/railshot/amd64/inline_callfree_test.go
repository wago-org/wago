//go:build linux && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

// inlineCallFreeModule: f(x) = add1(add1(x)) — its only calls are to the
// call-free leaf add1, so after inlining both, f makes no native call and is
// planned as call-free (the `all-calls-inlined` hint). This asserts the
// compile-time hint; end-to-end execution is covered cross-arch at the wago level
// (src/wago/inline_callfree_test.go).
func inlineCallFreeModule(t *testing.T) *wasm.Module {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{i32}, []wasm.ValType{i32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x10, 0x01, 0x10, 0x01, 0x0b}), // f: x; call add1; call add1
			wasmtest.Code([]byte{0x20, 0x00, 0x41, 0x01, 0x6a, 0x0b}),       // add1: x+1
		)),
	)
	m, err := wasm.DecodeModule(mod)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return m
}

func TestInlineCallFreeFires(t *testing.T) {
	s := compileWithStats(t, inlineCallFreeModule(t), false).Funcs[0]
	if s.Peephole["all-calls-inlined"] == 0 {
		t.Fatalf("all-calls-inlined = 0, want >=1 (all: %v)", s.Peephole)
	}
	// The hint disappears when disabled — proving it is what fired, and gated.
	saved := inlineCallFreeHintsEnabled
	inlineCallFreeHintsEnabled = false
	defer func() { inlineCallFreeHintsEnabled = saved }()
	if s := compileWithStats(t, inlineCallFreeModule(t), false).Funcs[0]; s.Peephole["all-calls-inlined"] != 0 {
		t.Fatalf("all-calls-inlined still fired with the hint disabled: %v", s.Peephole)
	}
}
