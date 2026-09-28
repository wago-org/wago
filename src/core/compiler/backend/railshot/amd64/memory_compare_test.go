//go:build linux && amd64

package amd64

import (
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func compareBitsForTest(a, b uint64, width int, cc Cond) bool {
	shift := 64 - width
	a, b = a<<shift>>shift, b<<shift>>shift
	sa, sb := int64(a<<shift)>>shift, int64(b<<shift)>>shift
	switch cc {
	case condE:
		return a == b
	case condNE:
		return a != b
	case condB:
		return a < b
	case condBE:
		return a <= b
	case condA:
		return a > b
	case condAE:
		return a >= b
	case condL:
		return sa < sb
	case condLE:
		return sa <= sb
	case condG:
		return sa > sb
	case condGE:
		return sa >= sb
	}
	panic("unsupported condition")
}

func TestMemoryCompareByteExtensionExhaustive(t *testing.T) {
	for _, wide := range []bool{false, true} {
		width := 32
		if wide {
			width = 64
		}
		for _, signed := range []bool{false, true} {
			st := memRefStorage(R12, 0, 1, signed, wide, 0)
			for c := 0; c < 256; c++ {
				constant := int64(c)
				if signed {
					constant = int64(int8(c))
				}
				for _, cc := range []Cond{condE, condNE, condB, condBE, condA, condAE, condL, condLE, condG, condGE} {
					imm, nativeCC, ok := memoryCompareImmediate(st, constant, wide, cc)
					if !ok {
						t.Fatalf("representable constant rejected: signed=%v c=%d", signed, constant)
					}
					for v := 0; v < 256; v++ {
						extended := int64(v)
						if signed {
							extended = int64(int8(v))
						}
						want := compareBitsForTest(uint64(extended), uint64(constant), width, cc)
						got := compareBitsForTest(uint64(v), uint64(uint32(imm)), 8, nativeCC)
						if got != want {
							t.Fatalf("wide=%v signed=%v v=%d c=%d cc=%d", wide, signed, v, constant, cc)
						}
					}
				}
			}
		}
	}
}

func TestMemoryCompareNative(t *testing.T) {
	for _, tc := range []struct {
		load, compare byte
		wide          bool
		size          int
		constant      int64
		value         uint64
		want, admit   bool
	}{
		{0x2d, 0x48, false, 1, 128, 255, false, true}, // unsigned load, signed compare
		{0x2c, 0x48, false, 1, 0, 255, true, true},
		{0x2c, 0x49, false, 1, -1, 128, true, true},
		{0x2c, 0x48, false, 1, 128, 255, true, false},
		{0x2d, 0x46, false, 1, 256, 0, false, false},
		{0x2f, 0x4e, false, 2, 32768, 65535, true, true},
		{0x2e, 0x46, false, 2, -1, 65535, true, true},
		{0x28, 0x48, false, 4, 0, 0x80000000, true, true},
		{0x35, 0x55, true, 4, 0, 0xffffffff, true, true},
		{0x35, 0x53, true, 4, 1 << 31, 0xffffffff, false, true},
		{0x33, 0x53, true, 2, 32768, 65535, false, true},
		{0x32, 0x54, true, 2, -1, 32768, true, true},
		{0x31, 0x53, true, 1, 128, 255, false, true},
		{0x34, 0x51, true, 4, -1, 0xffffffff, true, true},
		{0x34, 0x51, true, 4, 0xffffffff, 0xffffffff, false, false},
		{0x29, 0x51, true, 8, -1, ^uint64(0), true, true},
		{0x29, 0x51, true, 8, 1 << 40, 1 << 40, true, false},
		{0x2d, 0x45, false, 1, 0, 0, true, true},
		{0x2f, 0x45, false, 2, 0, 1, false, true},
		{0x28, 0x45, false, 4, 0, 0, true, true},
		{0x29, 0x50, true, 8, 0, 0, true, true},
	} {
		for mode := 0; mode < 3; mode++ { // value, fused branch, inverted fused branch
			body := []byte{0, 0x20, 0, tc.load, 0, 3}
			if tc.compare != 0x45 && tc.compare != 0x50 {
				if tc.wide {
					body = append(body, 0x42)
					body = append(body, wasmtest.SLEB64(tc.constant)...)
				} else {
					body = append(body, 0x41)
					body = append(body, wasmtest.SLEB32(int32(tc.constant))...)
				}
			}
			body = append(body, tc.compare)
			if mode == 2 {
				body = append(body, 0x45)
			}
			if mode != 0 {
				body = append(body, 0x04, 0x7f, 0x41, 1, 0x05, 0x41, 0, 0x0b)
			}
			// Address local remains live after the result is produced.
			body = append(body, 0x20, 0, 0x73, 0x0b)
			m := modMem(t, 1, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
			for _, guard := range []bool{false, true} {
				for _, on := range []bool{false, true} {
					t.Run(fmt.Sprintf("load=%x/cmp=%x/c=%d/mode=%d/guard=%v/on=%v", tc.load, tc.compare, tc.constant, mode, guard, on), func(t *testing.T) {
						var stats ModuleStats
						got, _, err := runMemAmd64WithOptions(t, m, CompileOptions{Stats: &stats, ElideBoundsChecks: guard, CompactNative: true, Optimizations: map[string]bool{"memory-compare-immediate": on}}, func(mem []byte) {
							for i := range mem {
								mem[i] = 0xa5
							}
							var raw [8]byte
							binary.LittleEndian.PutUint64(raw[:], tc.value)
							copy(mem[12:12+tc.size], raw[:tc.size])
						}, 9)
						want := uint64(0)
						if tc.want != (mode == 2) {
							want = 1
						}
						if err != nil || got != want^9 {
							t.Fatalf("got=%d want=%d err=%v", got, want^9, err)
						}
						if (stats.Funcs[0].Peephole["memory-compare-immediate"] > 0) != (on && tc.admit) {
							t.Fatalf("wrong admission: %v", stats.Funcs[0].Peephole)
						}
					})
				}
			}
		}
	}
}

func TestMemoryComparePreservesAccessBoundary(t *testing.T) {
	for _, tc := range []struct {
		load, eqz byte
		size      int
	}{{0x2d, 0x45, 1}, {0x2f, 0x45, 2}, {0x28, 0x45, 4}, {0x29, 0x50, 8}} {
		m := modMem(t, 1, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, []byte{
			0, 0x20, 0, 0x41, 1, 0x6a, tc.load, 0, 3, tc.eqz, 0x0b,
		})
		for _, on := range []bool{false, true} {
			for _, addr := range []uint64{uint64(65536 - tc.size - 4), uint64(65536 - tc.size - 3), 0xffffffff} {
				invalid := addr == uint64(65536-tc.size-3)
				for _, guard := range []bool{false, true} {
					if guard && invalid {
						continue // direct execution fixture has no signal trap handler
					}
					got, _, err := runMemAmd64WithOptions(t, m, CompileOptions{ElideBoundsChecks: guard, CompactNative: true, Optimizations: map[string]bool{"memory-compare-immediate": on}}, nil, addr)
					if (err != nil) != invalid || !invalid && got != 1 {
						t.Fatalf("size=%d on=%v guard=%v addr=%x result=%d err=%v", tc.size, on, guard, addr, got, err)
					}
				}
			}
		}
	}
}
