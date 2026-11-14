//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestShiftedALUImmediateLocalDestinations(t *testing.T) {
	old := shiftedALUImmediateEnabled
	defer func() { shiftedALUImmediateEnabled = old }()
	for _, wide := range []bool{false, true} {
		typ, constant, add, sub, xor := wasm.I32, byte(0x41), byte(0x6a), byte(0x6b), byte(0x73)
		if wide {
			typ, constant, add, sub, xor = wasm.I64, 0x42, 0x7c, 0x7d, 0x85
		}
		for _, c := range []int64{4096, -4096, 16384, -16384, 0xfff000, -0xfff000, 4097} {
			for _, subtract := range []bool{false, true} {
				op := add
				if subtract {
					op = sub
				}
				for _, dest := range []byte{0, 1} {
					body := []byte{0, 0x41, 0, 0x41, 42, 0x3a, 0, 0, 0x20, 0, constant}
					body = append(body, wasmtest.SLEB64(c)...)
					body = append(body, op, 0x21, dest, 0x20, 0, 0x20, 1, xor, 0x0b)
					m := mod1(t, []wasm.ValType{typ, typ}, []wasm.ValType{typ}, body)
					m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
					for _, on := range []bool{false, true} {
						shiftedALUImmediateEnabled = on
						for _, compact := range []bool{false, true} {
							for _, x := range []uint64{0, 1, 0x7fffffff, 0x80000000, 0xffffffff, 0xfeed00000001, 0x7fffffffffffffff, 0x8000000000000000, 0xffffffffffffffff} {
								y := uint64(0x7123456789abcdef)
								a, b := x, y
								v := x + uint64(c)
								if subtract {
									v = x - uint64(c)
								}
								if dest == 0 {
									a = v
								} else {
									b = v
								}
								want := a ^ b
								if !wide {
									want = uint64(uint32(want))
								}
								var stats ModuleStats
								opts := CompileOptions{CompactNative: compact, Optimizations: map[string]bool{"ordinary-shifted-immediate": true}}
								if diagnosticsEnabled {
									opts.Stats = &stats
								}
								got, err := runArm64WrapperWithOptions(t, m, opts, x, y)
								if err != nil || got != want {
									t.Fatalf("wide%v c%d sub%v dest%d on%v compact%v x%x got%x want%x: %v", wide, c, subtract, dest, on, compact, x, got, want, err)
								}
								if diagnosticsEnabled && on && c != 4097 && stats.Funcs[0].Peephole["shifted-add-sub-immediate"] == 0 {
									t.Fatalf("missed ALU immediate wide%v c%d sub%v dest%d compact%v x%x: %v", wide, c, subtract, dest, compact, x, stats.Funcs[0].Peephole)
								}
							}
						}
					}
				}
			}
		}
	}
}
