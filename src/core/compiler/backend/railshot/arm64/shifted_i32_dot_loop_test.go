//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func shiftedI32DotSelectionBody() []byte {
	return []byte{1, 1, 0x7f, 0x03, 0x40,
		0x20, 0, 0x28, 2, 0, 0x41, 0x80, 0x7f, 0x6a,
		0x20, 0, 0x20, 1, 0x6a, 0x22, 5, 0x41, 4, 0x6a, 0x28, 2, 0, 0x41, 0x80, 0x7f, 0x6a, 0x6c,
		0x20, 0, 0x41, 0x7c, 0x6a, 0x28, 2, 0, 0x41, 0x80, 0x7f, 0x6a,
		0x20, 5, 0x28, 2, 0, 0x41, 0x80, 0x7f, 0x6a, 0x6c,
		0x20, 4, 0x6a, 0x6a, 0x21, 4, 0x20, 0, 0x41, 8, 0x6a, 0x21, 0,
		0x20, 2, 0x20, 3, 0x41, 0x7e, 0x6a, 0x22, 3, 0x47, 0x0d, 0, 0x0b, 0x20, 4, 0x0b}
}
func TestShiftedI32DotVectorLoopSelection(t *testing.T) {
	requireCompilerDiagnostics(t)
	b := shiftedI32DotSelectionBody()
	m, e := wasm.DecodeModule(wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))), wasmtest.Section(3, wasmtest.Vec([]byte{0})), wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(b))), b...)))))
	if e != nil {
		t.Fatal(e)
	}
	for _, on := range []bool{false, true} {
		var stats ModuleStats
		c, e := CompileModuleWith(m, CompileOptions{Stats: &stats, Optimizations: map[string]bool{"integer-sum-loop": on}})
		if e != nil {
			t.Fatal(e)
		}
		c.CodeImage.Close()
		want := 0
		if on {
			want = 1
		}
		if got := stats.Funcs[0].Peephole["shifted-i32-dot-vector-loop"]; got != want {
			t.Fatalf("enabled=%t selected=%d want=%d", on, got, want)
		}
	}
}
func TestShiftedI32DotVectorLoopPressureAndEligibility(t *testing.T) {
	for _, mode := range []string{"gp", "gp-eight", "vector-seven", "interruptible", "inline", "exception", "calls"} {
		t.Run(mode, func(t *testing.T) {
			f := gpClassEmitter()
			f.policy = currentCodegenPolicy()
			f.m = &wasm.Module{Memories: []wasm.MemType{{}}}
			f.localType = []machineType{mtI32, mtI32, mtI32, mtI32, mtI32, mtI32}
			f.nParams = 5
			switch mode {
			case "gp":
				f.pinned = allAllocatableGP()
			case "gp-eight":
				f.pinned = allAllocatableGP()
				n := 0
				for _, reg := range gpAlloc {
					if n < 8 {
						f.pinned = f.pinned.remove(reg)
						n++
					}
				}
			case "vector-seven":
				f.fpinned = ^regMask(0)
				n := 0
				for _, reg := range fpAllocRegs {
					if (reg < 8 || reg >= 16) && n < 7 {
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
			pins, fpins := f.pinned, f.fpinned
			r := wasm.ReaderFrom(shiftedI32DotSelectionBody()[3:])
			_, _ = r.Byte()
			if done, e := f.tryShiftedI32DotLoop(&r); done || e != nil {
				t.Fatalf("done=%t err=%v", done, e)
			}
			if r.Offset() != 1 || len(f.a.B) != 0 || f.pinned != pins || f.fpinned != fpins {
				t.Fatal("fallback changed source, pins or emission")
			}
		})
	}
}
