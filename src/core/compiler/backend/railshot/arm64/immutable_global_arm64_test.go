//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func TestImmutableIntGlobalFoldARM64(t *testing.T) {
	for _, tc := range []struct {
		name    string
		typ     wasm.ValType
		init    []byte
		want    uint64
		mutable byte
		folds   int
	}{
		{name: "i32", typ: wasm.I32, init: []byte{0x41, 0x7f, 0x0b}, want: uint64(uint32(^uint32(0))), folds: 1},
		{name: "i64", typ: wasm.I64, init: []byte{0x42, 0x2a, 0x0b}, want: 42, folds: 1},
		{name: "mutable-near-miss", typ: wasm.I32, init: []byte{0x41, 0x2a, 0x0b}, want: 42, mutable: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := immutableIntGlobalModuleARM64(t, tc.typ, tc.mutable, tc.init)
			var stats ModuleStats
			if _, err := CompileModuleWith(m, CompileOptions{Stats: &stats, Workers: 2}); err != nil {
				t.Fatalf("compile: %v", err)
			}
			for i, st := range stats.Funcs {
				if got := st.Peephole["immutable-global-fold"]; got != tc.folds {
					t.Fatalf("function %d immutable-global-fold = %d, want %d; peeps=%v", i, got, tc.folds, st.Peephole)
				}
			}
			// The native unit harness intentionally does not construct the runtime
			// global-cell directory. Folded cases must not touch it; the mutable
			// near miss is therefore compile-only here and is covered end-to-end by
			// the runtime globals integration tests.
			if tc.folds != 0 {
				if got := runArm64u(t, m); got != tc.want {
					t.Fatalf("result = %#x, want %#x", got, tc.want)
				}
			}
		})
	}
}

func immutableIntGlobalModuleARM64(t testing.TB, typ wasm.ValType, mutable byte, init []byte) *wasm.Module {
	t.Helper()
	body := []byte{0x00, 0x23, 0x00, 0x0b}
	entry := append(wasmtest.ULEB(uint32(len(body))), body...)
	global := wasmtest.GlobalEntry(typ, mutable != 0, init)
	b := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{typ}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(6, wasmtest.Vec(global)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(entry, entry)),
	)
	m, err := wasm.DecodeModule(b)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return m
}
