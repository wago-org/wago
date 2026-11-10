//go:build (linux || darwin) && arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestRegisterMultiplyPreservesSourceLifetimes(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, mul, xor := wasm.I32, byte(0x6c), byte(0x73)
		if wide {
			typ, mul, xor = wasm.I64, 0x7e, 0x85
		}
		for _, square := range []bool{false, true} {
			for _, ownedRight := range []bool{false, true} {
				rhs := byte(1)
				if square {
					rhs = 0
				}
				// The loop makes the repeatedly read parameters eligible for loop pins.
				body := []byte{0, 0x03, 0x40, 0x0b, 0x20, 0, 0x20, rhs}
				if ownedRight {
					body = append(body, 0x20, 1-rhs, xor)
				}
				body = append(body, mul, 0x20, 0, xor, 0x20, 1, xor, 0x0b)
				params, results := []wasm.ValType{typ, typ}, []wasm.ValType{typ}
				m := modFuncs(t, funcDef{params, results, body}, funcDef{params, results, []byte{0, 0x20, 0, 0x0b}})
				for _, enabled := range []bool{false, true} {
					for _, workers := range []int{1, 4} {
						for _, a := range []uint64{0, 1, 0x80000000, 0x8000000000000000, ^uint64(0)} {
							for _, b := range []uint64{0, 1, 3, 0xdeadbeef01234567, ^uint64(0)} {
								r := b
								if square {
									r = a
								}
								if ownedRight {
									r = a ^ b
								}
								want := a*r ^ a ^ b
								if !wide {
									want = uint64(uint32(want))
								}
								var stats ModuleStats
								opts := CompileOptions{Workers: workers, Optimizations: map[string]bool{"three-op-sink": enabled}}
								if diagnosticsEnabled {
									opts.Stats = &stats
								}
								got, err := runArm64WrapperWithOptions(t, m, opts, a, b)
								if !wide {
									got = uint64(uint32(got))
								}
								if err != nil || got != want {
									t.Fatalf("wide=%t square=%t owned=%t enabled=%t a=%x b=%x got=%x want=%x err=%v", wide, square, ownedRight, enabled, a, b, got, want, err)
								}
								if diagnosticsEnabled && enabled && !ownedRight && stats.Funcs[0].Peephole["register-multiply"] != 1 {
									t.Fatalf("missing multiply cover wide=%t square=%t owned=%t: %v", wide, square, ownedRight, stats.Funcs[0].Peephole)
								}
							}
						}
					}
				}
			}
		}
	}
}
