//go:build arm64

package arm64

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
				cm, err := CompileModuleWith(m, CompileOptions{Stats: &stats, Optimizations: map[string]bool{"memory-min-bounds": enabled, "bounds-facts": false}})
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

func TestMemoryMinimumBoundsConstantCopy(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, wide := range []bool{false, true} {
		typ, c, and, shl := wasm.I32, byte(0x41), byte(0x71), byte(0x74)
		if wide {
			typ, c, and, shl = wasm.I64, 0x42, 0x83, 0x86
		}
		for _, n := range []byte{0, 4, 33, 63} {
			for _, mins := range [][2]uint64{{1, 1}, {0, 1}, {1, 0}, {0, 0}} {
				for _, enabled := range []bool{false, true} {
					body := []byte{0, 0x20, 0, c, 0xff, 0x3f, and, c, 2, shl, c, 1, 0x6a}
					if wide {
						body[len(body)-1] = 0x7c
					}
					body = append(body, 0x20, 1, c, 0xff, 0x3f, and, c, 2, shl, c, 1)
					if wide {
						body = append(body, 0x7c)
					} else {
						body = append(body, 0x6a)
					}
					body = append(body, c, n, 0xfc, 10, 1, 0, 0x41, 42, 0x0b)
					m := modMem(t, 1, []wasm.ValType{typ, typ}, []wasm.ValType{wasm.I32}, body)
					m.Memories = append(m.Memories, m.Memories[0])
					for i := range m.Memories {
						m.Memories[i].Limits.Min = mins[i]
						m.Memories[i].Limits.HasMax = true
						m.Memories[i].Limits.Max = 2
						m.Memories[i].Limits.Addr64 = wide
					}
					m.Memories[0].Shared = true
					var stats ModuleStats
					cm, err := CompileModuleWith(m, CompileOptions{Stats: &stats, Optimizations: map[string]bool{"memory-min-bounds": enabled}})
					if err != nil {
						t.Fatal(err)
					}
					cm.CodeImage.Close()
					want := int(mins[0] + mins[1])
					if !enabled || n == 0 || wide {
						want = 0
					}
					if got := stats.Funcs[0].Peephole["memory-min-bounds"]; got != want {
						t.Fatalf("wide=%t n=%d minima=%v enabled=%t proofs=%d want=%d", wide, n, mins, enabled, got, want)
					}
				}
			}
		}
	}
}
