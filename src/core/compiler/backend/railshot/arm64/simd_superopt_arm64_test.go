//go:build (linux || darwin) && arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func simdAndAnyTrueBodyArm64(a, b [16]byte) []byte {
	body := []byte{0x00}
	body = append(body, simdConst(a)...)
	body = append(body, simdConst(b)...)
	body = append(body, simdOp(78)...)
	body = append(body, simdOp(83)...)
	return append(body, 0x0b)
}

func TestSIMDAndAnyTrueSuperoptArm64(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	for _, tc := range []struct {
		name string
		a, b [16]byte
		want uint32
	}{
		{"zero", i8x16Bytes(1, 2, 4, 8), i8x16Bytes(16, 32, 64, -128), 0},
		{"low-bit", i8x16Bytes(3), i8x16Bytes(1), 1},
		{"high-lane", i8x16Bytes(0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, -128), i8x16Bytes(0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, -1), 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := simdAndAnyTrueBodyArm64(tc.a, tc.b)
			m := mod1(t, nil, i32, body)
			on := compileWithStats(t, m, false).Funcs[0]
			if got := on.Peephole["simd-and-anytrue"]; got != 1 {
				t.Fatalf("simd-and-anytrue = %d, want 1 (all: %v)", got, on.Peephole)
			}
			if got := runArm64I32(t, body); got != tc.want {
				t.Fatalf("and-anytrue = %d, want %d", got, tc.want)
			}
			func() {
				saved := simdSuperoptEnabled
				defer func() { simdSuperoptEnabled = saved }()
				simdSuperoptEnabled = false
				if got := runArm64I32(t, body); got != tc.want {
					t.Fatalf("scalar and-anytrue = %d, want %d", got, tc.want)
				}
			}()
			t.Logf("and-anytrue code: %d bytes", on.CodeBytes)
		})
	}
}

func TestSIMDAndAnyTrueSuperoptRejectsNonAdjacentArm64(t *testing.T) {
	body := simdAndAnyTrueBodyArm64(i8x16Bytes(1), i8x16Bytes(1))
	andEnd := len(body) - len(simdOp(83)) - 1
	body = append(body[:andEnd], append(simdOp(77), body[andEnd:]...)...)
	m := mod1(t, nil, []wasm.ValType{wasm.I32}, body)
	if got := compileWithStats(t, m, false).Funcs[0].Peephole["simd-and-anytrue"]; got != 0 {
		t.Fatalf("simd-and-anytrue = %d, want 0 for non-adjacent sequence", got)
	}
	if got := runArm64I32(t, body); got != 1 {
		t.Fatalf("not(and) any_true = %d, want 1", got)
	}
}

func simdNotAndBodyArm64(a, b [16]byte) []byte {
	body := []byte{0x00}
	body = append(body, simdConst(a)...)
	body = append(body, simdConst(b)...)
	body = append(body, simdOp(77)...)
	body = append(body, simdOp(78)...)
	return append(body, 0x0b)
}

func TestSIMDNotAndSuperoptArm64(t *testing.T) {
	a := i8x16Bytes(0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15)
	b := i8x16Bytes(-1, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14)
	body := simdNotAndBodyArm64(a, b)
	m := mod1(t, nil, []wasm.ValType{wasm.V128}, body)
	on := compileWithStats(t, m, false).Funcs[0]
	if got := on.Peephole["simd-not-and"]; got != 1 {
		t.Fatalf("simd-not-and = %d, want 1 (all: %v)", got, on.Peephole)
	}
	var off *CodegenStats
	func() {
		saved := simdSuperoptEnabled
		defer func() { simdSuperoptEnabled = saved }()
		simdSuperoptEnabled = false
		off = compileWithStats(t, m, false).Funcs[0]
	}()
	if on.CodeBytes >= off.CodeBytes {
		t.Fatalf("fused code = %d bytes, scalar = %d; want smaller", on.CodeBytes, off.CodeBytes)
	}
	t.Logf("not-and code: %d -> %d bytes", off.CodeBytes, on.CodeBytes)
	var want [16]byte
	for i := range want {
		want[i] = a[i] &^ b[i]
	}
	if got := runArm64V128(t, m); got != want {
		t.Fatalf("not-and = % x, want % x", got, want)
	}
}
