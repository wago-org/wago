//go:build (linux || darwin) && arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestVariableShiftThreeOperand(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for k := byte(0); k < 3; k++ {
			typ, op, xor := wasm.I32, byte(0x74), byte(0x73)
			if wide {
				typ, op, xor = wasm.I64, 0x86, 0x85
			}
			body := []byte{0, 0x3f, 0, 0x1a, 0x20, 0, 0x20, 1, op + k, 0x22, 1, 0x20, 0, xor, 0x0b}
			m := mod1(t, []wasm.ValType{typ, typ}, []wasm.ValType{typ}, body)
			m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
			var stats ModuleStats
			opts := CompileOptions{}
			if diagnosticsEnabled {
				opts.Stats = &stats
			}
			cm, e := CompileModuleWith(m, opts)
			if e != nil {
				t.Fatal(e)
			}
			if cm.CodeImage != nil {
				defer cm.CodeImage.Close()
			}
			if diagnosticsEnabled && stats.Funcs[0].Peephole["variable-shift-three-operand"] != 1 {
				t.Fatalf("missing source/count reuse: %v", stats.Funcs[0].Peephole)
			}
			for _, x := range []uint64{0, 1, 0x80000000, 0x8000000000000000, ^uint64(0)} {
				for _, c := range []uint64{0, 1, 31, 32, 63, 64, 127, ^uint64(0)} {
					count := c & 63
					if !wide {
						count &= 31
						x = uint64(uint32(x))
					}
					want := x << count
					if k == 1 {
						if wide {
							want = uint64(int64(x) >> count)
						} else {
							want = uint64(uint32(int32(x) >> count))
						}
					} else if k == 2 {
						want = x >> count
					}
					want ^= x
					if !wide {
						want = uint64(uint32(want))
					}
					got, e := runArm64WrapperWithOptions(t, m, opts, x, c)
					if !wide {
						got = uint64(uint32(got))
					}
					if e != nil || got != want {
						t.Fatalf("wide=%v op=%d x=%x c=%x got=%x want=%x err=%v", wide, k, x, c, got, want, e)
					}
				}
			}
		}
	}
}
