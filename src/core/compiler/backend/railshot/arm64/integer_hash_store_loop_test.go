//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func integerHashStoreSelectionBody() []byte {
	expr := []byte{0x41, 0xb7, 0xbc, 2, 0x73, 0x41}
	expr = append(expr, wasmtest.SLEB64(-2048144789)...)
	expr = append(expr, 0x6c, 0x22, 2, 0x41, 13, 0x76, 0x20, 2, 0x73, 0x41, 0xff, 1, 0x71)
	b := []byte{1, 1, 0x7f, 0x03, 0x40, 0x20, 0, 0x20, 1}
	b = append(b, expr...)
	b = append(b, 0x36, 2, 0, 0x20, 0, 0x41, 4, 0x6a, 0x20, 1, 0x41, 1, 0x6a)
	b = append(b, expr...)
	return append(b, 0x36, 2, 0, 0x20, 0, 0x41, 8, 0x6a, 0x21, 0, 0x20, 1, 0x41, 2, 0x6a, 0x22, 1, 0x41, 0x80, 2, 0x47, 0x0d, 0, 0x0b, 0x20, 2, 0x0b)
}

func TestIntegerHashStoreVectorLoopSelection(t *testing.T) {
	requireCompilerDiagnostics(t)
	b := integerHashStoreSelectionBody()
	m, err := wasm.DecodeModule(wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))), wasmtest.Section(3, wasmtest.Vec([]byte{0})), wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(b))), b...)))))
	if err != nil {
		t.Fatal(err)
	}
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
		if got := stats.Funcs[0].Peephole["integer-hash-store-vector-loop"]; got != want {
			t.Fatalf("enabled=%t selected=%d want=%d", on, got, want)
		}
	}
}

func TestIntegerHashStoreVectorLoopPressureAndEligibilityFallback(t *testing.T) {
	body := integerHashStoreSelectionBody()[3:]
	for _, mode := range []string{"gp", "vector-six", "interruptible", "inline", "exception", "calls"} {
		t.Run(mode, func(t *testing.T) {
			f := gpClassEmitter()
			f.policy = currentCodegenPolicy()
			f.m = &wasm.Module{Memories: []wasm.MemType{{}}}
			f.localType = []machineType{mtI32, mtI32, mtI32}
			f.nParams = 2
			switch mode {
			case "gp":
				f.pinned = allAllocatableGP()
			case "vector-six":
				f.fpinned = ^regMask(0)
				available := 0
				for _, reg := range fpAllocRegs {
					if (reg < 8 || reg >= 16) && available < 6 {
						f.fpinned = f.fpinned.remove(reg)
						available++
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
			pins, fpins := f.pinned, f.fpinned
			r := wasm.ReaderFrom(body)
			_, _ = r.Byte()
			if done, err := f.tryIntegerHashStoreLoop(&r); done || err != nil {
				t.Fatalf("fallback done=%t err=%v", done, err)
			}
			if r.Offset() != 1 || len(f.a.B) != 0 || f.pinned != pins || f.fpinned != fpins {
				t.Fatal("fallback changed source, emission or pins")
			}
		})
	}
}
