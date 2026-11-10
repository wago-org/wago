//go:build (linux || darwin) && arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestMulAddAffineImmediateLanesAndBorrowedInput(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, cons, xor := wasm.I32, byte(0x41), byte(0x73)
		if wide {
			typ, cons, xor = wasm.I64, 0x42, 0x85
		}
		for _, factor := range []uint64{3, 5, 9, 17, 129, 0x80000001} {
			for _, addend := range []int64{-4096, -1, 2, 4095, 4096, 65536} {
				for _, prefix := range []bool{false, true} {
					body := []byte{0, 0x3f, 0, 0x1a, 0x20, 0, cons}
					if wide {
						body = append(body, wasmtest.SLEB64(int64(factor))...)
					} else {
						body = append(body, wasmtest.SLEB32(int32(uint32(factor)))...)
					}
					mul, add := byte(0x6c), byte(0x6a)
					if wide {
						mul, add = 0x7e, 0x7c
					}
					body = append(body, mul, cons)
					if wide {
						body = append(body, wasmtest.SLEB64(addend)...)
					} else {
						body = append(body, wasmtest.SLEB32(int32(addend))...)
					}
					body = append(body, add)
					if prefix {
						body = append(body, 0x20, 0, xor)
					}
					body = append(body, 0x0b)
					m := mod1(t, []wasm.ValType{typ}, []wasm.ValType{typ}, body)
					m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
					for _, enabled := range []bool{false, true} {
						for _, n := range []uint64{0, 1, 2, 0x7fffffff, 0x80000000, 0xffffffff, 0x8000000000000000, 0xffffffffffffffff} {
							opts := CompileOptions{Optimizations: map[string]bool{"mul-add-fuse": enabled}}
							var stats ModuleStats
							if diagnosticsEnabled {
								opts.Stats = &stats
							}
							got, err := runArm64WrapperWithOptions(t, m, opts, n)
							want := n*factor + uint64(addend)
							if prefix {
								want ^= n
							}
							if !wide {
								want = uint64(uint32(want))
							}
							if err != nil || got != want {
								t.Fatalf("wide=%t factor=%d add=%d prefix=%t enabled=%t n=%x got=%x,%v want %x", wide, factor, addend, prefix, enabled, n, got, err, want)
							}
							if diagnosticsEnabled && enabled && fitsAddSubImm12(addend) && stats.Funcs[0].Peephole["mul-add-affine-immediate"] == 0 {
								t.Fatalf("affine immediate path not selected: wide=%t factor=%d addend=%d prefix=%t", wide, factor, addend, prefix)
							}
						}
					}
				}
			}
		}
	}
}
