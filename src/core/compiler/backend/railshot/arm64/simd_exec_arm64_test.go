//go:build (linux || darwin) && arm64

package arm64

import (
	"encoding/binary"
	"math"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/arm64spike"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func simdConst(v [16]byte) []byte {
	out := []byte{0xfd, 0x0c}
	return append(out, v[:]...)
}

func simdOp(sub uint32) []byte {
	return append([]byte{0xfd}, wasmtest.ULEB(sub)...)
}

func i8x16Bytes(v ...int8) [16]byte {
	var out [16]byte
	for i, x := range v {
		out[i] = byte(x)
	}
	return out
}

func i16x8Bytes(v ...int16) [16]byte {
	var out [16]byte
	for i, x := range v {
		binary.LittleEndian.PutUint16(out[i*2:], uint16(x))
	}
	return out
}

func i32x4Bytes(v ...int32) [16]byte {
	var out [16]byte
	for i, x := range v {
		binary.LittleEndian.PutUint32(out[i*4:], uint32(x))
	}
	return out
}

func i64x2Bytes(v ...int64) [16]byte {
	var out [16]byte
	for i, x := range v {
		binary.LittleEndian.PutUint64(out[i*8:], uint64(x))
	}
	return out
}

func f32x4Bits(v ...uint32) [16]byte {
	var out [16]byte
	for i, x := range v {
		binary.LittleEndian.PutUint32(out[i*4:], x)
	}
	return out
}

func f64x2Bits(v ...uint64) [16]byte {
	var out [16]byte
	for i, x := range v {
		binary.LittleEndian.PutUint64(out[i*8:], x)
	}
	return out
}

func f32x4Bytes(v ...float32) [16]byte {
	var out [16]byte
	for i, x := range v {
		binary.LittleEndian.PutUint32(out[i*4:], math.Float32bits(x))
	}
	return out
}

func f64x2Bytes(v ...float64) [16]byte {
	var out [16]byte
	for i, x := range v {
		binary.LittleEndian.PutUint64(out[i*8:], math.Float64bits(x))
	}
	return out
}

func runArm64I32(t *testing.T, body []byte) uint32 {
	t.Helper()
	m := mod1(t, nil, []wasm.ValType{wasm.I32}, body)
	cm, err := CompileModule(m)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	mem, err := arm64spike.MapExec(cm.Code)
	if err != nil {
		t.Fatalf("map: %v", err)
	}
	entry := uintptr(unsafe.Pointer(&mem[cm.InternalEntry[0]]))
	return uint32(arm64spike.Call2(entry, 0, 0))
}

func runArm64Result(t *testing.T, m *wasm.Module, n int) []byte {
	t.Helper()
	cm, err := CompileModule(m)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	eng, err := coreruntime.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	jm, err := coreruntime.NewJobMemory(65536)
	if err != nil {
		t.Fatal(err)
	}
	defer jm.Close()
	ar, err := coreruntime.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	defer ar.Close()
	mem, entry, err := coreruntime.MapCode(cm.Code)
	if err != nil {
		t.Fatalf("map: %v", err)
	}
	defer coreruntime.Unmap(mem)

	serArgs := ar.Alloc(256)
	results := ar.Alloc(256)
	trap := ar.Alloc(8)
	if err := eng.Call(entry+uintptr(cm.Entry[0]), serArgs, jm.LinearMemory(), trap, results); err != nil {
		t.Fatalf("call: %v", err)
	}
	out := make([]byte, n)
	copy(out, results[:n])
	return out
}

func runArm64V128(t *testing.T, m *wasm.Module) [16]byte {
	t.Helper()
	var out [16]byte
	copy(out[:], runArm64Result(t, m, 16))
	return out
}

func simdI32Body(v [16]byte, op uint32) []byte {
	body := []byte{0x00}
	body = append(body, simdConst(v)...)
	body = append(body, simdOp(op)...)
	body = append(body, 0x0b)
	return body
}

func f32ConstSplatBody(bits uint32) []byte {
	body := []byte{0x00, 0x43}
	var raw [4]byte
	binary.LittleEndian.PutUint32(raw[:], bits)
	body = append(body, raw[:]...)
	body = append(body, simdOp(19)...)
	body = append(body, 0x0b)
	return body
}

func f32ExtractLaneBody(v [16]byte, lane byte) []byte {
	body := []byte{0x00}
	body = append(body, simdConst(v)...)
	body = append(body, simdOp(31)...)
	body = append(body, lane, 0x0b)
	return body
}

func f64ExtractLaneBody(v [16]byte, lane byte) []byte {
	body := []byte{0x00}
	body = append(body, simdConst(v)...)
	body = append(body, simdOp(33)...)
	body = append(body, lane, 0x0b)
	return body
}

func v128BinaryBody(a, b [16]byte, sub uint32) []byte {
	body := []byte{0x00}
	body = append(body, simdConst(a)...)
	body = append(body, simdConst(b)...)
	body = append(body, simdOp(sub)...)
	body = append(body, 0x0b)
	return body
}

func simdBinaryCompareMaskBody(a, b, want [16]byte, op, cmpOp, bitmaskOp uint32) []byte {
	body := []byte{0x00}
	body = append(body, simdConst(a)...)
	body = append(body, simdConst(b)...)
	body = append(body, simdOp(op)...)
	body = append(body, simdConst(want)...)
	body = append(body, simdOp(cmpOp)...)
	body = append(body, simdOp(bitmaskOp)...)
	body = append(body, 0x0b)
	return body
}

func simdShiftCompareMaskBody(v, want [16]byte, count int32, op, cmpOp, bitmaskOp uint32) []byte {
	body := []byte{0x00}
	body = append(body, simdConst(v)...)
	body = append(body, 0x41)
	body = append(body, wasmtest.SLEB32(count)...)
	body = append(body, simdOp(op)...)
	body = append(body, simdConst(want)...)
	body = append(body, simdOp(cmpOp)...)
	body = append(body, simdOp(bitmaskOp)...)
	body = append(body, 0x0b)
	return body
}

func simdBinaryOpBitmaskBody(a, b [16]byte, op, bitmaskOp uint32) []byte {
	body := []byte{0x00}
	body = append(body, simdConst(a)...)
	body = append(body, simdConst(b)...)
	body = append(body, simdOp(op)...)
	body = append(body, simdOp(bitmaskOp)...)
	body = append(body, 0x0b)
	return body
}

func cmpMask(lanes int, pred func(int) bool) uint32 {
	var out uint32
	for lane := 0; lane < lanes; lane++ {
		if pred(lane) {
			out |= 1 << lane
		}
	}
	return out
}

func cmpU8Mask(a, b [16]byte, pred func(x, y uint8) bool) uint32 {
	return cmpMask(16, func(lane int) bool { return pred(a[lane], b[lane]) })
}

func cmpU16Mask(a, b [16]byte, pred func(x, y uint16) bool) uint32 {
	return cmpMask(8, func(lane int) bool {
		return pred(binary.LittleEndian.Uint16(a[lane*2:]), binary.LittleEndian.Uint16(b[lane*2:]))
	})
}

func cmpU32Mask(a, b [16]byte, pred func(x, y uint32) bool) uint32 {
	return cmpMask(4, func(lane int) bool {
		return pred(binary.LittleEndian.Uint32(a[lane*4:]), binary.LittleEndian.Uint32(b[lane*4:]))
	})
}

func cmpI64Mask(a, b [16]byte, pred func(x, y int64) bool) uint32 {
	return cmpMask(2, func(lane int) bool {
		x := int64(binary.LittleEndian.Uint64(a[lane*8:]))
		y := int64(binary.LittleEndian.Uint64(b[lane*8:]))
		return pred(x, y)
	})
}

func simdTernaryBody(a, b, c [16]byte, op uint32) []byte {
	body := []byte{0x00}
	body = append(body, simdConst(a)...)
	body = append(body, simdConst(b)...)
	body = append(body, simdConst(c)...)
	body = append(body, simdOp(op)...)
	body = append(body, 0x0b)
	return body
}

func simdShuffleCompareMaskBody(a, b, want [16]byte, lanes [16]byte) []byte {
	body := []byte{0x00}
	body = append(body, simdConst(a)...)
	body = append(body, simdConst(b)...)
	body = append(body, simdOp(13)...)
	body = append(body, lanes[:]...)
	body = append(body, simdConst(want)...)
	body = append(body, simdOp(35)...)
	body = append(body, simdOp(100)...)
	body = append(body, 0x0b)
	return body
}

func TestSIMDMovemaskBitmaskExec(t *testing.T) {
	cases := []struct {
		name string
		vec  [16]byte
		op   uint32
		want uint32
	}{
		{
			name: "i8x16.bitmask all byte lanes",
			vec:  i8x16Bytes(-1, 0, -2, 3, 4, -5, 6, 7, -8, 9, 10, 11, -12, 13, -14, 15),
			op:   100,
			want: 0x5125,
		},
		{
			name: "i16x8.bitmask lane signs",
			vec:  i16x8Bytes(-1, 2, -3, 4, 5, -6, 7, -8),
			op:   132,
			want: 0xa5,
		},
		{
			name: "i32x4.bitmask lane signs",
			vec:  i32x4Bytes(-1, 2, -3, 4),
			op:   164,
			want: 0x5,
		},
		{
			name: "i64x2.bitmask lane signs",
			vec:  i64x2Bytes(1, -2),
			op:   196,
			want: 0x2,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := runArm64I32(t, simdI32Body(tc.vec, tc.op)); got != tc.want {
				t.Fatalf("result = %#x, want %#x", got, tc.want)
			}
		})
	}
}

func TestSIMDArm64NativeUnaryExec(t *testing.T) {
	negZero32 := float32(math.Copysign(0, -1))
	negZero64 := math.Copysign(0, -1)
	cases := []struct {
		name string
		vec  [16]byte
		op   uint32
		want [16]byte
	}{
		{
			name: "i8x16.popcnt",
			vec:  i8x16Bytes(0x00, 0x01, 0x03, 0x07, 0x0f, 0x10, 0x55, 0x7f, -128, -127, -86, -1, 0x24, 0x42, 0x66, 0x7e),
			op:   98,
			want: i8x16Bytes(0, 1, 2, 3, 4, 1, 4, 7, 1, 2, 4, 8, 2, 2, 4, 6),
		},
		{
			name: "i64x2.abs",
			vec:  i64x2Bytes(-9223372036854775808, -1234567890123),
			op:   192,
			want: i64x2Bytes(-9223372036854775808, 1234567890123),
		},
		{"f32x4.abs", f32x4Bytes(negZero32, -4, 9, -16), 224, f32x4Bytes(0, 4, 9, 16)},
		{"f32x4.neg", f32x4Bytes(0, -4, 9, negZero32), 225, f32x4Bytes(negZero32, 4, -9, 0)},
		{"f64x2.abs", f64x2Bytes(negZero64, -9), 236, f64x2Bytes(0, 9)},
		{"f64x2.neg", f64x2Bytes(0, -9), 237, f64x2Bytes(negZero64, 9)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := mod1(t, nil, []wasm.ValType{wasm.V128}, simdI32Body(tc.vec, tc.op))
			if got := runArm64V128(t, m); got != tc.want {
				t.Fatalf("%s = % x, want % x", tc.name, got, tc.want)
			}
		})
	}
}

func TestSIMDFloatSplatExtractExec(t *testing.T) {
	splatBits := uint32(0x4048f5c3)
	m := mod1(t, nil, []wasm.ValType{wasm.V128}, f32ConstSplatBody(splatBits))
	if got, want := runArm64V128(t, m), f32x4Bits(splatBits, splatBits, splatBits, splatBits); got != want {
		t.Fatalf("f32x4.splat = % x, want % x", got, want)
	}

	f32Vec := f32x4Bits(0, 0, 0, 0xfe967699)
	m = mod1(t, nil, []wasm.ValType{wasm.F32}, f32ExtractLaneBody(f32Vec, 3))
	got32 := binary.LittleEndian.Uint32(runArm64Result(t, m, 4))
	if got32 != 0xfe967699 {
		t.Fatalf("f32x4.extract_lane = %#x, want %#x", got32, uint32(0xfe967699))
	}

	f64Vec := f64x2Bits(0, 0xfff0000000000001)
	m = mod1(t, nil, []wasm.ValType{wasm.F64}, f64ExtractLaneBody(f64Vec, 1))
	got64 := binary.LittleEndian.Uint64(runArm64Result(t, m, 8))
	if got64 != 0xfff0000000000001 {
		t.Fatalf("f64x2.extract_lane = %#x, want %#x", got64, uint64(0xfff0000000000001))
	}
}

func TestSIMDExtaddPairwiseExec(t *testing.T) {
	cases := []struct {
		name string
		vec  [16]byte
		op   uint32
		want [16]byte
	}{
		{
			name: "i16x8.extadd_pairwise_i8x16_s",
			vec:  i8x16Bytes(1, -2, 127, 1, -128, -1, 100, -100, 0, 0, 5, 6, -7, -8, -1, -1),
			op:   124,
			want: i16x8Bytes(-1, 128, -129, 0, 0, 11, -15, -2),
		},
		{
			name: "i16x8.extadd_pairwise_i8x16_u",
			vec:  i8x16Bytes(1, -1, 127, 1, -128, -1, 100, -100, 0, 0, 5, 6, -7, -8, -1, -1),
			op:   125,
			want: i16x8Bytes(256, 128, 383, 256, 0, 11, 497, 510),
		},
		{
			name: "i32x4.extadd_pairwise_i16x8_s",
			vec:  i16x8Bytes(1, -2, 32767, 1, -32768, -1, 100, -100),
			op:   126,
			want: i32x4Bytes(-1, 32768, -32769, 0),
		},
		{
			name: "i32x4.extadd_pairwise_i16x8_u",
			vec:  i16x8Bytes(1, -1, 32767, 1, -32768, -1, 100, -100),
			op:   127,
			want: i32x4Bytes(65536, 32768, 98303, 65536),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := mod1(t, nil, []wasm.ValType{wasm.V128}, simdI32Body(tc.vec, tc.op))
			if got := runArm64V128(t, m); got != tc.want {
				t.Fatalf("%s = % x, want % x", tc.name, got, tc.want)
			}
		})
	}
}

func TestSIMDQ15MulrAndDotExec(t *testing.T) {
	cases := []struct {
		name string
		a    [16]byte
		b    [16]byte
		op   uint32
		want [16]byte
	}{
		{
			name: "i16x8.q15mulr_sat_s",
			a:    i16x8Bytes(32767, -32768, -32768, 16384, -16384, 12345, -12345, 30000),
			b:    i16x8Bytes(32767, -32768, 32767, 16384, 16384, -23456, -23456, 2),
			op:   130,
			want: i16x8Bytes(32766, 32767, -32767, 8192, -8192, -8837, 8837, 2),
		},
		{
			name: "i32x4.dot_i16x8_s",
			a:    i16x8Bytes(300, -20, -32768, -32768, 1234, -30000, -1, 32767),
			b:    i16x8Bytes(-7, -400, -32768, -32768, -5, 2, -32768, 2),
			op:   186,
			want: i32x4Bytes(5900, math.MinInt32, -66170, 98302),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte{0x00}
			body = append(body, simdConst(tc.a)...)
			body = append(body, simdConst(tc.b)...)
			body = append(body, simdOp(tc.op)...)
			body = append(body, 0x0b)
			m := mod1(t, nil, []wasm.ValType{wasm.V128}, body)
			if got := runArm64V128(t, m); got != tc.want {
				t.Fatalf("%s = % x, want % x", tc.name, got, tc.want)
			}
		})
	}
}

func TestSIMDRelaxedDotExec(t *testing.T) {
	a := i8x16Bytes(-128, -128, 127, 127, 1, 2, -3, 4, 10, -20, 30, -40, 50, -60, 70, -80)
	b := i8x16Bytes(-128, -128, 127, 127, 5, 6, 7, 8, -1, -2, -3, -4, -5, -6, -7, -8)

	t.Run("i16x8.relaxed_dot_i8x16_i7x16_s", func(t *testing.T) {
		body := v128BinaryBody(a, b, 274)
		m := mod1(t, nil, []wasm.ValType{wasm.V128}, body)
		want := i16x8Bytes(32767, 32258, 17, 11, 30, 70, 110, 150)
		if got := runArm64V128(t, m); got != want {
			t.Fatalf("relaxed dot = % x, want % x", got, want)
		}
	})

	t.Run("i32x4.relaxed_dot_i8x16_i7x16_add_s", func(t *testing.T) {
		body := []byte{0x00}
		body = append(body, simdConst(a)...)
		body = append(body, simdConst(b)...)
		body = append(body, simdConst(i32x4Bytes(1, -2, 3, -4))...)
		body = append(body, simdOp(275)...)
		body = append(body, 0x0b)
		m := mod1(t, nil, []wasm.ValType{wasm.V128}, body)
		want := i32x4Bytes(65026, 26, 103, 256)
		if got := runArm64V128(t, m); got != want {
			t.Fatalf("relaxed dot add = % x, want % x", got, want)
		}
	})
}

func TestSIMDArm64NativeCompareExec(t *testing.T) {
	u8a := i8x16Bytes(-1, 0, 1, -128, 127, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15)
	u8b := i8x16Bytes(0, 0, 2, 127, -128, 5, 5, 8, 8, 10, 9, 12, 13, 12, 15, 14)
	u16a := i16x8Bytes(-1, 0, 1, -32768, 32767, 42, 100, 101)
	u16b := i16x8Bytes(0, 0, 2, 32767, -32768, 42, 101, 100)
	u32a := i32x4Bytes(-1, 0, 1, -2147483648)
	u32b := i32x4Bytes(0, 0, 2, 2147483647)
	i64a := i64x2Bytes(-9223372036854775808, 42)
	i64b := i64x2Bytes(-1, -42)

	cases := []struct {
		name      string
		a, b      [16]byte
		op        uint32
		bitmaskOp uint32
		want      uint32
	}{
		{"i8x16.lt_u", u8a, u8b, 38, 100, cmpU8Mask(u8a, u8b, func(x, y uint8) bool { return x < y })},
		{"i8x16.ge_u", u8a, u8b, 44, 100, cmpU8Mask(u8a, u8b, func(x, y uint8) bool { return x >= y })},
		{"i16x8.gt_u", u16a, u16b, 50, 132, cmpU16Mask(u16a, u16b, func(x, y uint16) bool { return x > y })},
		{"i16x8.le_u", u16a, u16b, 52, 132, cmpU16Mask(u16a, u16b, func(x, y uint16) bool { return x <= y })},
		{"i32x4.lt_u", u32a, u32b, 58, 164, cmpU32Mask(u32a, u32b, func(x, y uint32) bool { return x < y })},
		{"i32x4.ge_u", u32a, u32b, 64, 164, cmpU32Mask(u32a, u32b, func(x, y uint32) bool { return x >= y })},
		{"i64x2.lt_s", i64a, i64b, 216, 196, cmpI64Mask(i64a, i64b, func(x, y int64) bool { return x < y })},
		{"i64x2.ge_s", i64a, i64b, 219, 196, cmpI64Mask(i64a, i64b, func(x, y int64) bool { return x >= y })},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := simdBinaryOpBitmaskBody(tc.a, tc.b, tc.op, tc.bitmaskOp)
			if got := runArm64I32(t, body); got != tc.want {
				t.Fatalf("comparison mask = %#x, want %#x", got, tc.want)
			}
		})
	}
}

func TestSIMDBitselectExec(t *testing.T) {
	a := [16]byte{0xff, 0x00, 0x55, 0xaa, 0x0f, 0xf0, 0x33, 0xcc, 0x80, 0x7f, 0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc}
	b := [16]byte{0x00, 0xff, 0xaa, 0x55, 0xf0, 0x0f, 0xcc, 0x33, 0x7f, 0x80, 0xed, 0xcb, 0xa9, 0x87, 0x65, 0x43}
	mask := [16]byte{0xff, 0xff, 0x0f, 0xf0, 0x00, 0xff, 0x55, 0xaa, 0x80, 0x7f, 0xf0, 0x0f, 0x33, 0xcc, 0x5a, 0xa5}
	want := [16]byte{}
	for i := range want {
		want[i] = (a[i] & mask[i]) | (b[i] &^ mask[i])
	}
	m := mod1(t, nil, []wasm.ValType{wasm.V128}, simdTernaryBody(a, b, mask, 82))
	if got := runArm64V128(t, m); got != want {
		t.Fatalf("v128.bitselect = % x, want % x", got, want)
	}
}

func TestSIMDShuffleSwizzleExec(t *testing.T) {
	t.Run("i8x16.shuffle", func(t *testing.T) {
		a := i8x16Bytes(0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15)
		b := i8x16Bytes(100, 101, 102, 103, 104, 105, 106, 107, 108, 109, 110, 111, 112, 113, 114, 115)
		lanes := [16]byte{0, 16, 1, 17, 15, 31, 8, 24, 4, 20, 5, 21, 7, 23, 10, 26}
		want := i8x16Bytes(0, 100, 1, 101, 15, 115, 8, 108, 4, 104, 5, 105, 7, 107, 10, 110)
		if got := runArm64I32(t, simdShuffleCompareMaskBody(a, b, want, lanes)); got != 0xffff {
			t.Fatalf("comparison mask = %#x, want 0xffff", got)
		}
	})

	t.Run("i8x16.swizzle", func(t *testing.T) {
		src := [16]byte{0, 11, 22, 33, 44, 55, 66, 77, 88, 99, 111, 122, 133, 144, 155, 166}
		idx := [16]byte{15, 14, 13, 12, 0, 1, 2, 3, 16, 17, 31, 127, 128, 129, 254, 255}
		want := [16]byte{166, 155, 144, 133, 0, 11, 22, 33, 0, 0, 0, 0, 0, 0, 0, 0}
		body := simdBinaryCompareMaskBody(src, idx, want, 14, 35, 100)
		if got := runArm64I32(t, body); got != 0xffff {
			t.Fatalf("comparison mask = %#x, want 0xffff", got)
		}
	})

	t.Run("i8x16.relaxed_swizzle", func(t *testing.T) {
		src := i8x16Bytes(0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15)
		idx := [16]byte{0, 1, 15, 16, 17, 31, 32, 127, 128, 129, 254, 255, 2, 3, 4, 5}
		want := i8x16Bytes(0, 1, 15, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2, 3, 4, 5)
		body := simdBinaryCompareMaskBody(src, idx, want, 256, 35, 100)
		if got := runArm64I32(t, body); got != 0xffff {
			t.Fatalf("comparison mask = %#x, want 0xffff", got)
		}
	})
}

func TestSIMDIntegerExtendsExec(t *testing.T) {
	cases := []struct {
		name string
		in   [16]byte
		sub  uint32
		want [16]byte
	}{
		{"i16x8.extend_low_i8x16_s", i8x16Bytes(0, 1, 127, -128, -1, -56, 50, -6, 2, 3, 100, -100, -126, -127, -128, -1), 135, i16x8Bytes(0, 1, 127, -128, -1, -56, 50, -6)},
		{"i16x8.extend_high_i8x16_s", i8x16Bytes(0, 1, 127, -128, -1, -56, 50, -6, 2, 3, 100, -100, -126, -127, -128, -1), 136, i16x8Bytes(2, 3, 100, -100, -126, -127, -128, -1)},
		{"i16x8.extend_low_i8x16_u", i8x16Bytes(0, 1, 127, -128, -1, -56, 50, -6, 2, 3, 100, -100, -126, -127, -128, -1), 137, i16x8Bytes(0, 1, 127, 128, 255, 200, 50, 250)},
		{"i16x8.extend_high_i8x16_u", i8x16Bytes(0, 1, 127, -128, -1, -56, 50, -6, 2, 3, 100, -100, -126, -127, -128, -1), 138, i16x8Bytes(2, 3, 100, 156, 130, 129, 128, 255)},
		{"i32x4.extend_low_i16x8_s", i16x8Bytes(0, 1, 32767, -32768, -1, -12345, 12345, -2), 167, i32x4Bytes(0, 1, 32767, -32768)},
		{"i32x4.extend_high_i16x8_s", i16x8Bytes(0, 1, 32767, -32768, -1, -12345, 12345, -2), 168, i32x4Bytes(-1, -12345, 12345, -2)},
		{"i32x4.extend_low_i16x8_u", i16x8Bytes(0, 1, 32767, -32768, -1, -12345, 12345, -2), 169, i32x4Bytes(0, 1, 32767, 32768)},
		{"i32x4.extend_high_i16x8_u", i16x8Bytes(0, 1, 32767, -32768, -1, -12345, 12345, -2), 170, i32x4Bytes(65535, 53191, 12345, 65534)},
		{"i64x2.extend_low_i32x4_s", i32x4Bytes(0, -1, 2147483647, -2147483648), 199, i64x2Bytes(0, -1)},
		{"i64x2.extend_high_i32x4_s", i32x4Bytes(0, -1, 2147483647, -2147483648), 200, i64x2Bytes(2147483647, -2147483648)},
		{"i64x2.extend_low_i32x4_u", i32x4Bytes(0, -1, 2147483647, -2147483648), 201, i64x2Bytes(0, 4294967295)},
		{"i64x2.extend_high_i32x4_u", i32x4Bytes(0, -1, 2147483647, -2147483648), 202, i64x2Bytes(2147483647, 2147483648)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte{0x00}
			body = append(body, simdConst(tc.in)...)
			body = append(body, simdOp(tc.sub)...)
			body = append(body, 0x0b)
			m := mod1(t, nil, []wasm.ValType{wasm.V128}, body)
			if got := runArm64V128(t, m); got != tc.want {
				t.Fatalf("%s = % x, want % x", tc.name, got, tc.want)
			}
		})
	}
}

func TestSIMDIntegerExtmulExec(t *testing.T) {
	i8a := i8x16Bytes(1, -2, 127, -128, -1, -56, 50, -6, 2, 3, 100, -100, -126, -127, -128, -1)
	i8b := i8x16Bytes(-3, 4, -2, 2, -1, 3, -5, -6, 10, -20, 2, -2, -1, 1, -128, -1)
	i16a := i16x8Bytes(1, -2, 32767, -32768, -1, -12345, 12345, -2)
	i16b := i16x8Bytes(-3, 4, -2, 2, -1, 3, -5, -32768)
	i32a := i32x4Bytes(1, -2, 2147483647, -2147483648)
	i32b := i32x4Bytes(-3, 4, -2, 2)
	cases := []struct {
		name string
		a    [16]byte
		b    [16]byte
		sub  uint32
		want [16]byte
	}{
		{"i16x8.extmul_low_i8x16_s", i8a, i8b, 156, i16x8Bytes(-3, -8, -254, -256, 1, -168, -250, 36)},
		{"i16x8.extmul_high_i8x16_s", i8a, i8b, 157, i16x8Bytes(20, -60, 200, 200, 126, -127, 16384, 1)},
		{"i16x8.extmul_low_i8x16_u", i8a, i8b, 158, i16x8Bytes(253, 1016, 32258, 256, -511, 600, 12550, -3036)},
		{"i16x8.extmul_high_i8x16_u", i8a, i8b, 159, i16x8Bytes(20, 708, 200, -25912, -32386, 129, 16384, -511)},
		{"i32x4.extmul_low_i16x8_s", i16a, i16b, 188, i32x4Bytes(-3, -8, -65534, -65536)},
		{"i32x4.extmul_high_i16x8_s", i16a, i16b, 189, i32x4Bytes(1, -37035, -61725, 65536)},
		{"i32x4.extmul_low_i16x8_u", i16a, i16b, 190, i32x4Bytes(65533, 262136, 2147352578, 65536)},
		{"i32x4.extmul_high_i16x8_u", i16a, i16b, 191, i32x4Bytes(-131071, 159573, 808980195, 2147418112)},
		{"i64x2.extmul_low_i32x4_s", i32a, i32b, 220, i64x2Bytes(-3, -8)},
		{"i64x2.extmul_high_i32x4_s", i32a, i32b, 221, i64x2Bytes(-4294967294, -4294967296)},
		{"i64x2.extmul_low_i32x4_u", i32a, i32b, 222, i64x2Bytes(4294967293, 17179869176)},
		{"i64x2.extmul_high_i32x4_u", i32a, i32b, 223, i64x2Bytes(9223372028264841218, 4294967296)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := mod1(t, nil, []wasm.ValType{wasm.V128}, v128BinaryBody(tc.a, tc.b, tc.sub))
			if got := runArm64V128(t, m); got != tc.want {
				t.Fatalf("%s = % x, want % x", tc.name, got, tc.want)
			}
		})
	}
}

func TestSIMDNarrowExec(t *testing.T) {
	cases := []struct {
		name      string
		a, b      [16]byte
		want      [16]byte
		op        uint32
		cmpOp     uint32
		bitmaskOp uint32
		wantMask  uint32
	}{
		{
			name:      "i8x16.narrow_i16x8_s",
			a:         i16x8Bytes(200, -200, 127, -128, 0, 50, -50, 30000),
			b:         i16x8Bytes(-32768, 32767, 1, -1, 128, -129, 100, -100),
			want:      i8x16Bytes(127, -128, 127, -128, 0, 50, -50, 127, -128, 127, 1, -1, 127, -128, 100, -100),
			op:        101,
			cmpOp:     35,
			bitmaskOp: 100,
			wantMask:  0xffff,
		},
		{
			name:      "i8x16.narrow_i16x8_u",
			a:         i16x8Bytes(-1, -128, 0, 1, 254, 255, 256, 32767),
			b:         i16x8Bytes(-32768, -2, 2, 300, 1000, 255, 256, -1),
			want:      i8x16Bytes(0, 0, 0, 1, -2, -1, -1, -1, 0, 0, 2, -1, -1, -1, -1, 0),
			op:        102,
			cmpOp:     35,
			bitmaskOp: 100,
			wantMask:  0xffff,
		},
		{
			name:      "i16x8.narrow_i32x4_s",
			a:         i32x4Bytes(40000, -40000, 32767, -32768),
			b:         i32x4Bytes(0, -1, 123456789, -123456789),
			want:      i16x8Bytes(32767, -32768, 32767, -32768, 0, -1, 32767, -32768),
			op:        133,
			cmpOp:     45,
			bitmaskOp: 132,
			wantMask:  0xff,
		},
		{
			name:      "i16x8.narrow_i32x4_u",
			a:         i32x4Bytes(-1, -123, 0, 65535),
			b:         i32x4Bytes(65536, 70000, 42, -2147483648),
			want:      i16x8Bytes(0, 0, 0, -1, -1, -1, 42, 0),
			op:        134,
			cmpOp:     45,
			bitmaskOp: 132,
			wantMask:  0xff,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := simdBinaryCompareMaskBody(tc.a, tc.b, tc.want, tc.op, tc.cmpOp, tc.bitmaskOp)
			if got := runArm64I32(t, body); got != tc.wantMask {
				t.Fatalf("comparison mask = %#x, want %#x", got, tc.wantMask)
			}
		})
	}
}

func TestSIMDShiftExec(t *testing.T) {
	cases := []struct {
		name      string
		vec, want [16]byte
		count     int32
		op        uint32
		cmpOp     uint32
		bitmaskOp uint32
		wantMask  uint32
	}{
		{"i8x16.shl", i8x16Bytes(1, -2, 64, -128, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14), i8x16Bytes(2, -4, -128, 0, 6, 8, 10, 12, 14, 16, 18, 20, 22, 24, 26, 28), 1, 107, 35, 100, 0xffff},
		{"i8x16.shr_s", i8x16Bytes(-128, -2, 4, 127, -1, 64, 32, 16, 8, 4, 2, 1, 0, -64, -32, -16), i8x16Bytes(-64, -1, 2, 63, -1, 32, 16, 8, 4, 2, 1, 0, 0, -32, -16, -8), 1, 108, 35, 100, 0xffff},
		{"i8x16.shr_u", i8x16Bytes(-128, -2, 4, 127, -1, 64, 32, 16, 8, 4, 2, 1, 0, -64, -32, -16), i8x16Bytes(64, 127, 2, 63, 127, 32, 16, 8, 4, 2, 1, 0, 0, 96, 112, 120), 1, 109, 35, 100, 0xffff},
		{"i16x8.shr_s", i16x8Bytes(-32768, -2, 4, 32767, -1, 1024, -1024, 7), i16x8Bytes(-8192, -1, 1, 8191, -1, 256, -256, 1), 2, 140, 45, 132, 0xff},
		{"i16x8.shr_u", i16x8Bytes(-32768, -2, 4, 32767, -1, 1024, -1024, 7), i16x8Bytes(8192, 16383, 1, 8191, 16383, 256, 16128, 1), 2, 141, 45, 132, 0xff},
		{"i32x4.shl", i32x4Bytes(1, -2, 0x40000000, -1), i32x4Bytes(4, -8, 0, -4), 2, 171, 55, 164, 0xf},
		{"i32x4.shr_s", i32x4Bytes(-2147483648, -2, 1024, 7), i32x4Bytes(-268435456, -1, 128, 0), 3, 172, 55, 164, 0xf},
		{"i32x4.shr_u", i32x4Bytes(-2147483648, -2, 1024, 7), i32x4Bytes(268435456, 536870911, 128, 0), 3, 173, 55, 164, 0xf},
		{"i64x2.shl", i64x2Bytes(1, -2), i64x2Bytes(8, -16), 3, 203, 214, 196, 0x3},
		{"i64x2.shr_u", i64x2Bytes(-9223372036854775808, -2), i64x2Bytes(2305843009213693952, 4611686018427387903), 2, 205, 214, 196, 0x3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := simdShiftCompareMaskBody(tc.vec, tc.want, tc.count, tc.op, tc.cmpOp, tc.bitmaskOp)
			if got := runArm64I32(t, body); got != tc.wantMask {
				t.Fatalf("comparison mask = %#x, want %#x", got, tc.wantMask)
			}
		})
	}
}

func TestSIMDFloatCompareExec(t *testing.T) {
	nan32 := uint32(0x7fc00001)
	f32a := f32x4Bits(math.Float32bits(1), math.Float32bits(3), nan32, math.Float32bits(4))
	f32b := f32x4Bits(math.Float32bits(2), math.Float32bits(3), math.Float32bits(5), nan32)
	nan64 := uint64(0x7ff8000000000001)
	f64a := f64x2Bits(nan64, math.Float64bits(4))
	f64b := f64x2Bits(math.Float64bits(5), math.Float64bits(4))

	cases := []struct {
		name      string
		a, b      [16]byte
		op        uint32
		bitmaskOp uint32
		want      uint32
	}{
		{"f32x4.eq", f32a, f32b, 65, 164, 0x2},
		{"f32x4.ne", f32a, f32b, 66, 164, 0xd},
		{"f32x4.lt", f32a, f32b, 67, 164, 0x1},
		{"f32x4.gt", f32a, f32b, 68, 164, 0x0},
		{"f32x4.le", f32a, f32b, 69, 164, 0x3},
		{"f32x4.ge", f32a, f32b, 70, 164, 0x2},
		{"f64x2.eq", f64a, f64b, 71, 196, 0x2},
		{"f64x2.ne", f64a, f64b, 72, 196, 0x1},
		{"f64x2.lt", f64a, f64b, 73, 196, 0x0},
		{"f64x2.gt", f64a, f64b, 74, 196, 0x0},
		{"f64x2.le", f64a, f64b, 75, 196, 0x2},
		{"f64x2.ge", f64a, f64b, 76, 196, 0x2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := simdBinaryOpBitmaskBody(tc.a, tc.b, tc.op, tc.bitmaskOp)
			if got := runArm64I32(t, body); got != tc.want {
				t.Fatalf("comparison mask = %#x, want %#x", got, tc.want)
			}
		})
	}
}

func requireF32x4BitsOrNaN(t *testing.T, name string, got [16]byte, want [4]uint32, wantNaN [4]bool) {
	t.Helper()
	for lane := 0; lane < 4; lane++ {
		bits := binary.LittleEndian.Uint32(got[lane*4:])
		if wantNaN[lane] {
			if !math.IsNaN(float64(math.Float32frombits(bits))) {
				t.Fatalf("%s lane %d = 0x%08x, want NaN", name, lane, bits)
			}
			continue
		}
		if bits != want[lane] {
			t.Fatalf("%s lane %d = 0x%08x, want 0x%08x", name, lane, bits, want[lane])
		}
	}
}

func requireF64x2BitsOrNaN(t *testing.T, name string, got [16]byte, want [2]uint64, wantNaN [2]bool) {
	t.Helper()
	for lane := 0; lane < 2; lane++ {
		bits := binary.LittleEndian.Uint64(got[lane*8:])
		if wantNaN[lane] {
			if !math.IsNaN(math.Float64frombits(bits)) {
				t.Fatalf("%s lane %d = 0x%016x, want NaN", name, lane, bits)
			}
			continue
		}
		if bits != want[lane] {
			t.Fatalf("%s lane %d = 0x%016x, want 0x%016x", name, lane, bits, want[lane])
		}
	}
}

func TestSIMDCorePackedFloatConversions(t *testing.T) {
	t.Run("f32x4.demote_f64x2_zero", func(t *testing.T) {
		body := append(append(append([]byte{0x00}, simdConst(f64x2Bytes(1.5, -2.75))...), simdOp(94)...), 0x0b)
		m := mod1(t, nil, []wasm.ValType{wasm.V128}, body)
		got := runArm64V128(t, m)
		requireF32x4BitsOrNaN(t, "f32x4.demote_f64x2_zero", got,
			[4]uint32{math.Float32bits(1.5), math.Float32bits(-2.75), math.Float32bits(0), math.Float32bits(0)},
			[4]bool{})
	})

	t.Run("f32x4.demote_f64x2_zero_nan", func(t *testing.T) {
		body := append(append(append([]byte{0x00}, simdConst(f64x2Bytes(math.Copysign(0, -1), math.NaN()))...), simdOp(94)...), 0x0b)
		m := mod1(t, nil, []wasm.ValType{wasm.V128}, body)
		got := runArm64V128(t, m)
		requireF32x4BitsOrNaN(t, "f32x4.demote_f64x2_zero_nan", got,
			[4]uint32{math.Float32bits(float32(math.Copysign(0, -1))), 0, math.Float32bits(0), math.Float32bits(0)},
			[4]bool{false, true, false, false})
	})

	t.Run("f64x2.promote_low_f32x4", func(t *testing.T) {
		src := f32x4Bytes(float32(math.Copysign(0, -1)), float32(math.Inf(1)), 1234, float32(math.NaN()))
		body := append(append(append([]byte{0x00}, simdConst(src)...), simdOp(95)...), 0x0b)
		m := mod1(t, nil, []wasm.ValType{wasm.V128}, body)
		got := runArm64V128(t, m)
		requireF64x2BitsOrNaN(t, "f64x2.promote_low_f32x4", got,
			[2]uint64{math.Float64bits(math.Copysign(0, -1)), math.Float64bits(math.Inf(1))},
			[2]bool{})
	})

	t.Run("f64x2.promote_low_f32x4_nan", func(t *testing.T) {
		src := f32x4Bits(math.Float32bits(float32(math.NaN())), math.Float32bits(-2.5), math.Float32bits(float32(math.Inf(-1))), math.Float32bits(99))
		body := append(append(append([]byte{0x00}, simdConst(src)...), simdOp(95)...), 0x0b)
		m := mod1(t, nil, []wasm.ValType{wasm.V128}, body)
		got := runArm64V128(t, m)
		requireF64x2BitsOrNaN(t, "f64x2.promote_low_f32x4_nan", got,
			[2]uint64{0, math.Float64bits(-2.5)},
			[2]bool{true, false})
	})

	t.Run("i32x4.trunc_sat_f32x4_s", func(t *testing.T) {
		src := f32x4Bytes(float32(math.NaN()), float32(math.Inf(-1)), -1.75, float32(math.Inf(1)))
		body := append(append(append([]byte{0x00}, simdConst(src)...), simdOp(248)...), 0x0b)
		m := mod1(t, nil, []wasm.ValType{wasm.V128}, body)
		if got, want := runArm64V128(t, m), i32x4Bytes(0, math.MinInt32, -1, math.MaxInt32); got != want {
			t.Fatalf("i32x4.trunc_sat_f32x4_s = % x, want % x", got, want)
		}
	})

	t.Run("i32x4.trunc_sat_f32x4_u", func(t *testing.T) {
		src := f32x4Bytes(float32(math.NaN()), -1, 255.75, float32(math.Inf(1)))
		body := append(append(append([]byte{0x00}, simdConst(src)...), simdOp(249)...), 0x0b)
		m := mod1(t, nil, []wasm.ValType{wasm.V128}, body)
		if got, want := runArm64V128(t, m), i32x4Bytes(0, 0, 255, -1); got != want {
			t.Fatalf("i32x4.trunc_sat_f32x4_u = % x, want % x", got, want)
		}
	})

	t.Run("i32x4.trunc_sat_f64x2_zero", func(t *testing.T) {
		src := f64x2Bytes(math.Inf(-1), math.Inf(1))
		body := append(append(append([]byte{0x00}, simdConst(src)...), simdOp(252)...), 0x0b)
		m := mod1(t, nil, []wasm.ValType{wasm.V128}, body)
		if got, want := runArm64V128(t, m), i32x4Bytes(math.MinInt32, math.MaxInt32, 0, 0); got != want {
			t.Fatalf("i32x4.trunc_sat_f64x2_zero = % x, want % x", got, want)
		}
	})

	t.Run("i32x4.trunc_sat_f64x2_s_edges", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			a, b float64
			want [16]byte
		}{
			{"nan_and_fraction", math.NaN(), -123.75, i32x4Bytes(0, -123, 0, 0)},
			{"overflow", float64(math.MinInt32) - 1, float64(math.MaxInt32) + 1, i32x4Bytes(math.MinInt32, math.MaxInt32, 0, 0)},
		} {
			t.Run(tc.name, func(t *testing.T) {
				body := append(append(append([]byte{0x00}, simdConst(f64x2Bytes(tc.a, tc.b))...), simdOp(252)...), 0x0b)
				m := mod1(t, nil, []wasm.ValType{wasm.V128}, body)
				if got := runArm64V128(t, m); got != tc.want {
					t.Fatalf("i32x4.trunc_sat_f64x2_s = % x, want % x", got, tc.want)
				}
			})
		}
	})

	t.Run("i32x4.trunc_sat_f64x2_u_edges", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			a, b float64
			want [16]byte
		}{
			{"negative_and_fraction", -1, 123.75, i32x4Bytes(0, 123, 0, 0)},
			{"nan_and_overflow", math.NaN(), float64(math.MaxUint32) + 1, i32x4Bytes(0, -1, 0, 0)},
		} {
			t.Run(tc.name, func(t *testing.T) {
				body := append(append(append([]byte{0x00}, simdConst(f64x2Bytes(tc.a, tc.b))...), simdOp(253)...), 0x0b)
				m := mod1(t, nil, []wasm.ValType{wasm.V128}, body)
				if got := runArm64V128(t, m); got != tc.want {
					t.Fatalf("i32x4.trunc_sat_f64x2_u = % x, want % x", got, tc.want)
				}
			})
		}
	})

	cases := []struct {
		name string
		src  [16]byte
		sub  uint32
		want [16]byte
	}{
		{
			name: "f32x4.convert_i32x4_s",
			src:  i32x4Bytes(math.MinInt32, -1, 0, math.MaxInt32),
			sub:  250,
			want: f32x4Bytes(float32(math.MinInt32), -1, 0, float32(math.MaxInt32)),
		},
		{
			name: "f32x4.convert_i32x4_u",
			src:  i32x4Bytes(0, 1, -2147483648, -1),
			sub:  251,
			want: f32x4Bytes(0, 1, float32(uint32(1)<<31), float32(uint64(math.MaxUint32))),
		},
		{
			name: "f64x2.convert_low_i32x4_s",
			src:  i32x4Bytes(math.MinInt32, math.MaxInt32, 123, 456),
			sub:  254,
			want: f64x2Bytes(float64(math.MinInt32), float64(math.MaxInt32)),
		},
		{
			name: "f64x2.convert_low_i32x4_u",
			src:  i32x4Bytes(-2147483648, -1, 123, 456),
			sub:  255,
			want: f64x2Bytes(float64(uint32(1)<<31), float64(uint64(math.MaxUint32))),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := append(append(append([]byte{0x00}, simdConst(tc.src)...), simdOp(tc.sub)...), 0x0b)
			m := mod1(t, nil, []wasm.ValType{wasm.V128}, body)
			if got := runArm64V128(t, m); got != tc.want {
				t.Fatalf("%s = % x, want % x", tc.name, got, tc.want)
			}
		})
	}
}

func TestSIMDCorePackedFloatMinMax(t *testing.T) {
	negZero32 := math.Float32bits(float32(math.Copysign(0, -1)))
	posZero32 := math.Float32bits(0)
	nan32a := uint32(0x7fc00001)
	nan32b := uint32(0x7fc00002)
	canonicalNaN32 := uint32(0xffc00000)
	negZero64 := math.Float64bits(math.Copysign(0, -1))
	posZero64 := math.Float64bits(0)
	nan64a := uint64(0x7ff8000000000001)
	nan64b := uint64(0x7ff8000000000002)
	canonicalNaN64 := uint64(0xfff8000000000000)

	f32a := f32x4Bits(math.Float32bits(3), negZero32, nan32a, math.Float32bits(4))
	f32b := f32x4Bits(math.Float32bits(2), posZero32, math.Float32bits(5), nan32b)
	f32Cases := []struct {
		name    string
		sub     uint32
		want    [4]uint32
		wantNaN [4]bool
	}{
		{"f32x4.min", 232, [4]uint32{math.Float32bits(2), negZero32, canonicalNaN32, canonicalNaN32}, [4]bool{}},
		{"f32x4.max", 233, [4]uint32{math.Float32bits(3), posZero32, canonicalNaN32, canonicalNaN32}, [4]bool{}},
		{"f32x4.pmin", 234, [4]uint32{math.Float32bits(2), negZero32, 0, math.Float32bits(4)}, [4]bool{false, false, true, false}},
		{"f32x4.pmax", 235, [4]uint32{math.Float32bits(3), negZero32, 0, math.Float32bits(4)}, [4]bool{false, false, true, false}},
	}
	for _, tc := range f32Cases {
		t.Run(tc.name, func(t *testing.T) {
			m := mod1(t, nil, []wasm.ValType{wasm.V128}, v128BinaryBody(f32a, f32b, tc.sub))
			got := runArm64V128(t, m)
			requireF32x4BitsOrNaN(t, tc.name, got, tc.want, tc.wantNaN)
		})
	}

	f64a := f64x2Bits(nan64a, negZero64)
	f64b := f64x2Bits(math.Float64bits(5), posZero64)
	f64Cases := []struct {
		name    string
		a       [16]byte
		b       [16]byte
		sub     uint32
		want    [2]uint64
		wantNaN [2]bool
	}{
		{"f64x2.min", f64a, f64b, 244, [2]uint64{canonicalNaN64, negZero64}, [2]bool{}},
		{"f64x2.max", f64a, f64b, 245, [2]uint64{canonicalNaN64, posZero64}, [2]bool{}},
		{"f64x2.pmin_first_nan", f64a, f64b, 246, [2]uint64{0, negZero64}, [2]bool{true, false}},
		{"f64x2.pmax_first_nan", f64a, f64b, 247, [2]uint64{0, negZero64}, [2]bool{true, false}},
		{"f64x2.pmin_second_nan", f64x2Bits(math.Float64bits(4), negZero64), f64x2Bits(nan64b, posZero64), 246, [2]uint64{math.Float64bits(4), negZero64}, [2]bool{false, false}},
		{"f64x2.pmax_second_nan", f64x2Bits(math.Float64bits(4), negZero64), f64x2Bits(nan64b, posZero64), 247, [2]uint64{math.Float64bits(4), negZero64}, [2]bool{false, false}},
	}
	for _, tc := range f64Cases {
		t.Run(tc.name, func(t *testing.T) {
			m := mod1(t, nil, []wasm.ValType{wasm.V128}, v128BinaryBody(tc.a, tc.b, tc.sub))
			got := runArm64V128(t, m)
			requireF64x2BitsOrNaN(t, tc.name, got, tc.want, tc.wantNaN)
		})
	}
}

func TestSIMDAnyAndAllTrueExec(t *testing.T) {
	cases := []struct {
		name string
		vec  [16]byte
		op   uint32
		want uint32
	}{
		{"v128.any_true zero", [16]byte{}, 83, 0},
		{"v128.any_true high lane", i8x16Bytes(0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1), 83, 1},
		{"i8x16.all_true yes", i8x16Bytes(1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16), 99, 1},
		{"i8x16.all_true no", i8x16Bytes(1, 2, 3, 0, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16), 99, 0},
		{"i16x8.all_true yes", i16x8Bytes(1, 2, 3, 4, 5, 6, 7, 8), 131, 1},
		{"i16x8.all_true no", i16x8Bytes(1, 2, 0, 4, 5, 6, 7, 8), 131, 0},
		{"i32x4.all_true yes", i32x4Bytes(1, 2, 3, 4), 163, 1},
		{"i32x4.all_true no", i32x4Bytes(1, 0, 3, 4), 163, 0},
		{"i64x2.all_true yes", i64x2Bytes(1, 2), 195, 1},
		{"i64x2.all_true no", i64x2Bytes(1, 0), 195, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := runArm64I32(t, simdI32Body(tc.vec, tc.op)); got != tc.want {
				t.Fatalf("result = %d, want %d", got, tc.want)
			}
		})
	}
}
