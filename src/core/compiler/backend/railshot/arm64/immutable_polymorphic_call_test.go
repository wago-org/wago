//go:build arm64

package arm64

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestIndirectCallLazyContextSelection(t *testing.T) {
	requireCompilerDiagnostics(t)
	i32 := []wasm.ValType{wasm.I32}
	raw := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, i32), wasmtest.FuncType(i32, i32))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{1}, []byte{1})),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0, 3})),
		wasmtest.Section(9, wasmtest.Vec([]byte{0, 0x41, 0, 0x0b, 2, 1, 2})),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 1, 0x20, 0, 0x11, 1, 0, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x41, 1, 0x6a, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x41, 3, 0x6a, 0x0b}),
		)),
	)
	for _, imported := range []bool{false, true} {
		for _, exported := range []bool{false, true} {
			for _, regABI := range []bool{false, true} {
				for _, immutable := range []bool{false, true} {
					for _, typed := range []bool{false, true} {
						t.Run(fmt.Sprintf("imported%t/exported%t/abi%t/immutable%t/typed%t", imported, exported, regABI, immutable, typed), func(t *testing.T) {
							m, err := wasm.DecodeModule(raw)
							if err != nil {
								t.Fatal(err)
							}
							if imported {
								m.Imports = append(m.Imports, wasm.Import{Module: "env", Name: "host", Type: wasm.NewFuncExternType(wasm.TypeIdx{Index: 1})})
								m.Elements[0].Kind.Funcs = []wasm.FuncIdx{0, 2}
							}
							if exported {
								m.Exports = append(m.Exports, wasm.Export{Name: "table", Index: wasm.ExternIdx{Kind: wasm.ExternTable, Index: 0}})
							}
							var stats ModuleStats
							c, err := CompileModuleWith(m, CompileOptions{Stats: &stats, Optimizations: map[string]bool{"inline": false, "reg-abi": regABI, "immutable-table": immutable, "immutable-table-type": typed}})
							if err != nil {
								t.Fatal(err)
							}
							c.CodeImage.Close()
							want := 1
							if got := stats.Funcs[0].Peephole["internal-call-indirect-context-elide"]; got != want {
								t.Fatalf("selected %d, want %d", got, want)
							}
							want = 0
							if !imported && !exported && immutable && typed {
								want = 1
							}
							if got := stats.Funcs[0].Peephole["immutable-local-indirect-descriptor-elide"]; got != want {
								t.Fatalf("descriptor elision %d, want %d", got, want)
							}
						})
					}
				}
			}
		}
	}

}
