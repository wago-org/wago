//go:build amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestIntegerSumPureCallSelection(t *testing.T) {
	requireCompilerDiagnostics(t)
	helper := []byte{0x20, 0, 0x41, 3, 0x6c, 0x41, 1, 0x6a, 0x0b}
	caller := []byte{1, 1, 0x7f, 0x03, 0x40, 0x20, 1, 0x20, 2, 0x10, 0, 0x6a, 0x21, 1, 0x20, 2, 0x41, 1, 0x6a, 0x21, 2, 0x20, 2, 0x20, 0, 0x49, 0x0d, 0, 0x0b, 0x20, 1, 0x0b}
	m, err := wasm.DecodeModule(wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))), wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(helper), append(wasmtest.ULEB(uint32(len(caller))), caller...)))))
	if err != nil {
		t.Fatal(err)
	}
	for _, inline := range []bool{false, true} {
		for _, sum := range []bool{false, true} {
			var stats ModuleStats
			c, err := CompileModuleWith(m, CompileOptions{Stats: &stats, Optimizations: map[string]bool{"inline": inline, "integer-sum-loop": sum}})
			if err != nil {
				t.Fatal(err)
			}
			c.CodeImage.Close()
			want := 0
			if inline && sum {
				want = 1
			}
			if got := stats.Funcs[1].Peephole["integer-sum-pure-call"]; got != want {
				t.Fatalf("inline=%t sum=%t got=%d want=%d", inline, sum, got, want)
			}
		}
	}
}
