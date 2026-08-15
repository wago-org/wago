//go:build amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func threeWayUnsignedBody(typ wasm.ValType, reverse bool) []byte {
	gt, lt := byte(0x4b), byte(0x49)
	if wasm.EqualValType(typ, wasm.I64) {
		gt, lt = 0x56, 0x54
	}
	first, second := gt, lt
	if reverse {
		first, second = lt, gt
	}
	return []byte{
		0x00,
		0x20, 0x00, 0x20, 0x01, first,
		0x20, 0x00, 0x20, 0x01, second,
		0x6b, 0x0b,
	}
}

func TestThreeWayUnsignedCompare(t *testing.T) {
	for _, typ := range []wasm.ValType{wasm.I32, wasm.I64} {
		for _, reverse := range []bool{false, true} {
			m := mod1(t, []wasm.ValType{typ, typ}, []wasm.ValType{wasm.I32}, threeWayUnsignedBody(typ, reverse))
			var stats ModuleStats
			on, err := CompileModuleWith(m, CompileOptions{Stats: &stats})
			if err != nil {
				t.Fatal(err)
			}
			off, err := CompileModuleWith(m, CompileOptions{Optimizations: map[string]bool{"st-flags": false}})
			if err != nil {
				t.Fatal(err)
			}
			if got := stats.Funcs[0].Peephole["three-way-unsigned"]; got != 1 {
				t.Fatalf("%v reverse=%t hits = %d, want 1 (all: %v)", typ, reverse, got, stats.Funcs[0].Peephole)
			}
			if len(on.Code) >= len(off.Code) {
				t.Fatalf("%v reverse=%t code bytes enabled/disabled = %d/%d, want reduction", typ, reverse, len(on.Code), len(off.Code))
			}
			for _, pair := range [][2]uint64{{0, 0}, {0, 1}, {1, 0}, {0x7fffffff, 0x80000000}, {0xffffffff, 0}, {^uint64(0), 1}} {
				want := int32(0)
				a, b := pair[0], pair[1]
				if wasm.EqualValType(typ, wasm.I32) {
					a, b = uint64(uint32(a)), uint64(uint32(b))
				}
				if a < b {
					want = -1
				} else if a > b {
					want = 1
				}
				if reverse {
					want = -want
				}
				if got := int32(runCompiledAmd64u(t, on, pair[:]...)); got != want {
					t.Fatalf("%v reverse=%t (%#x, %#x) = %d, want %d", typ, reverse, pair[0], pair[1], got, want)
				}
			}
		}
	}
}

func TestThreeWayUnsignedCompareNearMisses(t *testing.T) {
	tests := []struct {
		name string
		body []byte
	}{
		{"signed", []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x4a, 0x20, 0x00, 0x20, 0x01, 0x48, 0x6b, 0x0b}},
		{"different operands", []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x4b, 0x20, 0x01, 0x20, 0x00, 0x49, 0x6b, 0x0b}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, tc.body)
			var stats ModuleStats
			if _, err := CompileModuleWith(m, CompileOptions{Stats: &stats}); err != nil {
				t.Fatal(err)
			}
			if got := stats.Funcs[0].Peephole["three-way-unsigned"]; got != 0 {
				t.Fatalf("hits = %d, want 0", got)
			}
		})
	}
}
