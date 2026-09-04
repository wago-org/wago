//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func i64Mask32Module(t *testing.T, mask uint64, maskFirst, tee bool) *wasm.Module {
	t.Helper()
	body := []byte{0x00}
	maskConst := append([]byte{0x42}, wasmtest.SLEB64(int64(mask))...)
	value := []byte{0x20, 0x00}
	if maskFirst {
		body = append(body, maskConst...)
		body = append(body, value...)
	} else {
		body = append(body, value...)
		body = append(body, maskConst...)
	}
	body = append(body, 0x83) // i64.and
	if tee {
		body = append(body, 0x22, 0x00) // local.tee 0: exercise target sinking
	}
	body = append(body, 0x0b)
	return mod1(t, []wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}, body)
}

func setI64Mask32(t testing.TB, enabled bool) {
	t.Helper()
	if !SetOptKnob("i64-mask32", enabled) {
		t.Fatal("i64-mask32 is not registered")
	}
}

func TestI64Mask32Lowering(t *testing.T) {
	saved := i64Mask32Enabled
	defer SetOptKnob("i64-mask32", saved)

	for _, tc := range []struct {
		name      string
		mask      uint64
		maskFirst bool
		tee       bool
	}{
		{name: "low-31", mask: 0x7fffffff},
		{name: "high-bit", mask: 0x80000000},
		{name: "mixed", mask: 0x00ff00ff},
		{name: "full-low-32", mask: 0xffffffff},
		{name: "constant-left", mask: 0x80000000, maskFirst: true},
		{name: "local-tee", mask: 0x00ff00ff, tee: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := i64Mask32Module(t, tc.mask, tc.maskFirst, tc.tee)
			setI64Mask32(t, true)
			on := compileWithStats(t, m, false).Funcs[0]
			if got := on.Peephole["i64-mask32"]; got != 1 {
				t.Fatalf("i64-mask32 = %d, want 1 (all: %v)", got, on.Peephole)
			}

			setI64Mask32(t, false)
			off := compileWithStats(t, m, false).Funcs[0]
			if got := off.Peephole["i64-mask32"]; got != 0 {
				t.Fatalf("disabled i64-mask32 = %d, want 0", got)
			}
			if !tc.tee && on.CodeBytes >= off.CodeBytes {
				t.Fatalf("enabled code = %d bytes, disabled = %d; want smaller", on.CodeBytes, off.CodeBytes)
			}

			for _, x := range []uint64{
				0, 1, 0x7fffffff, 0x80000000, 0xffffffff,
				0x1_0000_0000, 0xdead_beef_cafe_babe, 0xffffffffffffffff,
			} {
				want := x & tc.mask
				setI64Mask32(t, true)
				if got := runAmd64u(t, m, x); got != want {
					t.Fatalf("enabled f(%#x) = %#x, want %#x", x, got, want)
				}
				setI64Mask32(t, false)
				if got := runAmd64u(t, m, x); got != want {
					t.Fatalf("disabled f(%#x) = %#x, want %#x", x, got, want)
				}
			}
		})
	}
}
