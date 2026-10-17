//go:build arm64

package arm64

import (
	"math/bits"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestWrappedWideShiftedRegisterALU(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, shift := range []byte{0x86, 0x87, 0x88, 0x89, 0x8a} {
		for _, op := range []byte{0x6a, 0x6b, 0x71, 0x72, 0x73} {
			if (shift == 0x89 || shift == 0x8a) && (op == 0x6a || op == 0x6b) {
				continue // ADD/SUB have no rotated register operand.
			}
			for _, count := range []int64{13, 31, 32, 63, 77, -1} {
				// The i64 result also observes whether the i32 sink is canonical.
				body := []byte{0, 0x20, 0, 0xa7, 0x20, 1, 0x42}
				body = append(body, wasmtest.SLEB64(count)...)
				body = append(body, shift, 0xa7, op, 0xad, 0x0b)
				m := mod1(t, []wasm.ValType{wasm.I64, wasm.I64}, []wasm.ValType{wasm.I64}, body)
				var on, off ModuleStats
				for _, cfg := range []struct {
					enabled bool
					stats   *ModuleStats
				}{{true, &on}, {false, &off}} {
					c, err := CompileModuleWith(m, CompileOptions{Stats: cfg.stats, Optimizations: map[string]bool{"shifted-register-alu": cfg.enabled}})
					if err != nil {
						t.Fatal(err)
					}
					c.CodeImage.Close()
				}
				if on.Funcs[0].Peephole["wrapped-shifted-register-alu"] != 1 || off.Funcs[0].Peephole["wrapped-shifted-register-alu"] != 0 {
					t.Fatalf("shift=%x op=%x count=%d: cover not selected only when enabled", shift, op, count)
				}
				if on.Funcs[0].CodeBytes+4 > off.Funcs[0].CodeBytes {
					t.Fatalf("shift=%x op=%x count=%d: on=%d off=%d; no instruction saved", shift, op, count, on.Funcs[0].CodeBytes, off.Funcs[0].CodeBytes)
				}
				for _, inputs := range [][2]uint64{{0, 0}, {0x8000000000000001, 0xfedcba9876543210}, {^uint64(0), ^uint64(0)}, {0x0123456789abcdef, 0x8000000000000000}} {
					x, y, k := inputs[0], inputs[1], uint(count)&63
					switch shift {
					case 0x86:
						y <<= k
					case 0x87:
						y = uint64(int64(y) >> k)
					case 0x88:
						y >>= k
					case 0x89:
						y = bits.RotateLeft64(y, int(k))
					case 0x8a:
						y = bits.RotateLeft64(y, -int(k))
					}
					want := uint32(x)
					switch op {
					case 0x6a:
						want += uint32(y)
					case 0x6b:
						want -= uint32(y)
					case 0x71:
						want &= uint32(y)
					case 0x72:
						want |= uint32(y)
					case 0x73:
						want ^= uint32(y)
					}
					got, err := runArm64WrapperWithOptions(t, m, CompileOptions{}, inputs[:]...)
					if err != nil || got != uint64(want) {
						t.Fatalf("shift=%x op=%x count=%d inputs=%x got=%x want=%x err=%v", shift, op, count, inputs, got, want, err)
					}
				}
			}
		}
	}
}
