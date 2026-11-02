//go:build (linux || darwin) && arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestMulAddBorrowsImmutableLoopConstants(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, constant, add, sub, mul := wasm.I32, byte(0x41), byte(0x6a), byte(0x6b), byte(0x6c)
		fold := func(x uint64) uint64 { return uint64(uint32(x)) }
		if wide {
			typ, constant, add, sub, mul = wasm.I64, 0x42, 0x7c, 0x7d, 0x7e
			fold = func(x uint64) uint64 { return x }
		}
		for _, coefficient := range []uint64{17, 29} {
			for _, literalAccumulator := range []bool{false, true} {
				for _, subtract := range []bool{false, true} {
					for _, reverse := range []bool{false, true} {
						body := []byte{1, 1, 0x7f, 0x41, 9, 0x21, 1, 0x03, 0x40}
						putConstant := func() { body = append(body, constant); body = append(body, wasmtest.SLEB64(int64(coefficient))...) }
						if literalAccumulator {
							putConstant()
						} else {
							body = append(body, 0x20, 0)
						}
						if reverse {
							putConstant()
							body = append(body, 0x20, 0)
						} else {
							body = append(body, 0x20, 0)
							putConstant()
						}
						body = append(body, mul)
						if subtract {
							body = append(body, sub)
						} else {
							body = append(body, add)
						}
						body = append(body, 0x21, 0, 0x20, 0)
						// This second use also establishes a lease for reversed-source
						// forms, whose constant is not immediately followed by MUL.
						putConstant()
						body = append(body, mul, 0x21, 0, 0x20, 1, 0x41, 1, 0x6b, 0x22, 1, 0x0d, 0, 0x0b, 0x20, 0, 0x0b)
						m := mod1(t, []wasm.ValType{typ}, []wasm.ValType{typ}, body)
						for _, cache := range []bool{false, true} {
							for _, input := range []uint64{0, 1, 0xdeadbeef80000000, ^uint64(0)} {
								want := fold(input)
								for range 9 {
									accumulator := want
									if literalAccumulator {
										accumulator = coefficient
									}
									product := want * coefficient
									if subtract {
										want = fold(accumulator - product)
									} else {
										want = fold(accumulator + product)
									}
									want = fold(want * coefficient)
								}
								var stats ModuleStats
								opts := CompileOptions{Optimizations: map[string]bool{"loop-int-const": cache, "scoped-loop-int-const": cache, "mul-add-fuse": true}}
								if diagnosticsEnabled {
									opts.Stats = &stats
								}
								got, err := runArm64WrapperWithOptions(t, m, opts, input)
								if err != nil || fold(got) != want {
									t.Fatalf("wide=%v coefficient=%d literal=%v sub=%v reverse=%v cache=%v input=%x got=%x want=%x err=%v", wide, coefficient, literalAccumulator, subtract, reverse, cache, input, got, want, err)
								}
								borrow := coefficient == 29 || literalAccumulator
								if diagnosticsEnabled && (stats.Funcs[0].Peephole["mul-add-cached-constant"] > 0) != (cache && borrow) {
									t.Fatalf("wide=%v coefficient=%d literal=%v cache=%v stats=%v", wide, coefficient, literalAccumulator, cache, stats.Funcs[0].Peephole)
								}
							}
						}
					}
				}
			}
		}
	}
}
