//go:build amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func widenedCarryArithmeticBody(op byte, carryLeft bool, compare byte) []byte {
	carry := []byte{0x20, 0x01, 0x20, 0x02, compare, 0xad}
	body := []byte{0x00}
	if carryLeft {
		body = append(body, carry...)
		body = append(body, 0x20, 0x00)
	} else {
		body = append(body, 0x20, 0x00)
		body = append(body, carry...)
	}
	return append(body, op, 0x0b)
}

func TestWidenedCarryArithmetic(t *testing.T) {
	tests := []struct {
		name      string
		op        byte
		carryLeft bool
		compare   byte
	}{
		{"add-right-lt", 0x7c, false, 0x54},
		{"add-left-lt", 0x7c, true, 0x54},
		{"sub-right-lt", 0x7d, false, 0x54},
		{"add-right-gt", 0x7c, false, 0x56},
		{"sub-right-gt", 0x7d, false, 0x56},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := mod1(t, []wasm.ValType{wasm.I64, wasm.I64, wasm.I64}, []wasm.ValType{wasm.I64}, widenedCarryArithmeticBody(tc.op, tc.carryLeft, tc.compare))
			var stats ModuleStats
			on, err := CompileModuleWith(m, CompileOptions{Stats: &stats})
			if err != nil {
				t.Fatal(err)
			}
			off, err := CompileModuleWith(m, CompileOptions{Optimizations: map[string]bool{"widened-carry-arith": false}})
			if err != nil {
				t.Fatal(err)
			}
			if got := stats.Funcs[0].Peephole["widened-carry-arith"]; got != 1 {
				t.Fatalf("hits = %d, want 1 (all: %v)", got, stats.Funcs[0].Peephole)
			}
			if len(on.Code) >= len(off.Code) {
				t.Fatalf("code bytes enabled/disabled = %d/%d, want reduction", len(on.Code), len(off.Code))
			}
			for _, args := range [][3]uint64{{10, 1, 2}, {10, 2, 1}, {0, 0, ^uint64(0)}, {^uint64(0), 0, 1}} {
				carry := uint64(0)
				if tc.compare == 0x54 && args[1] < args[2] || tc.compare == 0x56 && args[1] > args[2] {
					carry = 1
				}
				want := args[0] + carry
				if tc.op == 0x7d {
					want = args[0] - carry
				}
				if got := runCompiledAmd64u(t, on, args[:]...); got != want {
					t.Fatalf("args=%#x result=%#x, want %#x", args, got, want)
				}
			}
		})
	}
}

func TestWidenedCarryArithmeticNearMisses(t *testing.T) {
	tests := []struct {
		name string
		body []byte
	}{
		{"unsigned-less-equal", widenedCarryArithmeticBody(0x7c, false, 0x58)},
		{"signed-less", widenedCarryArithmeticBody(0x7c, false, 0x53)},
		{"carry-left-sub", widenedCarryArithmeticBody(0x7d, true, 0x54)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := mod1(t, []wasm.ValType{wasm.I64, wasm.I64, wasm.I64}, []wasm.ValType{wasm.I64}, tc.body)
			var stats ModuleStats
			if _, err := CompileModuleWith(m, CompileOptions{Stats: &stats}); err != nil {
				t.Fatal(err)
			}
			if got := stats.Funcs[0].Peephole["widened-carry-arith"]; got != 0 {
				t.Fatalf("hits = %d, want 0", got)
			}
		})
	}
}
