//go:build amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestIntegerSumInvariantGlobalSelection(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, mutable := range []bool{false, true} {
		for _, fenced := range []bool{false, true} {
			for _, enabled := range []bool{false, true} {
				for _, regABI := range []bool{false, true} {
					for _, hotGlobal := range []bool{false, true} {
						mut := byte(0)
						if mutable {
							mut = 1
						}
						body := []byte{1, 1, 0x7f}
						// A global competes with the three loop locals for finite GP
						// homes. Extra straight-line uses make it hotter than the bound;
						// reg ABI alone does not guarantee a value pin on AMD64.
						if hotGlobal {
							for j := 0; j < 3; j++ {
								body = append(body, 0x23, 0, 0x1a)
							}
						}
						body = append(body, 0x03, 0x40)
						if fenced {
							body = append(body, 0xfe, 3, 0)
						}
						body = append(body, 0x20, 1, 0x23, 0, 0x6a, 0x21, 1, 0x20, 2, 0x41, 1, 0x6a, 0x21, 2, 0x20, 2, 0x20, 0, 0x49, 0x0d, 0, 0x0b, 0x20, 1, 0x0b)
						m, err := wasm.DecodeModule(wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))), wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))), wasmtest.Section(6, wasmtest.Vec([]byte{0x7f, mut, 0x41, 41, 0x0b})), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...)))))
						if err != nil {
							t.Fatal(err)
						}
						var stats ModuleStats
						c, err := CompileModuleWith(m, CompileOptions{Stats: &stats, Optimizations: map[string]bool{"integer-sum-loop": enabled, "inline": false, "reg-abi": regABI}})
						if err != nil {
							t.Fatal(err)
						}
						c.CodeImage.Close()
						want := 0
						pinned := stats.Funcs[0].PinnedGlobalsValue != 0
						if mutable && regABI && !fenced && pinned != hotGlobal {
							t.Fatalf("hot=%t: pinned=%t", hotGlobal, pinned)
						}
						if enabled && !fenced && (!mutable || pinned) {
							want = 1
						}
						if got := stats.Funcs[0].Peephole["integer-sum-invariant-global"]; got != want {
							t.Fatalf("mutable=%t fenced=%t enabled=%t regABI=%t hot=%t got=%d want=%d", mutable, fenced, enabled, regABI, hotGlobal, got, want)
						}
					}
				}
			}
		}
	}
}
