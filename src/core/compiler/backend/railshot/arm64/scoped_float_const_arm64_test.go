//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"math"
	"testing"
)

func TestScopedFloatConstantsAcrossCalls(t *testing.T) {
	body := []byte{2, 1, 0x7c, 1, 0x7f, 0x20, 0, 0x21, 2, 0x10, 1, 0x1a}
	body = appendF64ConstForCacheTest(body, 1)
	body = append(body, 0x21, 1)
	for _, local := range []byte{0, 2} {
		body = append(body, 0x03, 0x40, 0x20, 1)
		body = appendF64ConstForCacheTest(body, 0.2)
		body = append(body, 0xa2, 0x21, 1, 0x20, local, 0x41, 1, 0x6b, 0x22, local, 0x0d, 0, 0x0b, 0x10, 1, 0x1a)
	}
	body = append(body, 0x20, 1, 0xbd, 0x0b)
	callee := appendF64ConstForCacheTest([]byte{0}, 12.345)
	callee = append(callee, 0x0b)
	m := modFuncs(t, funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I64}, body: body}, funcDef{results: []wasm.ValType{wasm.F64}, body: callee})
	for _, enabled := range []bool{false, true} {
		for _, n := range []uint64{1, 2, 7, 31, 128} {
			want := 1.0
			for i := uint64(0); i < 2*n; i++ {
				want *= 0.2
			}
			var stats ModuleStats
			var out *ModuleStats
			if diagnosticsEnabled {
				out = &stats
			}
			got, err := runArm64WrapperWithOptions(t, m, CompileOptions{Stats: out, Optimizations: map[string]bool{"inline": false, "scoped-loop-float-const": enabled}}, n)
			if err != nil || got != math.Float64bits(want) {
				t.Fatalf("enabled%t n%d: got%x want%x err%v", enabled, n, got, math.Float64bits(want), err)
			}
			if diagnosticsEnabled {
				hits := stats.Funcs[0].Peephole["scoped-loop-float-const"]
				if enabled && hits != 2 || !enabled && hits != 0 {
					t.Fatalf("enabled%t: leases%d", enabled, hits)
				}
			}
		}
	}
}
