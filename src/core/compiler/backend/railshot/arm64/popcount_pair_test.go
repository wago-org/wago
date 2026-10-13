//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"math/bits"
	"testing"
)

func TestPopcountPairSelectionAndExecution(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, wide := range []bool{false, true} {
		for _, accumulator := range []bool{false, true} {
			typ, pop, add := wasm.I32, byte(0x69), byte(0x6a)
			if wide {
				typ, pop, add = wasm.I64, 0x7b, 0x7c
			}
			body := []byte{0, 0x20, 0, pop, 0x20, 1, pop}
			if accumulator {
				body = append(body, 0x20, 2, add)
			}
			body = append(body, add, 0x0b)
			m := mod1(t, []wasm.ValType{typ, typ, typ}, []wasm.ValType{typ}, body)
			var stats ModuleStats
			c, err := CompileModuleWith(m, CompileOptions{Stats: &stats})
			if err != nil {
				t.Fatal(err)
			}
			c.CodeImage.Close()
			var off ModuleStats
			rollback, err := CompileModuleWith(m, CompileOptions{Stats: &off, Optimizations: map[string]bool{"popcount-pair": false}})
			if err != nil {
				t.Fatal(err)
			}
			rollback.CodeImage.Close()
			if off.Funcs[0].Peephole["popcount-pair"] != 0 {
				t.Fatal("disabled optimization selected")
			}
			if stats.Funcs[0].CodeBytes >= off.Funcs[0].CodeBytes {
				t.Fatalf("fused bytes=%d rollback=%d", stats.Funcs[0].CodeBytes, off.Funcs[0].CodeBytes)
			}

			if got := stats.Funcs[0].Peephole["popcount-pair"]; got != 1 {
				t.Fatalf("wide=%t accumulator=%t selected=%d", wide, accumulator, got)
			}
			for _, a := range []uint64{0, 1, 0xffffffff, 0xffffffffffffffff, 0x5555555555555555} {
				for _, b := range []uint64{0, 3, 0x8000000000000000, ^uint64(0)} {
					want := uint64(bits.OnesCount64(a) + bits.OnesCount64(b))
					if !wide {
						want = uint64(bits.OnesCount32(uint32(a)) + bits.OnesCount32(uint32(b)))
					}
					acc := ^uint64(0) - 31
					if accumulator {
						want += acc
					}
					if !wide {
						want = uint64(uint32(want))
					}
					got, err := runArm64WrapperWithOptions(t, m, CompileOptions{}, a, b, acc)
					if err != nil || got != want {
						t.Fatalf("wide=%t acc=%t a=%x b=%x got=%x err=%v want=%x", wide, accumulator, a, b, got, err, want)
					}
				}
			}
		}
	}
}
