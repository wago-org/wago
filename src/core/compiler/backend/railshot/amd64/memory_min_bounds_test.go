//go:build amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestMemoryMinimumBoundsExpressionProof(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, wide := range []bool{false, true} {
		typ, c, and, shl, add := wasm.I32, byte(0x41), byte(0x71), byte(0x74), byte(0x6a)
		if wide {
			typ, c, and, shl, add = wasm.I64, 0x42, 0x83, 0x86, 0x7c
		}
		body := []byte{0, 0x20, 0, c, 0xff, 0x3f, and, c, 2, shl, c, 1, add, 0x28, 0, 0, 0x0b}
		for _, shared := range []bool{false, true} {
			m := modMem(t, 1, []wasm.ValType{typ}, []wasm.ValType{wasm.I32}, body)
			m.Memories[0].Shared = shared
			m.Memories[0].Limits.Addr64 = wide
			m.Memories[0].Limits.HasMax = true
			m.Memories[0].Limits.Max = 2
			for _, enabled := range []bool{false, true} {
				var stats ModuleStats
				cm, err := CompileModuleWith(m, CompileOptions{Stats: &stats, Optimizations: map[string]bool{"memory-min-bounds": enabled}})
				if err != nil {
					t.Fatal(err)
				}
				cm.CodeImage.Close()
				want := 0
				if enabled {
					want = 1
				}
				if got := stats.Funcs[0].Peephole["memory-min-bounds"]; got != want {
					t.Fatalf("wide=%t shared=%t enabled=%t: proofs=%d want=%d", wide, shared, enabled, got, want)
				}
			}
			m.Memories[0].Limits.Min = 0
			var stats ModuleStats
			cm, err := CompileModuleWith(m, CompileOptions{Stats: &stats})
			if err != nil {
				t.Fatal(err)
			}
			cm.CodeImage.Close()
			if stats.Funcs[0].Peephole["memory-min-bounds"] != 0 {
				t.Fatal("zero-minimum access received proof")
			}
		}
	}
}

func TestMemoryMinimumBoundsConstantFill(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, shared := range []bool{false, true} {
		for _, enabled := range []bool{false, true} {
			body := []byte{0, 0x20, 0, 0x41, 0xff, 0x3f, 0x71, 0x41, 2, 0x74, 0x41, 1, 0x6a, 0x41, 7, 0x41, 33, 0xfc, 11, 0, 0x41, 42, 0x0b}
			m := modMem(t, 1, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
			m.Memories[0].Shared = shared
			m.Memories[0].Limits.HasMax = true
			m.Memories[0].Limits.Max = 2
			var stats ModuleStats
			cm, err := CompileModuleWith(m, CompileOptions{Stats: &stats, Optimizations: map[string]bool{"memory-min-bounds": enabled}})
			if err != nil {
				t.Fatal(err)
			}
			cm.CodeImage.Close()
			want := 0
			if enabled {
				want = 1
			}
			if got := stats.Funcs[0].Peephole["memory-min-bounds"]; got != want {
				t.Fatalf("shared=%t enabled=%t: fill proofs=%d want=%d", shared, enabled, got, want)
			}
		}
	}
}
