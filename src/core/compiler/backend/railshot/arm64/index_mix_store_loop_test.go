//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestIndexMixStoreVectorSelection(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, first := range []bool{false, true} {
		b := append([]byte{0, 3}, wasmtest.IndexMixStoreLoopBody(0x9e3779b9, 0x85ebca6b, 65535, 0, 16, 13, first, nil)...)
		b = append(b, 0x20, 2, 0x0b)
		m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, b)
		for _, on := range []bool{false, true} {
			var stats ModuleStats
			c, err := CompileModuleWith(m, CompileOptions{Stats: &stats, Optimizations: map[string]bool{"bulk-fill-vector": on}})
			if err != nil {
				t.Fatal(err)
			}
			c.CodeImage.Close()
			want := 0
			if on {
				want = 1
			}
			if got := stats.Funcs[0].Peephole["index-mix-store-vector-loop"]; got != want {
				t.Fatalf("boundFirst%t on%t got%d want%d", first, on, got, want)
			}
		}
	}
}

func TestIndexMixStoreEligibilityPreservesFallback(t *testing.T) {
	for _, mode := range []string{"stack", "gp-three", "vector-five", "interruptible", "inline", "exception", "calls"} {
		t.Run(mode, func(t *testing.T) {
			f := gpClassEmitter()
			f.policy = currentCodegenPolicy()
			f.m = &wasm.Module{Memories: []wasm.MemType{{}}}
			f.localType = []machineType{mtI32, mtI32, mtI32, mtI32}
			f.nParams = 4
			switch mode {
			case "stack":
				f.pushValue(storage{kind: stConst, typ: mtI32, cval: 42})
			case "gp-three":
				f.pinned = allAllocatableGP()
				for _, reg := range gpAlloc[:3] {
					f.pinned = f.pinned.remove(reg)
				}
			case "vector-five":
				f.fpinned = ^regMask(0)
				n := 0
				for _, reg := range fpAllocRegs {
					if (reg < 8 || reg >= 16) && n < 5 {
						f.fpinned = f.fpinned.remove(reg)
						n++
					}
				}
			case "interruptible":
				f.interruptible = true
			case "inline":
				f.localBase = 1
			case "exception":
				f.moduleEH = true
			case "calls":
				f.usesCalls = true
			}
			pins, fpins, depth := f.pinned, f.fpinned, f.depth()
			r := wasm.ReaderFrom(wasmtest.IndexMixStoreLoopBody(0x9e3779b9, 0x85ebca6b, 65535, 0, 16, 13, true, nil))
			if done, err := f.tryIndexMixStoreLoop(&r); done || err != nil || r.Offset() != 0 || f.depth() != depth || f.pinned != pins || f.fpinned != fpins || len(f.a.B) != 0 {
				t.Fatalf("fallback mutated state: done%t err%v", done, err)
			}
		})
	}
}
