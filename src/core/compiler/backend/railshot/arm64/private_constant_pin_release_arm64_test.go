//go:build (linux || darwin) && arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestPrivateConstantCarriersPreserveOldReads(t *testing.T) {
	old := privateConstantsEnabled
	t.Cleanup(func() { privateConstantsEnabled = old })
	for _, wideLocals := range []bool{false, true} {
		for _, oldRead := range []bool{false, true} {
			decls := byte(1)
			if wideLocals {
				decls = 81
			}
			body := []byte{1, decls, 0x7f}
			if oldRead {
				body = append(body, 0x20, 1)
			}
			body = append(body, 0x23, 0, 0x41, 7, 0x6b, 0x21, 1)
			if wideLocals {
				body = append(body, 0x20, 1, 0x1a)
				for range 140 {
					body = append(body, 0x01)
				}
			}
			body = append(body, 0x03, 0x7f, 0x20, 1, 0x41, 0, 0x0d, 0, 0x0b)
			if oldRead {
				body = append(body, 0x6a)
			}
			body = append(body, 0x0b)
			m := modMem(t, 1, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
			m.Globals = []wasm.Global{{Type: wasm.GlobalType{Type: wasm.I32, Mutable: true}, Init: wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrI32Const, I32: 17}}}}}
			for _, enabled := range []bool{false, true} {
				privateConstantsEnabled = enabled
				var stats ModuleStats
				opts := CompileOptions{Optimizations: map[string]bool{"wide-reuse-pins": false}}
				if diagnosticsEnabled {
					opts.Stats = &stats
				}
				got, err := runPrivateGlobalCells(t, m, opts, 0, 17)
				if err != nil || got != 10 {
					t.Fatalf("wideLocals=%v oldRead=%v enabled=%v got=%x err=%v", wideLocals, oldRead, enabled, got, err)
				}
				if diagnosticsEnabled && (stats.Funcs[0].Peephole["private-local-constant-fact"] > 0) != enabled {
					t.Fatalf("wideLocals=%v enabled=%v stats=%v", wideLocals, enabled, stats.Funcs[0].Peephole)
				}
			}
		}
	}
}

func TestPrivateConstantDefinedGlobalDoesNotFoldImportedCell(t *testing.T) {
	old := privateConstantsEnabled
	t.Cleanup(func() { privateConstantsEnabled = old })
	for _, wide := range []bool{false, true} {
		vt, typ, cst, add := wasm.I32, byte(0x7f), byte(0x41), byte(0x6a)
		if wide {
			vt, typ, cst, add = wasm.I64, 0x7e, 0x42, 0x7c
		}
		imp := append(wasmtest.Name("env"), wasmtest.Name("live")...)
		imp = append(imp, 3, typ, 1)
		body := []byte{0, 0x23, 0, 0x23, 1, add, 0x0b}
		data := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{vt}))), wasmtest.Section(2, wasmtest.Vec(imp)), wasmtest.Section(3, wasmtest.Vec([]byte{0})), wasmtest.Section(6, wasmtest.Vec([]byte{typ, 1, cst, 17, 0x0b})), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))))
		m, err := wasm.DecodeModule(data)
		if err != nil {
			t.Fatal(err)
		}
		for _, enabled := range []bool{false, true} {
			privateConstantsEnabled = enabled
			for _, cell := range []uint64{0, 1, 123, 0xdeadbeef00000001, ^uint64(0)} {
				want := cell + 17
				if !wide {
					want = uint64(uint32(want))
				}
				var stats ModuleStats
				opts := CompileOptions{}
				if diagnosticsEnabled {
					opts.Stats = &stats
				}
				got, err := runPrivateGlobalCells(t, m, opts, 0, cell, 17)
				if err != nil || got != want {
					t.Fatalf("wide=%v enabled=%v cell=%x got=%x want=%x err=%v", wide, enabled, cell, got, want, err)
				}
				if diagnosticsEnabled {
					hits := stats.Funcs[0].Peephole["private-global-constant"]
					wantHits := 0 // Imported and defined global reads both retain their carriers.
					if hits != wantHits {
						t.Fatalf("enabled=%v hits=%d want=%d", enabled, hits, wantHits)
					}
				}
			}
		}
	}
}
