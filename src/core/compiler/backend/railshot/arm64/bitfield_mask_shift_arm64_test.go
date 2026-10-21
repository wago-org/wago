//go:build (linux || darwin) && arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestBitfieldMaskBeforeShift(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, c, and, shr := wasm.I32, byte(0x41), byte(0x71), byte(0x76)
		width := uint64(32)
		if wide {
			typ, c, and, shr = wasm.I64, 0x42, 0x83, 0x88
			width = 64
		}
		for _, pair := range [][2]int64{{0xf05, 8}, {0xffff00ff, 16}, {-8, 3}, {-1 << 63, 63}, {0xf05, 72}, {0xf05, -56}, {0x505, 8}, {0, 8}, {0xf05, 0}, {-1, 8}} {
			for _, commuted := range []bool{false, true} {
				mask := uint64(pair[0])
				value := pair[0]
				if !wide {
					mask = uint64(uint32(mask))
					value = int64(int32(value))
				}
				body := []byte{0, 0x3f, 0, 0x1a}
				if !commuted {
					body = append(body, 0x20, 0)
				}
				body = append(body, c)
				body = append(body, wasmtest.SLEB64(value)...)
				if commuted {
					body = append(body, 0x20, 0)
				}
				body = append(body, and, c)
				body = append(body, wasmtest.SLEB64(pair[1])...)
				body = append(body, shr, 0x22, 0, 0x0b)
				m := mod1(t, []wasm.ValType{typ}, []wasm.ValType{typ}, body)
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
				shift := uint64(pair[1]) & (width - 1)
				q := mask >> shift
				full := ^uint64(0)
				if !wide {
					full = 0xffffffff
				}
				selected := shift != 0 && q != 0 && q&(q+1) == 0 && mask != full
				if diagnosticsEnabled {
					got := stats.Funcs[0].Peephole["bitfield-mask-before-shift"] != 0
					if got != selected {
						t.Fatalf("wide=%v pair=%v commuted=%v selected=%v want=%v stats=%v", wide, pair, commuted, got, selected, stats.Funcs[0].Peephole)
					}
				}
				for _, x := range []uint64{0, 1, 0xf00, 0x80000000, 0x8000000000000000, ^uint64(0)} {
					want := (x & mask) >> shift
					got, e := runArm64WrapperWithOptions(t, m, opts, x)
					if e != nil || got != want {
						t.Fatalf("wide=%v pair=%v commuted=%v x=%x got=%x want=%x err=%v", wide, pair, commuted, x, got, want, e)
					}
				}
			}
		}
	}
}
