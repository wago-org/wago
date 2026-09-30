//go:build linux && arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"math"
	"testing"
)

func TestFlushBelowPreservesLiveSpillSourcesARM64(t *testing.T) {
	for _, deferred := range []bool{false, true} {
		for _, n := range []int{8, 16, 32, 64} {
			body := []byte{0x00, 0x42, 0x07}
			for i := 0; i < n; i++ {
				body = append(body, 0x20, 0x00, 0x20, 0x01, 0x80)
				if deferred {
					body = append(body, 0x42, 0x03, 0x7c)
				}
			}
			body = append(body, 0x20, 0x00, 0x42, 0x00, 0x56, 0x04, 0x40, 0x42, 0x63, 0x21, 0x00, 0x0b)
			for i := 0; i < n; i++ {
				body = append(body, 0x7c)
			}
			body = append(body, 0x0b)
			m := mod1(t, []wasm.ValType{wasm.I64, wasm.I64}, []wasm.ValType{wasm.I64}, body)
			got := runArm64u(t, m, 69, 3)
			want := uint64(7 + n*23)
			if deferred {
				want += uint64(n * 3)
			}
			if got != want {
				t.Fatalf("n=%d deferred=%v: got=%d want=%d", n, deferred, got, want)
			}
		}
	}
}

// A lazy constant is the lowest logical value. Eager square roots exhaust the
// FP bank and spill later values into slot zero before the fused if flushes.
func flushFloatSpillModuleARM64(t testing.TB, n int, deferred, wide bool) *wasm.Module {
	t.Helper()
	body := []byte{0x00, 0x44, 0, 0, 0, 0, 0, 0, 0x1c, 0x40} // f64.const 7
	if wide {
		body = []byte{0x01, 0x01, 0x7e, 0xfd, 0x0c, 7, 0, 0, 0, 0, 0, 0, 0, 11, 0, 0, 0, 0, 0, 0, 0}
	}
	for i := 0; i < n; i++ {
		body = append(body, 0x20, 0x00, 0x9f)
		if deferred {
			body = append(body, 0x44, 0, 0, 0, 0, 0, 0, 0xf0, 0x3f, 0xa0)
		}
	}
	body = append(body, 0x20, 0x01, 0x42, 0x00, 0x56, 0x04, 0x40, 0x44, 0, 0, 0, 0, 0, 0, 0x34, 0x40, 0x21, 0x00, 0x0b)
	for i := 1; i < n; i++ {
		body = append(body, 0xa0)
	}
	if wide {
		body = append(body, 0xb1, 0x21, 0x02, 0xfd, 0x1d, 0x00, 0x20, 0x02, 0x7c)
	} else {
		body = append(body, 0xa0, 0xbd)
	}
	body = append(body, 0x0b)
	return mod1(t, []wasm.ValType{wasm.F64, wasm.I64}, []wasm.ValType{wasm.I64}, body)
}

func TestFlushBelowPreservesLiveFloatSpillsARM64(t *testing.T) {
	for _, deferred := range []bool{false, true} {
		for _, wide := range []bool{false, true} {
			for _, n := range []int{8, 16, 32, 64} {
				m := flushFloatSpillModuleARM64(t, n, deferred, wide)
				if diagnosticsEnabled {
					var stats ModuleStats
					cm, err := CompileModuleWith(m, CompileOptions{Stats: &stats})
					if err != nil {
						t.Fatal(err)
					}
					cm.CodeImage.Close()
					if stats.Funcs[0].Peephole["cmp-branch-fuse"] == 0 {
						t.Fatal("regression did not select fused conditional lowering")
					}
				}
				increment := 3
				if deferred {
					increment++
				}
				want := math.Float64bits(float64(7 + n*increment))
				if wide {
					want = uint64(7 + n*increment)
				}
				for _, condition := range []uint64{0, 1} {
					got := runArm64u(t, m, math.Float64bits(9), condition)
					if got != want {
						t.Fatalf("n=%d deferred=%v wide=%v condition=%d: got=%x want=%x", n, deferred, wide, condition, got, want)
					}
				}
			}
		}
	}
}

func BenchmarkCompileFlushBelowARM64(b *testing.B) {
	for _, n := range []int{8, 32} {
		name := "common8"
		if n == 32 {
			name = "pressure32"
		}
		b.Run(name, func(b *testing.B) {
			m := flushFloatSpillModuleARM64(b, n, false, false)
			cm, err := CompileModule(m)
			if err != nil {
				b.Fatal(err)
			}
			codeBytes := len(cm.Code)
			cm.CodeImage.Close()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				cm, err := CompileModule(m)
				if err != nil {
					b.Fatal(err)
				}
				cm.CodeImage.Close()
			}
			b.ReportMetric(float64(codeBytes), "code-B/op")
		})
	}
}
