//go:build amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func copysignBits(a, b uint64, f64 bool) uint64 {
	if f64 {
		return a&fMagMask64 | b&fSignMask64
	}
	return uint64(uint32(a)&fMagMask32 | uint32(b)&fSignMask32)
}

func TestFCopysignXorMaskExec(t *testing.T) {
	shapes := []struct {
		name        string
		leftPrefix  []byte
		rightSuffix []byte
		negateSign  bool
	}{
		{name: "borrowed"},
		{name: "owned-left", leftPrefix: []byte{0x9a}},
		{name: "owned-right", rightSuffix: []byte{0x9a}, negateSign: true},
		{name: "both-owned", leftPrefix: []byte{0x9a}, rightSuffix: []byte{0x9a}, negateSign: true},
	}
	values64 := [][2]uint64{
		{0, fSignMask64},
		{fSignMask64, 0},
		{0x7ff8000000001234, fSignMask64},
		{0xfff0000000000000, 0x3ff0000000000000},
		{0x0123456789abcdef, 0xfedcba9876543210},
	}
	for _, shape := range shapes {
		t.Run("f64/"+shape.name, func(t *testing.T) {
			body := []byte{0x00, 0x20, 0x00}
			body = append(body, shape.leftPrefix...)
			body = append(body, 0x20, 0x01)
			body = append(body, shape.rightSuffix...)
			body = append(body, 0xa6, 0xbd, 0x0b) // f64.copysign; i64.reinterpret_f64
			m := mod1(t, []wasm.ValType{wasm.F64, wasm.F64}, []wasm.ValType{wasm.I64}, body)
			for _, values := range values64 {
				b := values[1]
				if shape.negateSign {
					b ^= fSignMask64
				}
				if got, want := runAmd64u(t, m, values[0], values[1]), copysignBits(values[0], b, true); got != want {
					t.Fatalf("copysign(%#x, %#x) = %#x, want %#x", values[0], values[1], got, want)
				}
			}
			stats := compileWithStats(t, m, false).Funcs[0]
			if got := stats.Peephole["fcopysign-xor-mask"]; got != 1 {
				t.Fatalf("fcopysign-xor-mask = %d, want 1 (all: %v)", got, stats.Peephole)
			}
			wantMaskPools := 1 + len(shape.leftPrefix) + len(shape.rightSuffix)
			if got := stats.Peephole["float-mask-const-pool"]; got != wantMaskPools {
				t.Fatalf("float-mask-const-pool = %d, want %d (all: %v)", got, wantMaskPools, stats.Peephole)
			}
			if shape.name == "borrowed" {
				var off ModuleStats
				if _, err := CompileModuleWith(m, CompileOptions{
					Optimizations: map[string]bool{"v128-const-cache": false},
					Stats:         &off,
				}); err != nil {
					t.Fatal(err)
				}
				if got := off.Funcs[0].Peephole["float-mask-const-pool"]; got != 0 {
					t.Fatalf("disabled float-mask-const-pool = %d, want 0", got)
				}
			}
		})
	}

	values32 := [][2]uint64{
		{0, uint64(fSignMask32)},
		{uint64(fSignMask32), 0},
		{0x7fc01234, uint64(fSignMask32)},
		{0xff800000, 0x3f800000},
		{0x01234567, 0xfedcba98},
	}
	for _, values := range values32 {
		body := []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x98, 0xbc, 0x0b} // f32.copysign; i32.reinterpret_f32
		m := mod1(t, []wasm.ValType{wasm.F32, wasm.F32}, []wasm.ValType{wasm.I32}, body)
		if got, want := runAmd64u(t, m, values[0], values[1]), copysignBits(values[0], values[1], false); uint32(got) != uint32(want) {
			t.Fatalf("f32.copysign(%#x, %#x) = %#x, want %#x", values[0], values[1], uint32(got), uint32(want))
		}
	}
}
