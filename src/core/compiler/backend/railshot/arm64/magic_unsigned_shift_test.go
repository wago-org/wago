//go:build (linux || darwin) && arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot"
)

func TestMagicUnsigned32CombinedShiftPreservesQuotientAndRemainder(t *testing.T) {
	for _, divisor := range []uint64{3, 5, 7, 17, 20, 31, 37, 63, 65, 255, 257, 65537, 0x80000001, 0xffffffff} {
		_, shift, add := railshot.MagicU(divisor, 32)
		for _, remainder := range []bool{false, true} {
			op := divOp{name: "div_u", opcode: 0x6e}
			if remainder {
				op = divOp{name: "rem_u", opcode: 0x70, rem: true}
			}
			m := divConstMod(t, false, op, divisor)
			values := []uint64{0, 1, divisor - 1, divisor, divisor + 1, divisor*3 - 1, divisor * 3, 0x7fffffff, 0x80000000, 0xffffffff, ^uint64(0)}
			state := uint32(0x91e10da5)
			for range 24 {
				state ^= state << 13
				state ^= state >> 17
				state ^= state << 5
				values = append(values, uint64(state))
			}
			for _, enabled := range []bool{false, true} {
				for _, n := range values {
					var stats ModuleStats
					opts := CompileOptions{Optimizations: map[string]bool{"magic-div": enabled}}
					if diagnosticsEnabled {
						opts.Stats = &stats
					}
					// UMULL must ignore dirty carrier bits above the Wasm i32.
					got, err := runArm64WrapperWithOptions(t, m, opts, n|0xa5a5a5a500000000)
					want := uint32(n) / uint32(divisor)
					if remainder {
						want = uint32(n) % uint32(divisor)
					}
					if err != nil || uint32(got) != want {
						t.Fatalf("op=%s d=%x n=%x enabled=%t got=%x err=%v want=%x", op.name, divisor, n, enabled, got, err, want)
					}
					if diagnosticsEnabled {
						combinedShift := stats.Funcs[0].Peephole["magic-u32-shift"] != 0 || stats.Funcs[0].Peephole["magic-quotient-direct"] != 0 && shift > 0
						if combinedShift != (enabled && !add && shift > 0) {
							t.Fatalf("op=%s d=%x enabled=%t stats=%v", op.name, divisor, enabled, stats.Funcs[0].Peephole)
						}
					}
					shiftedProduct := divisor > 1 && (divisor-1)&(divisor-2) == 0
					if diagnosticsEnabled && (stats.Funcs[0].Peephole["magic-rem-shifted-product"] != 0) != (enabled && remainder && shiftedProduct) {
						t.Fatalf("remainder product d=%x enabled=%t stats=%v", divisor, enabled, stats.Funcs[0].Peephole)
					}
				}
			}
		}
	}
}
