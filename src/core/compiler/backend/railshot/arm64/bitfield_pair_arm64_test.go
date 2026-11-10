//go:build (linux || darwin) && arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestBitfieldPairSelection(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, c, shr, and, shl := wasm.I32, byte(0x41), byte(0x76), byte(0x71), byte(0x74)
		if wide {
			typ, c, shr, and, shl = wasm.I64, 0x42, 0x88, 0x83, 0x86
		}
		for _, tc := range []struct {
			body []byte
			want func(uint64) uint64
		}{
			{[]byte{0, 0x20, 0, c, 11, shr, c, 31, and, 0x0b}, func(x uint64) uint64 { return (x >> 11) & 31 }},
			{[]byte{0, 0x20, 0, c, 31, and, c, 8, shl, 0x0b}, func(x uint64) uint64 { return (x & 31) << 8 }},
		} {
			body := append([]byte{0, 0x3f, 0, 0x1a}, tc.body[1:]...)
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
			if diagnosticsEnabled && stats.Funcs[0].Peephole["bitfield-pair"] != 1 {
				t.Fatalf("pair not selected: %v", stats.Funcs[0].Peephole)
			}
			for _, x := range []uint64{0, 1, 31, 32, 0x80000000, 0x8000000000000000, ^uint64(0)} {
				got, e := runArm64WrapperWithOptions(t, m, opts, x)
				if e != nil || got != tc.want(x) {
					t.Fatalf("wide=%v x=%x got=%x want=%x err=%v", wide, x, got, tc.want(x), e)
				}
			}
		}
	}
}

func TestBitfieldShiftPairBoundaries(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, c, shl, shr := wasm.I32, byte(0x41), byte(0x74), byte(0x76)
		if wide {
			typ, c, shl, shr = wasm.I64, 0x42, 0x86, 0x88
		}
		for _, signed := range []bool{false, true} {
			for _, pair := range [][2]int64{{1, 1}, {20, 27}, {27, 20}, {31, 1}, {1, 31}, {63, 1}, {1, 63}, {-1, 65}, {64, 0}, {0, 64}} {
				body := []byte{0, 0x3f, 0, 0x1a, 0x20, 0, c}
				body = append(body, wasmtest.SLEB64(pair[0])...)
				body = append(body, shl, c)
				body = append(body, wasmtest.SLEB64(pair[1])...)
				op := shr
				if signed {
					op--
				}
				body = append(body, op, 0x22, 0, 0x0b)
				m := mod1(t, []wasm.ValType{typ}, []wasm.ValType{typ}, body)
				m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
				mask := uint64(31)
				if wide {
					mask = 63
				}
				l, r := uint64(pair[0])&mask, uint64(pair[1])&mask
				for _, x := range []uint64{0, 1, 0x80000000, 0x8000000000000000, ^uint64(0)} {
					want := x << l
					if !wide {
						want = uint64(uint32(want))
					}
					if signed {
						if wide {
							want = uint64(int64(want) >> r)
						} else {
							want = uint64(uint32(int32(want) >> r))
						}
					} else {
						want >>= r
					}
					got, e := runArm64WrapperWithOptions(t, m, CompileOptions{}, x)
					if !wide {
						got = uint64(uint32(got))
					}
					if e != nil || got != want {
						t.Fatalf("wide=%v signed=%v pair=%v x=%x got=%x want=%x err=%v", wide, signed, pair, x, got, want, e)
					}
				}
			}
		}
	}
}

func TestBitfieldUnaryFallback(t *testing.T) {
	for _, tc := range []struct {
		typ          wasm.ValType
		unary, c, op byte
	}{
		{wasm.I32, 0x67, 0x41, 0x71}, {wasm.I64, 0x79, 0x42, 0x83},
		{wasm.I32, 0xc0, 0x41, 0x74}, {wasm.I64, 0xc2, 0x42, 0x86},
	} {
		m := mod1(t, []wasm.ValType{tc.typ}, []wasm.ValType{tc.typ}, []byte{0, 0x3f, 0, 0x1a, 0x20, 0, tc.unary, tc.c, 2, tc.op, 0x0b})
		m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
		cm, e := CompileModuleWith(m, CompileOptions{})
		if e != nil {
			t.Fatal(e)
		}
		if cm.CodeImage != nil {
			cm.CodeImage.Close()
		}
		if _, e = runArm64WrapperWithOptions(t, m, CompileOptions{}, 1); e != nil {
			t.Fatal(e)
		}
	}
}
