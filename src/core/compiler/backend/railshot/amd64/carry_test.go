//go:build amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func teeAddCarryBody(typ wasm.ValType, compare byte, operand byte, extend bool) []byte {
	add := byte(0x6a)
	if wasm.EqualValType(typ, wasm.I64) {
		add = 0x7c
	}
	body := []byte{
		0x01, 0x01, wasm.MustEncodeValType(typ), // one declared local: sum
		0x20, 0x00, 0x20, 0x01, add,
		0x22, 0x02, // local.tee sum
		0x20, operand, compare,
	}
	if extend {
		body = append(body, 0xad) // i64.extend_i32_u
	}
	return append(body, 0x0b)
}

func TestTeeAddCarry(t *testing.T) {
	tests := []struct {
		name string
		typ  wasm.ValType
	}{
		{"i32-extended", wasm.I32},
		{"i64-extended", wasm.I64},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			compare := byte(0x49)
			if wasm.EqualValType(tc.typ, wasm.I64) {
				compare = 0x54
			}
			m := mod1(t, []wasm.ValType{tc.typ, tc.typ}, []wasm.ValType{wasm.I64}, teeAddCarryBody(tc.typ, compare, 0, true))
			var stats ModuleStats
			on, err := CompileModuleWith(m, CompileOptions{Stats: &stats})
			if err != nil {
				t.Fatal(err)
			}
			off, err := CompileModuleWith(m, CompileOptions{Optimizations: map[string]bool{"tee-add-carry": false}})
			if err != nil {
				t.Fatal(err)
			}
			if got := stats.Funcs[0].Peephole["tee-add-carry"]; got != 1 {
				t.Fatalf("hits = %d, want 1 (all: %v)", got, stats.Funcs[0].Peephole)
			}
			if len(on.Code) >= len(off.Code) {
				t.Fatalf("code bytes enabled/disabled = %d/%d, want reduction", len(on.Code), len(off.Code))
			}
			values := [][2]uint64{{0, 0}, {1, 2}, {0xffffffff, 1}, {^uint64(0), 1}, {^uint64(0), ^uint64(0)}}
			for _, pair := range values {
				want := uint64(0)
				a, b := pair[0], pair[1]
				if wasm.EqualValType(tc.typ, wasm.I32) {
					a, b = uint64(uint32(a)), uint64(uint32(b))
					if uint32(a)+uint32(b) < uint32(a) {
						want = 1
					}
				} else if a+b < a {
					want = 1
				}
				if got := runCompiledAmd64u(t, on, pair[:]...); got != want {
					t.Fatalf("(%#x + %#x) carry = %d, want %d", pair[0], pair[1], got, want)
				}
			}
		})
	}
}

func TestTeeAddCarryNearMisses(t *testing.T) {
	tests := []struct {
		name    string
		compare byte
		operand byte
	}{
		{"signed", 0x48, 0},
		{"greater", 0x4b, 0},
		{"non-operand", 0x49, 2},
		{"not-extended", 0x49, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			extend := tc.name != "not-extended"
			body := teeAddCarryBody(wasm.I32, tc.compare, tc.operand, extend)
			params := []wasm.ValType{wasm.I32, wasm.I32}
			if tc.operand == 2 {
				params = append(params, wasm.I32)
				body[9] = 0x03 // local.tee index after three params.
			}
			result := wasm.I64
			if !extend {
				result = wasm.I32
			}
			m := mod1(t, params, []wasm.ValType{result}, body)
			var stats ModuleStats
			if _, err := CompileModuleWith(m, CompileOptions{Stats: &stats}); err != nil {
				t.Fatal(err)
			}
			if got := stats.Funcs[0].Peephole["tee-add-carry"]; got != 0 {
				t.Fatalf("hits = %d, want 0", got)
			}
		})
	}
}
