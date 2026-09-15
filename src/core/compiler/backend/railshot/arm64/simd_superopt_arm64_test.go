//go:build (linux || darwin) && arm64

package arm64

import (
	"math/bits"
	"strconv"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func simdI32x4RotateRightBodyArm64(v [16]byte, shift byte) []byte {
	body := []byte{0x01, 0x01, 0x7b} // one v128 local
	body = append(body, simdConst(v)...)
	body = append(body, 0x21, 0x00) // local.set 0
	body = append(body, 0x20, 0x00, 0x41, shift)
	body = append(body, simdOp(173)...) // i32x4.shr_u
	body = append(body, 0x20, 0x00, 0x41, 32-shift)
	body = append(body, simdOp(171)...) // i32x4.shl
	body = append(body, simdOp(80)...)  // v128.or
	return append(body, 0x0b)
}

func TestSIMDI32x4RotateRightSuperoptArm64(t *testing.T) {
	values := []uint32{0x01234567, 0x89abcdef, 0x80000001, 0xfedcba98}
	v := i32x4Bytes(int32(values[0]), int32(values[1]), int32(values[2]), int32(values[3]))
	for _, shift := range []byte{1, 7, 12, 31} {
		t.Run(strconv.Itoa(int(shift)), func(t *testing.T) {
			body := simdI32x4RotateRightBodyArm64(v, shift)
			m := mod1(t, nil, []wasm.ValType{wasm.V128}, body)
			on := compileWithStats(t, m, false).Funcs[0]
			if got := on.Peephole["simd-rotr-i32x4"]; got != 1 {
				t.Fatalf("simd-rotr-i32x4 = %d, want 1 (all: %v)", got, on.Peephole)
			}

			var off *CodegenStats
			func() {
				saved := simdSuperoptEnabled
				defer func() { simdSuperoptEnabled = saved }()
				simdSuperoptEnabled = false
				off = compileWithStats(t, m, false).Funcs[0]
			}()
			if got, want := off.CodeBytes-on.CodeBytes, 4; got != want {
				t.Fatalf("code reduction = %d bytes, want %d", got, want)
			}

			want := i32x4Bytes(
				int32(bits.RotateLeft32(values[0], -int(shift))),
				int32(bits.RotateLeft32(values[1], -int(shift))),
				int32(bits.RotateLeft32(values[2], -int(shift))),
				int32(bits.RotateLeft32(values[3], -int(shift))),
			)
			if got := runArm64V128(t, m); got != want {
				t.Fatalf("rotate-right %d = % x, want % x", shift, got, want)
			}
		})
	}
}

func TestSIMDShuffleRotate8CachedInPlaceArm64(t *testing.T) {
	values := []uint32{0x01234567, 0x89abcdef, 0x80000001, 0xfedcba98}
	v := i32x4Bytes(int32(values[0]), int32(values[1]), int32(values[2]), int32(values[3]))
	body := []byte{0x01, 0x01, 0x7b} // one v128 local
	body = append(body, simdConst(v)...)
	body = append(body, 0x21, 0x00) // local.set 0
	for range 2 {
		body = append(body, 0x20, 0x00, 0x20, 0x00) // local.get 0 twice
		body = append(body, simdOp(13)...)
		body = append(body, i8x16Rotate8[:]...)
		body = append(body, 0x21, 0x00) // local.set 0
	}
	body = append(body, 0x20, 0x00, 0x0b)
	m := mod1(t, nil, []wasm.ValType{wasm.V128}, body)
	stats := compileWithStats(t, m, false).Funcs[0]
	if got := stats.Peephole["simd-shuffle-rotr8-tbl-inplace"]; got != 2 {
		t.Fatalf("in-place rotate8 shuffles = %d, want 2 (all: %v)", got, stats.Peephole)
	}
	want := i32x4Bytes(
		int32(bits.RotateLeft32(values[0], -16)),
		int32(bits.RotateLeft32(values[1], -16)),
		int32(bits.RotateLeft32(values[2], -16)),
		int32(bits.RotateLeft32(values[3], -16)),
	)
	if got := runArm64V128(t, m); got != want {
		t.Fatalf("two rotate-right-8 shuffles = % x, want % x", got, want)
	}
	func() {
		saved := v128ConstCacheEnabled
		defer func() { v128ConstCacheEnabled = saved }()
		v128ConstCacheEnabled = false
		if got := compileWithStats(t, m, false).Funcs[0].Peephole["simd-shuffle-rotr8-tbl-inplace"]; got != 0 {
			t.Fatalf("disabled in-place rotate8 shuffles = %d, want 0", got)
		}
		if got := runArm64V128(t, m); got != want {
			t.Fatalf("uncached two rotate-right-8 shuffles = % x, want % x", got, want)
		}
	}()
}

func TestSIMDShuffleRotate16InPlaceArm64(t *testing.T) {
	values := []uint32{0x01234567, 0x89abcdef, 0x80000001, 0xfedcba98}
	v := i32x4Bytes(int32(values[0]), int32(values[1]), int32(values[2]), int32(values[3]))
	body := []byte{0x01, 0x01, 0x7b} // one v128 local
	body = append(body, simdConst(v)...)
	body = append(body, 0x21, 0x00)             // local.set 0
	body = append(body, 0x20, 0x00, 0x20, 0x00) // local.get 0 twice
	body = append(body, simdOp(13)...)
	body = append(body, i8x16Rotate16[:]...)
	body = append(body, 0x21, 0x00, 0x20, 0x00, 0x0b) // local.set 0; local.get 0; end
	m := mod1(t, nil, []wasm.ValType{wasm.V128}, body)
	stats := compileWithStats(t, m, false).Funcs[0]
	if got := stats.Peephole["simd-shuffle-rotr16-inplace"]; got != 1 {
		t.Fatalf("in-place rotate16 shuffles = %d, want 1 (all: %v)", got, stats.Peephole)
	}
	want := i32x4Bytes(
		int32(bits.RotateLeft32(values[0], -16)),
		int32(bits.RotateLeft32(values[1], -16)),
		int32(bits.RotateLeft32(values[2], -16)),
		int32(bits.RotateLeft32(values[3], -16)),
	)
	if got := runArm64V128(t, m); got != want {
		t.Fatalf("rotate-right-16 shuffle = % x, want % x", got, want)
	}

	other := []byte{0x01, 0x02, 0x7b} // two v128 locals
	other = append(other, simdConst(v)...)
	other = append(other, 0x21, 0x00)             // local.set 0
	other = append(other, 0x20, 0x00, 0x20, 0x00) // local.get 0 twice
	other = append(other, simdOp(13)...)
	other = append(other, i8x16Rotate16[:]...)
	other = append(other, 0x21, 0x01, 0x20, 0x01, 0x0b) // local.set 1; local.get 1; end
	otherModule := mod1(t, nil, []wasm.ValType{wasm.V128}, other)
	if got := compileWithStats(t, otherModule, false).Funcs[0].Peephole["simd-shuffle-rotr16-inplace"]; got != 0 {
		t.Fatalf("different-local rotate16 sink fired %d time(s), want 0", got)
	}
	if got := runArm64V128(t, otherModule); got != want {
		t.Fatalf("different-local rotate-right-16 shuffle = % x, want % x", got, want)
	}
}

func TestV128BinaryLocalSinkArm64(t *testing.T) {
	value := [16]byte{0xff, 0x0f, 0xf0, 0xaa, 1, 2, 3, 4, 0x55, 0xcc, 9, 8, 7, 6, 5, 4}
	mask := [16]byte{0x0f, 0xff, 0x0f, 0x55, 0xff, 0, 0xff, 0, 0xaa, 0x33, 0xff, 0, 0xff, 0, 0xff, 0}
	var want [16]byte
	for i := range want {
		want[i] = value[i] & mask[i]
	}
	for _, tc := range []struct {
		name string
		sink byte
		tail []byte
	}{
		{name: "set", sink: 0x21, tail: []byte{0x20, 0x00}},
		{name: "tee", sink: 0x22, tail: []byte{0x1a, 0x20, 0x00}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte{0x01, 0x01, 0x7b} // one v128 local
			body = append(body, simdConst(value)...)
			body = append(body, 0x21, 0x00, 0x20, 0x00) // local.set 0; local.get 0
			body = append(body, simdConst(mask)...)
			body = append(body, simdOp(78)...) // v128.and
			body = append(body, tc.sink, 0x00)
			body = append(body, tc.tail...)
			body = append(body, 0x0b)
			m := mod1(t, nil, []wasm.ValType{wasm.V128}, body)
			stats := compileWithStats(t, m, false).Funcs[0]
			if got := stats.Peephole["v128-local-sink"]; got != 1 {
				t.Fatalf("v128 local sinks = %d, want 1 (all: %v)", got, stats.Peephole)
			}
			if got := runArm64V128(t, m); got != want {
				t.Fatalf("v128 local sink = % x, want % x", got, want)
			}
		})
	}
}

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

func simdBitmaskNonZeroBodyArm64(v [16]byte, compare int32) []byte {
	body := []byte{0x00}
	body = append(body, simdConst(v)...)
	body = append(body, simdOp(100)...)
	body = append(body, 0x41, byte(compare), 0x47, 0x0b) // i32.const compare; i32.ne; end
	return body
}

func TestSIMDBitmaskNonZeroSuperoptArm64(t *testing.T) {
	for _, tc := range []struct {
		name string
		v    [16]byte
		want uint32
	}{
		{"none", i8x16Bytes(0, 1, 2, 127), 0},
		{"low-lane", i8x16Bytes(-1), 1},
		{"high-lane", i8x16Bytes(0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, -128), 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := simdBitmaskNonZeroBodyArm64(tc.v, 0)
			m := mod1(t, nil, []wasm.ValType{wasm.I32}, body)
			on := compileWithStats(t, m, false).Funcs[0]
			if got := on.Peephole["simd-bitmask-nonzero"]; got != 1 {
				t.Fatalf("simd-bitmask-nonzero = %d, want 1 (all: %v)", got, on.Peephole)
			}
			if got := runArm64I32(t, body); got != tc.want {
				t.Fatalf("bitmask != 0 = %d, want %d", got, tc.want)
			}

			var off *CodegenStats
			func() {
				saved := simdSuperoptEnabled
				defer func() { simdSuperoptEnabled = saved }()
				simdSuperoptEnabled = false
				off = compileWithStats(t, m, false).Funcs[0]
				if got := runArm64I32(t, body); got != tc.want {
					t.Fatalf("unfused bitmask != 0 = %d, want %d", got, tc.want)
				}
			}()
			if on.CodeBytes >= off.CodeBytes {
				t.Fatalf("fused code = %d bytes, unfused = %d; want smaller", on.CodeBytes, off.CodeBytes)
			}
		})
	}
}

func TestSIMDBitmaskNonZeroSuperoptRejectsOtherConstantArm64(t *testing.T) {
	body := simdBitmaskNonZeroBodyArm64(i8x16Bytes(-1), 1)
	m := mod1(t, nil, []wasm.ValType{wasm.I32}, body)
	if got := compileWithStats(t, m, false).Funcs[0].Peephole["simd-bitmask-nonzero"]; got != 0 {
		t.Fatalf("simd-bitmask-nonzero = %d, want 0 for comparison with one", got)
	}
	if got := runArm64I32(t, body); got != 0 { // bitmask is exactly one.
		t.Fatalf("bitmask != 1 = %d, want 0", got)
	}
}

func simdBitmaskPopcntBodyArm64(v [16]byte) []byte {
	body := []byte{0x00}
	body = append(body, simdConst(v)...)
	body = append(body, simdOp(100)...)
	return append(body, 0x69, 0x0b) // i32.popcnt; end
}

func TestSIMDBitmaskPopcntSuperoptArm64(t *testing.T) {
	for _, tc := range []struct {
		name string
		v    [16]byte
		want uint32
	}{
		{"none", i8x16Bytes(0, 1, 2, 127), 0},
		{"ends", i8x16Bytes(-1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, -128), 2},
		{"all", i8x16Bytes(-1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1), 16},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := simdBitmaskPopcntBodyArm64(tc.v)
			m := mod1(t, nil, []wasm.ValType{wasm.I32}, body)
			on := compileWithStats(t, m, false).Funcs[0]
			if got := on.Peephole["simd-bitmask-popcnt"]; got != 1 {
				t.Fatalf("simd-bitmask-popcnt = %d, want 1 (all: %v)", got, on.Peephole)
			}
			if got := runArm64I32(t, body); got != tc.want {
				t.Fatalf("popcnt(bitmask) = %d, want %d", got, tc.want)
			}

			var off *CodegenStats
			func() {
				saved := simdSuperoptEnabled
				defer func() { simdSuperoptEnabled = saved }()
				simdSuperoptEnabled = false
				off = compileWithStats(t, m, false).Funcs[0]
				if got := runArm64I32(t, body); got != tc.want {
					t.Fatalf("unfused popcnt(bitmask) = %d, want %d", got, tc.want)
				}
			}()
			if on.CodeBytes >= off.CodeBytes {
				t.Fatalf("fused code = %d bytes, unfused = %d; want smaller", on.CodeBytes, off.CodeBytes)
			}
		})
	}
}

func simdExtShuffleBodyArm64(a, b [16]byte, offset byte) []byte {
	body := []byte{0x00}
	body = append(body, simdConst(a)...)
	body = append(body, simdConst(b)...)
	body = append(body, simdOp(13)...)
	for lane := byte(0); lane < 16; lane++ {
		body = append(body, offset+lane)
	}
	return append(body, 0x0b)
}

func TestSIMDShuffleExtSelectorArm64(t *testing.T) {
	a := i8x16Bytes(0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15)
	b := i8x16Bytes(16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31)
	for offset := byte(1); offset < 16; offset++ {
		body := simdExtShuffleBodyArm64(a, b, offset)
		m := mod1(t, nil, []wasm.ValType{wasm.V128}, body)
		if got := compileWithStats(t, m, false).Funcs[0].Peephole["simd-shuffle-ext"]; got != 1 {
			t.Fatalf("offset %d: simd-shuffle-ext = %d, want 1", offset, got)
		}
		var want [16]byte
		for i := range want {
			want[i] = offset + byte(i)
		}
		if got := runArm64V128(t, m); got != want {
			t.Fatalf("offset %d: ext = %v, want %v", offset, got, want)
		}
	}
}

func TestSIMDShuffleExtSelectorRejectsNonContiguousArm64(t *testing.T) {
	body := simdExtShuffleBodyArm64(i8x16Bytes(), i8x16Bytes(), 7)
	body[len(body)-2] = 0 // Break the final lane of the shuffle immediate.
	m := mod1(t, nil, []wasm.ValType{wasm.V128}, body)
	if got := compileWithStats(t, m, false).Funcs[0].Peephole["simd-shuffle-ext"]; got != 0 {
		t.Fatalf("simd-shuffle-ext = %d, want 0 for non-contiguous shuffle", got)
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
