//go:build (linux || darwin) && arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestSignedPowerTwoBiasQuotientsAndRemaindersArm64(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, c, div, rem := wasm.I32, byte(0x41), byte(0x6d), byte(0x6f)
		width := 32
		if wide {
			typ, c, div, rem = wasm.I64, 0x42, 0x7f, 0x81
			width = 64
		}
		for _, k := range []int{1, 2, 7, 8, 15, width - 2, width - 1} {
			for _, negative := range []bool{false, true} {
				divisor := int64(uint64(1) << k)
				if negative {
					divisor = -divisor
				}
				if !wide {
					divisor = int64(int32(divisor))
				}
				if divisor == 0 {
					continue
				}
				values := []int64{0, 1, -1, divisor, divisor - 1, divisor + 1, -divisor, -divisor - 1, -divisor + 1, -1 << 63, 1<<63 - 1, -1 << 31, 1<<31 - 1, 0x12345678, -0x12345678}
				for _, remainder := range []bool{false, true} {
					op := div
					if remainder {
						op = rem
					}
					body := []byte{0, 0x20, 0, c}
					body = append(body, wasmtest.SLEB64(divisor)...)
					body = append(body, op, 0x0b)
					m := mod1(t, []wasm.ValType{typ}, []wasm.ValType{typ}, body)
					for _, enabled := range []bool{false, true} {
						for _, value := range values {
							if !wide {
								value = int64(int32(value))
							}
							want := value / divisor
							if remainder {
								want = value % divisor
							}
							expected := uint64(want)
							if !wide {
								expected = uint64(uint32(want))
							}
							var stats ModuleStats
							opts := CompileOptions{Optimizations: map[string]bool{"shifted-register-alu": enabled}}
							if diagnosticsEnabled {
								opts.Stats = &stats
							}
							got, err := runArm64WrapperWithOptions(t, m, opts, uint64(value))
							if err != nil || got != expected {
								t.Fatalf("wide=%v divisor=%d value=%d remainder=%v enabled=%v: got=%x want=%x err=%v", wide, divisor, value, remainder, enabled, got, expected, err)
							}
							if diagnosticsEnabled {
								hits := stats.Funcs[0].Peephole["signed-power-two-shifted-bias"]
								if (hits != 0) != (enabled && !remainder) {
									t.Fatalf("enabled=%v remainder=%v: selections=%d", enabled, remainder, hits)
								}
							}
						}
					}
				}
			}
		}
	}
}
