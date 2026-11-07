//go:build (linux || darwin) && arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestLocalMemorySinkPreservesEarlierReadsArm64(t *testing.T) {
	for _, tc := range []struct {
		op     byte
		size   int
		signed bool
	}{{0x28, 4, false}, {0x2c, 1, true}, {0x2d, 1, false}, {0x2e, 2, true}, {0x2f, 2, false}} {
		for _, tee := range []bool{false, true} {
			for _, oldLoad := range []bool{false, true} {
				body := []byte{0, 0x41, 0, 0x41}
				body = append(body, wasmtest.SLEB32(-2005440939)...)
				body = append(body, 0x36, 2, 0, 0x41, 4, 0x41)
				body = append(body, wasmtest.SLEB32(-19088744)...)
				body = append(body, 0x36, 2, 0, 0x20, 0)
				if oldLoad {
					body = append(body, 0x28, 2, 0)
				} else {
					body = append(body, 0x41, 9, 0x6a)
				}
				body = append(body, 0x20, 0, tc.op, 0, 4)
				if tee {
					body = append(body, 0x22, 0, 0x20, 0, 0x6a)
				} else {
					body = append(body, 0x21, 0, 0x20, 0)
				}
				body = append(body, 0x73, 0x0b)
				m := modMem(t, 1, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
				for _, enabled := range []bool{false, true} {
					for _, guard := range []bool{false, true} {
						for _, input := range []uint64{0, 4, 0x8877665500000000, 0x8877665500000004} {
							p := uint32(input)
							old := p + 9
							if oldLoad {
								if p == 0 {
									old = 0x88776655
								} else {
									old = 0xfedcba98
								}
							}
							var value uint32
							if p == 0 {
								value = 0xfedcba98
							}
							switch tc.size {
							case 1:
								if tc.signed {
									value = uint32(int32(int8(value)))
								} else {
									value = uint32(uint8(value))
								}
							case 2:
								if tc.signed {
									value = uint32(int32(int16(value)))
								} else {
									value = uint32(uint16(value))
								}
							}
							if tee {
								value += value
							}
							want := old ^ value
							var stats ModuleStats
							opts := CompileOptions{ElideBoundsChecks: guard, Optimizations: map[string]bool{"unary-sink": enabled, "tee-sink": true, "load-pair": false, "store-forward": false}}
							if diagnosticsEnabled {
								opts.Stats = &stats
							}
							got, err := runArm64WrapperWithOptions(t, m, opts, input)
							if err != nil || got != uint64(want) {
								t.Fatalf("op=%x tee=%v oldload=%v enabled=%v guard=%v input=%x: got=%x want=%x err=%v", tc.op, tee, oldLoad, enabled, guard, input, got, want, err)
							}
							if diagnosticsEnabled {
								hits := stats.Funcs[0].Peephole["local-memory-sink"]
								if (hits != 0) != enabled {
									t.Fatalf("enabled=%v: selections=%d", enabled, hits)
								}
							}
						}
						// This low-level wrapper does not install signal handling for
						// guard pages. Exercise trap preservation in explicit mode.
						if !guard {
							if _, err := runArm64WrapperWithOptions(t, m, CompileOptions{Optimizations: map[string]bool{"unary-sink": enabled, "tee-sink": true}}, 65532); err == nil {
								t.Fatalf("op=%x tee=%v enabled=%v: cross-boundary load did not trap", tc.op, tee, enabled)
							}
						}
					}
				}
			}
		}
	}
}
