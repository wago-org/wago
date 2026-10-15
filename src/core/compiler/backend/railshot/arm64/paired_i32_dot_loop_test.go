//go:build arm64

package arm64

import (
	"bytes"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func pairedI32DotSelectionBody() []byte {
	return []byte{1, 2, 0x7f, 0x03, 0x40,
		0x20, 0, 0x20, 2, 0x6a, 0x22, 4, 0x41, 4, 0x6a, 0x28, 2, 0, 0x41, 0x80, 0x7f, 0x6a,
		0x20, 1, 0x20, 2, 0x6a, 0x22, 5, 0x41, 4, 0x6a, 0x28, 2, 0, 0x41, 0x80, 0x7f, 0x6a, 0x6c,
		0x20, 4, 0x28, 2, 0, 0x41, 0x80, 0x7f, 0x6a, 0x20, 5, 0x28, 2, 0, 0x41, 0x80, 0x7f, 0x6a, 0x6c,
		0x20, 3, 0x6a, 0x6a, 0x21, 3, 0x20, 2, 0x41, 8, 0x6a, 0x22, 2, 0x41, 0x80, 2, 0x47, 0x0d, 0, 0x0b, 0x20, 3, 0x0b}
}

func TestPairedI32DotVectorLoopSelection(t *testing.T) {
	requireCompilerDiagnostics(t)
	b := pairedI32DotSelectionBody()
	m, err := wasm.DecodeModule(wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))), wasmtest.Section(3, wasmtest.Vec([]byte{0})), wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(b))), b...)))))
	if err != nil {
		t.Fatal(err)
	}
	for _, on := range []bool{false, true} {
		var stats ModuleStats
		c, err := CompileModuleWith(m, CompileOptions{Stats: &stats, Optimizations: map[string]bool{"integer-sum-loop": on}})
		if err != nil {
			t.Fatal(err)
		}
		c.CodeImage.Close()
		want := 0
		if on {
			want = 1
		}
		if got := stats.Funcs[0].Peephole["paired-i32-dot-vector-loop"]; got != want {
			t.Fatalf("enabled=%t selected=%d want=%d", on, got, want)
		}
	}
}

func TestPairedI32DotVectorLoopPressureAndEligibilityFallback(t *testing.T) {
	body := pairedI32DotSelectionBody()[3:]
	for _, mode := range []string{"gp", "vector", "vector-five", "vector-seven", "interruptible", "inline", "exception", "calls"} {
		t.Run(mode, func(t *testing.T) {
			f := gpClassEmitter()
			f.policy = currentCodegenPolicy()
			f.m = &wasm.Module{Memories: []wasm.MemType{{}}}
			f.localType = []machineType{mtI32, mtI32, mtI32, mtI32, mtI32, mtI32}
			f.nParams = 4
			switch mode {
			case "gp":
				f.pinned = allAllocatableGP()
			case "vector":
				f.fpinned = ^regMask(0)
			case "vector-five", "vector-seven":
				limit := 5
				if mode == "vector-seven" {
					limit = 7
				}
				f.fpinned = ^regMask(0)
				available := 0
				for _, reg := range fpAllocRegs {
					if (reg < 8 || reg >= 16) && available < limit {
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
			if done, err := f.tryPairedI32DotLoop(&r); done || err != nil {
				t.Fatalf("fallback done=%t err=%v", done, err)
			}
			if r.Offset() != 1 || len(f.a.B) != 0 || f.pinned != pins || f.fpinned != fpins {
				t.Fatal("fallback changed source, emission or pins")
			}
		})
	}
}

func TestPairedI32DotVectorLoopLiteralPressure(t *testing.T) {
	requireCompilerDiagnostics(t)
	b := pairedI32DotSelectionBody()
	b[1] = 7 // Eleven live locals, including the four parameters.
	prefix := []byte{}
	for x := byte(6); x < 11; x++ {
		prefix = append(prefix, 0x20, 0, 0x21, x)
	}
	b = append(append(append([]byte{}, b[:3]...), prefix...), b[3:]...)
	literal := append([]byte{0x41}, wasmtest.SLEB32(0x1234567)...)
	b = bytes.ReplaceAll(b, []byte{0x41, 0x80, 0x7f}, literal)
	for x := byte(6); x < 11; x++ {
		b = append(b[:len(b)-1], 0x20, x, 0x6a, 0x0b)
	}
	b = append(b[:len(b)-1], append(literal, 0x6a, 0x0b)...)
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})), wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(b))), b...)))))
	if err != nil {
		t.Fatal(err)
	}
	var stats ModuleStats
	c, err := CompileModuleWith(m, CompileOptions{Stats: &stats})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CodeImage.Close()
	if stats.Funcs[0].Peephole["paired-i32-dot-vector-loop"] != 1 {
		t.Fatalf("lost vector path under literal pressure: %v", stats.Funcs[0].Peephole)
	}
}
