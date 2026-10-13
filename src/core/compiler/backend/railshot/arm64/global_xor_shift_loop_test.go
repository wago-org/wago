//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestGlobalXorShiftVectorLoopSelection(t *testing.T) {
	requireCompilerDiagnostics(t)
	b := []byte{1, 1, 0x7f, 0x03, 0x40, 0x20, 1, 0x02, 0x7f, 0x23, 0, 0x42}
	b = append(b, wasmtest.SLEB64(4294967313)...)
	b = append(b, 0x7c, 0x24, 0, 0x23, 0, 0xa7, 0x23, 0, 0x42, 32, 0x88, 0xa7, 0x73, 0x0b, 0x6a, 0x21, 1, 0x20, 2, 0x41, 1, 0x6a, 0x21, 2, 0x20, 2, 0x20, 0, 0x49, 0x0d, 0, 0x0b, 0x20, 1, 0x0b)
	m, err := wasm.DecodeModule(wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))), wasmtest.Section(3, wasmtest.Vec([]byte{0})), wasmtest.Section(6, wasmtest.Vec([]byte{0x7e, 1, 0x42, 0, 0x0b})), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(b))), b...)))))
	if err != nil {
		t.Fatal(err)
	}
	for _, enabled := range []bool{false, true} {
		var stats ModuleStats
		c, err := CompileModuleWith(m, CompileOptions{Stats: &stats, Optimizations: map[string]bool{"integer-sum-loop": enabled}})
		if err != nil {
			t.Fatal(err)
		}
		c.CodeImage.Close()
		want := 0
		if enabled {
			want = 1
		}
		if got := stats.Funcs[0].Peephole["global-xor-shift-vector-loop"]; got != want {
			t.Fatalf("enabled=%t selected=%d want=%d", enabled, got, want)
		}
	}
}

func TestGlobalXorShiftVectorLoopPressureAndEligibilityFallback(t *testing.T) {
	b := []byte{0x03, 0x40, 0x20, 1, 0x02, 0x7f, 0x23, 0, 0x42, 1, 0x7c, 0x24, 0, 0x23, 0, 0xa7, 0x23, 0, 0x42, 32, 0x88, 0xa7, 0x73, 0x0b, 0x6a, 0x21, 1, 0x20, 2, 0x41, 1, 0x6a, 0x21, 2, 0x20, 2, 0x20, 0, 0x49, 0x0d, 0, 0x0b}
	for _, mode := range []string{"gp", "vector", "interruptible", "inline", "exception", "calls", "parameter-counter", "outer-loop"} {
		t.Run(mode, func(t *testing.T) {
			f := gpClassEmitter()
			f.policy = currentCodegenPolicy()
			f.m = &wasm.Module{Globals: []wasm.Global{{Type: wasm.GlobalType{Type: wasm.I64, Mutable: true}}}}
			f.localType, f.nParams = []machineType{mtI32, mtI32, mtI32}, 2
			switch mode {
			case "gp":
				f.pinned = allAllocatableGP()
			case "vector":
				f.fpinned = ^regMask(0)
			case "interruptible":
				f.interruptible = true
			case "inline":
				f.localBase = 1
			case "exception":
				f.moduleEH = true
			case "calls":
				f.usesCalls = true
			case "parameter-counter":
				f.nParams = 3
			case "outer-loop":
				f.ctrl = []ctrlFrame{{kind: cfLoop}}
			}
			pins, fpins := f.pinned, f.fpinned
			r := wasm.ReaderFrom(b)
			_, _ = r.Byte()
			if done, err := f.tryGlobalXorShiftLoop(&r); done || err != nil {
				t.Fatalf("fallback done=%t err=%v", done, err)
			}
			if r.Offset() != 1 || len(f.a.B) != 0 || f.pinned != pins || f.fpinned != fpins {
				t.Fatal("fallback changed source, emission or pins")
			}
		})
	}
}
