//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func weightedI32WindowSelectionBody(n int) []byte {
	b := []byte{0, 0x03, 0x40}
	get := func(x byte) { b = append(b, 0x20, x) }
	con := func(x int32) { b = append(b, 0x41); b = append(b, wasmtest.SLEB64(int64(x))...) }
	for i := 0; i < n; i++ {
		get(byte(i + 4))
		get(0)
		off := int32((n - i - 1) * 4)
		if off != 0 {
			con(off)
			b = append(b, 0x6a)
			if off == 4 {
				b = append(b, 0x22, 1)
			}
		}
		b = append(b, 0x28, 2, 0)
		con(-32768)
		b = append(b, 0x6a, 0x6c)
	}
	for i := 1; i < n; i++ {
		b = append(b, 0x6a)
	}
	con(128)
	b = append(b, 0x6d)
	get(3)
	b = append(b, 0x73)
	con(16777619)
	b = append(b, 0x6c, 0x21, 3)
	get(1)
	b = append(b, 0x21, 0)
	get(2)
	con(-1)
	b = append(b, 0x6a, 0x22, 2, 0x0d, 0, 0x0b)
	get(3)
	return append(b, 0x0b)
}
func TestWeightedI32WindowVectorSelection(t *testing.T) {
	requireCompilerDiagnostics(t)
	pinnedSelections := 0
	for _, n := range []int{4, 8, 12, 32} {
		b := weightedI32WindowSelectionBody(n)
		params := make([]wasm.ValType, n+4)
		for i := range params {
			params[i] = wasm.I32
		}
		m, e := wasm.DecodeModule(wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, []wasm.ValType{wasm.I32}))), wasmtest.Section(3, wasmtest.Vec([]byte{0})), wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(b))), b...)))))
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
			if got := stats.Funcs[0].Peephole["weighted-i32-window-vector-loop"]; got != want {
				t.Fatalf("terms%d enabled%t got%d want%d", n, on, got, want)
			}
			pinned := stats.Funcs[0].Peephole["weighted-window-pinned-counter"]
			if !on && pinned != 0 {
				t.Fatal("disabled optimization borrowed a counter register")
			}
			pinnedSelections += pinned
		}
	}
	if pinnedSelections == 0 {
		t.Fatal("selection fixtures did not exercise a pinned counter")
	}
}
func TestWeightedI32WindowPressureAndEligibility(t *testing.T) {
	for _, mode := range []string{"stack", "gp-three", "vector-fourteen", "interruptible", "inline", "exception", "calls"} {
		t.Run(mode, func(t *testing.T) {
			f := gpClassEmitter()
			f.policy = currentCodegenPolicy()
			f.m = &wasm.Module{Memories: []wasm.MemType{{}}}
			f.localType = make([]machineType, 36)
			for i := range f.localType {
				f.localType[i] = mtI32
			}
			f.nParams = 36
			switch mode {
			case "stack":
				f.pushValue(storage{kind: stConst, typ: mtI32, cval: 42})
			case "gp-three":
				f.pinned = allAllocatableGP()
				for i, reg := range gpAlloc {
					if i < 3 {
						f.pinned = f.pinned.remove(reg)
					}
				}
			case "vector-fourteen":
				f.fpinned = ^regMask(0)
				n := 0
				for _, reg := range fpAllocRegs {
					if (reg < 8 || reg >= 16) && n < 14 {
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
			depth := f.depth()
			r := wasm.ReaderFrom(weightedI32WindowSelectionBody(32)[1:])
			_, _ = r.Byte()
			if done, e := f.tryWeightedI32WindowLoop(&r); done || e != nil {
				t.Fatalf("done=%t err=%v", done, e)
			}
			if f.depth() != depth || r.Offset() != 1 || len(f.a.B) != 0 || f.pinned != pins || f.fpinned != fpins {
				t.Fatal("fallback changed source, pins or emission")
			}
		})
	}
}
