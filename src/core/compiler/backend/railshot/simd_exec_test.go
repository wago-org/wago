//go:build linux && amd64

package amd64

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func v128ConstBytes(b [16]byte) []byte {
	out := []byte{0xfd, 0x0c}
	return append(out, b[:]...)
}

func simdOp(sub uint32) []byte {
	return append([]byte{0xfd}, wasmtest.ULEB(sub)...)
}

func v128BinaryBody(a, b [16]byte, sub uint32) []byte {
	body := []byte{0x00}
	body = append(body, v128ConstBytes(a)...)
	body = append(body, v128ConstBytes(b)...)
	body = append(body, simdOp(sub)...)
	body = append(body, 0x0b)
	return body
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

func cmpMaskBytes(width int, lanes ...bool) [16]byte {
	var out [16]byte
	for i, ok := range lanes {
		if !ok {
			continue
		}
		for j := 0; j < width; j++ {
			out[i*width+j] = 0xff
		}
	}
	return out
}

func runAmd64V128(t *testing.T, m *wasm.Module, arg *[16]byte) [16]byte {
	t.Helper()
	cm, err := CompileModule(m)
	if err != nil {
		t.Fatalf("amd64 compile: %v", err)
	}
	eng, err := runtime.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	jm, err := runtime.NewJobMemory(65536)
	if err != nil {
		t.Fatal(err)
	}
	defer jm.Close()
	ar, err := runtime.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	defer ar.Close()
	mem, entry, err := runtime.MapCode(cm.Code)
	if err != nil {
		t.Fatalf("map: %v", err)
	}
	defer runtime.Unmap(mem)

	serArgs := ar.Alloc(256)
	results := ar.Alloc(256)
	trap := ar.Alloc(8)
	if arg != nil {
		copy(serArgs, arg[:])
	}
	if err := eng.Call(entry+uintptr(cm.Entry[0]), serArgs, jm.LinearMemory(), trap, results); err != nil {
		t.Fatalf("call: %v", err)
	}
	var out [16]byte
	copy(out[:], results[:16])
	return out
}

func runMemAmd64V128(t *testing.T, m *wasm.Module, setup func([]byte)) ([16]byte, []byte, error) {
	t.Helper()
	cm, err := CompileModule(m)
	if err != nil {
		t.Fatalf("amd64 compile: %v", err)
	}
	eng, err := runtime.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	jm, err := runtime.NewJobMemory(1 << 16)
	if err != nil {
		t.Fatal(err)
	}
	defer jm.Close()
	lin := jm.LinearMemory()
	if setup != nil {
		setup(lin)
	}
	ar, err := runtime.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	defer ar.Close()
	mem, entry, err := runtime.MapCode(cm.Code)
	if err != nil {
		t.Fatalf("map: %v", err)
	}
	defer runtime.Unmap(mem)
	serArgs := ar.Alloc(256)
	results := ar.Alloc(256)
	trap := ar.Alloc(8)
	callErr := eng.Call(entry+uintptr(cm.Entry[0]), serArgs, lin, trap, results)
	var out [16]byte
	copy(out[:], results[:16])
	return out, append([]byte(nil), lin...), callErr
}

func TestSIMDV128ConstResultAndFrontendGate(t *testing.T) {
	want := [16]byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}
	body := v128ConstBytes(want)
	body = append(body, 0x0b)
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.V128}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	m, err := frontend.DecodeValidate(mod)
	if err != nil {
		t.Fatalf("DecodeValidate: %v", err)
	}
	if got := runAmd64V128(t, m, nil); got != want {
		t.Fatalf("v128.const result = % x, want % x", got, want)
	}
}

func TestSIMDV128ParamLocalResult(t *testing.T) {
	want := [16]byte{1, 3, 5, 7, 9, 11, 13, 15, 17, 19, 21, 23, 25, 27, 29, 31}
	// (func (param v128) (result v128) (local v128) local.get 0; local.set 1; local.get 1)
	body := []byte{0x01, 0x01, 0x7b, 0x20, 0x00, 0x21, 0x01, 0x20, 0x01, 0x0b}
	m := mod1(t, []wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128}, body)
	if got := runAmd64V128(t, m, &want); got != want {
		t.Fatalf("v128 param/local/result = % x, want % x", got, want)
	}
}

func TestSIMDV128LoadStoreAndBitwise(t *testing.T) {
	a := [16]byte{0xff, 0x0f, 0xf0, 0x55, 0xaa, 0x33, 0xcc, 0x99, 0x12, 0x34, 0x56, 0x78, 0x87, 0x65, 0x43, 0x21}
	b := [16]byte{0x0f, 0xff, 0x0f, 0xaa, 0x55, 0xcc, 0x33, 0x66, 0xf0, 0x0f, 0xf0, 0x0f, 0x78, 0x56, 0x34, 0x12}
	var want [16]byte
	for i := range want {
		want[i] = a[i] & b[i]
	}
	body := []byte{0x00}
	body = append(body, v128ConstBytes(a)...)
	body = append(body, v128ConstBytes(b)...)
	body = append(body, 0xfd, 0x4e, 0x0b) // v128.and; end
	m := mod1(t, nil, []wasm.ValType{wasm.V128}, body)
	if got := runAmd64V128(t, m, nil); got != want {
		t.Fatalf("v128.and = % x, want % x", got, want)
	}

	mask := [16]byte{0xff, 0, 0xff, 0, 0xff, 0, 0xff, 0, 0xf0, 0x0f, 0xaa, 0x55, 0x33, 0xcc, 0x5a, 0xa5}
	var selectWant [16]byte
	for i := range selectWant {
		selectWant[i] = (a[i] & mask[i]) | (b[i] &^ mask[i])
	}
	selectBody := []byte{0x00}
	selectBody = append(selectBody, v128ConstBytes(a)...)
	selectBody = append(selectBody, v128ConstBytes(b)...)
	selectBody = append(selectBody, v128ConstBytes(mask)...)
	selectBody = append(selectBody, 0xfd, 0x52, 0x0b) // v128.bitselect; end
	selectMod := mod1(t, nil, []wasm.ValType{wasm.V128}, selectBody)
	if got := runAmd64V128(t, selectMod, nil); got != selectWant {
		t.Fatalf("v128.bitselect = % x, want % x", got, selectWant)
	}

	// Store the result at linear-memory offset 32, then load it back from offset 32.
	storeBody := []byte{0x00, 0x41, 0x20}
	storeBody = append(storeBody, v128ConstBytes(want)...)
	storeBody = append(storeBody, 0xfd, 0x0b, 0x04, 0x00, 0x0b) // v128.store align=16 offset=0
	storeMod := modMem(t, 1, nil, nil, storeBody)
	_, mem, err := runMemAmd64V128(t, storeMod, nil)
	if err != nil {
		t.Fatalf("store call: %v", err)
	}
	if !bytes.Equal(mem[32:48], want[:]) {
		t.Fatalf("stored bytes = % x, want % x", mem[32:48], want)
	}

	loadBody := []byte{0x00, 0x41, 0x20, 0xfd, 0x00, 0x04, 0x00, 0x0b} // i32.const 32; v128.load; end
	loadMod := modMem(t, 1, nil, []wasm.ValType{wasm.V128}, loadBody)
	got, _, err := runMemAmd64V128(t, loadMod, func(mem []byte) { copy(mem[32:48], want[:]) })
	if err != nil {
		t.Fatalf("load call: %v", err)
	}
	if got != want {
		t.Fatalf("v128.load = % x, want % x", got, want)
	}
}

func TestSIMDSplatLanes(t *testing.T) {
	cases := []struct {
		name string
		body []byte
		want [16]byte
	}{
		{"i8x16.splat", append(append([]byte{0x00, 0x41}, wasmtest.SLEB32(-91)...), 0xfd, 0x0f, 0x0b), [16]byte{0xa5, 0xa5, 0xa5, 0xa5, 0xa5, 0xa5, 0xa5, 0xa5, 0xa5, 0xa5, 0xa5, 0xa5, 0xa5, 0xa5, 0xa5, 0xa5}},
		{"i16x8.splat", append(append([]byte{0x00, 0x41}, wasmtest.SLEB32(0x1234)...), 0xfd, 0x10, 0x0b), [16]byte{0x34, 0x12, 0x34, 0x12, 0x34, 0x12, 0x34, 0x12, 0x34, 0x12, 0x34, 0x12, 0x34, 0x12, 0x34, 0x12}},
		{"i32x4.splat", append(append([]byte{0x00, 0x41}, wasmtest.SLEB32(0x12345678)...), 0xfd, 0x11, 0x0b), [16]byte{0x78, 0x56, 0x34, 0x12, 0x78, 0x56, 0x34, 0x12, 0x78, 0x56, 0x34, 0x12, 0x78, 0x56, 0x34, 0x12}},
		{"i64x2.splat", append(append([]byte{0x00, 0x42}, wasmtest.SLEB64(0x1122334455667788)...), 0xfd, 0x12, 0x0b), [16]byte{0x88, 0x77, 0x66, 0x55, 0x44, 0x33, 0x22, 0x11, 0x88, 0x77, 0x66, 0x55, 0x44, 0x33, 0x22, 0x11}},
		{"f32x4.splat", []byte{0x00, 0x43, 0x00, 0x00, 0xc0, 0x3f, 0xfd, 0x13, 0x0b}, [16]byte{0x00, 0x00, 0xc0, 0x3f, 0x00, 0x00, 0xc0, 0x3f, 0x00, 0x00, 0xc0, 0x3f, 0x00, 0x00, 0xc0, 0x3f}},
		{"f64x2.splat", []byte{0x00, 0x44, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x04, 0xc0, 0xfd, 0x14, 0x0b}, [16]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x04, 0xc0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x04, 0xc0}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := mod1(t, nil, []wasm.ValType{wasm.V128}, tc.body)
			if got := runAmd64V128(t, m, nil); got != tc.want {
				t.Fatalf("%s = % x, want % x", tc.name, got, tc.want)
			}
		})
	}
}

func TestSIMDExtractLanes(t *testing.T) {
	vec := [16]byte{0xf0, 0x7f, 0x80, 0xff, 0x78, 0x56, 0x34, 0x12, 0x00, 0x00, 0xc0, 0xbf, 0x00, 0x00, 0x00, 0x40}
	prefix := append([]byte{0x00}, v128ConstBytes(vec)...)
	cases := []struct {
		name    string
		results []wasm.ValType
		body    []byte
		want    uint64
	}{
		{"i8x16.extract_lane_s", []wasm.ValType{wasm.I32}, append(append([]byte{}, prefix...), 0xfd, 0x15, 0x00, 0x0b), uint64(0xfffffff0)},
		{"i8x16.extract_lane_u", []wasm.ValType{wasm.I32}, append(append([]byte{}, prefix...), 0xfd, 0x16, 0x00, 0x0b), 0xf0},
		{"i16x8.extract_lane_s", []wasm.ValType{wasm.I32}, append(append([]byte{}, prefix...), 0xfd, 0x18, 0x01, 0x0b), uint64(0xffffff80)},
		{"i32x4.extract_lane", []wasm.ValType{wasm.I32}, append(append([]byte{}, prefix...), 0xfd, 0x1b, 0x01, 0x0b), 0x12345678},
		{"i64x2.extract_lane", []wasm.ValType{wasm.I64}, append(append([]byte{}, prefix...), 0xfd, 0x1d, 0x01, 0x0b), 0x40000000bfc00000},
		{"f32x4.extract_lane", []wasm.ValType{wasm.F32}, append(append([]byte{}, prefix...), 0xfd, 0x1f, 0x02, 0x0b), uint64(math.Float32bits(-1.5))},
		{"f64x2.extract_lane", []wasm.ValType{wasm.F64}, append(append([]byte{}, prefix...), 0xfd, 0x21, 0x01, 0x0b), 0x40000000bfc00000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := mod1(t, nil, tc.results, tc.body)
			if got := runAmd64u(t, m); got != tc.want {
				t.Fatalf("%s = %#x, want %#x", tc.name, got, tc.want)
			}
		})
	}
}

func TestSIMDReplaceLanes(t *testing.T) {
	zero := [16]byte{}
	cases := []struct {
		name string
		body []byte
		want [16]byte
	}{
		{"i8x16.replace_lane", append(append(append([]byte{0x00}, v128ConstBytes(zero)...), append([]byte{0x41}, wasmtest.SLEB32(-91)...)...), 0xfd, 0x17, 0x0f, 0x0b), [16]byte{15: 0xa5}},
		{"i16x8.replace_lane", append(append(append([]byte{0x00}, v128ConstBytes(zero)...), append([]byte{0x41}, wasmtest.SLEB32(0x1234)...)...), 0xfd, 0x1a, 0x07, 0x0b), [16]byte{14: 0x34, 15: 0x12}},
		{"i32x4.replace_lane", append(append(append([]byte{0x00}, v128ConstBytes(zero)...), append([]byte{0x41}, wasmtest.SLEB32(0x12345678)...)...), 0xfd, 0x1c, 0x02, 0x0b), [16]byte{8: 0x78, 9: 0x56, 10: 0x34, 11: 0x12}},
		{"i64x2.replace_lane", append(append(append([]byte{0x00}, v128ConstBytes(zero)...), append([]byte{0x42}, wasmtest.SLEB64(0x1122334455667788)...)...), 0xfd, 0x1e, 0x01, 0x0b), [16]byte{8: 0x88, 9: 0x77, 10: 0x66, 11: 0x55, 12: 0x44, 13: 0x33, 14: 0x22, 15: 0x11}},
		{"f32x4.replace_lane", append([]byte{0x00}, append(append(v128ConstBytes(zero), 0x43, 0x00, 0x00, 0xc0, 0x3f), 0xfd, 0x20, 0x02, 0x0b)...), [16]byte{8: 0x00, 9: 0x00, 10: 0xc0, 11: 0x3f}},
		{"f64x2.replace_lane", append([]byte{0x00}, append(append(v128ConstBytes(zero), 0x44, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x04, 0xc0), 0xfd, 0x22, 0x01, 0x0b)...), [16]byte{8: 0x00, 9: 0x00, 10: 0x00, 11: 0x00, 12: 0x00, 13: 0x00, 14: 0x04, 15: 0xc0}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := mod1(t, nil, []wasm.ValType{wasm.V128}, tc.body)
			if got := runAmd64V128(t, m, nil); got != tc.want {
				t.Fatalf("%s = % x, want % x", tc.name, got, tc.want)
			}
		})
	}
}

func TestSIMDIntegerUnary(t *testing.T) {
	cases := []struct {
		name string
		in   [16]byte
		sub  uint32
		want [16]byte
	}{
		{"i8x16.neg", i8x16Bytes(-128, -127, -2, -1, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 127), 97, i8x16Bytes(-128, 127, 2, 1, 0, -1, -2, -3, -4, -5, -6, -7, -8, -9, -10, -127)},
		{"i16x8.neg", i16x8Bytes(-32768, -32767, -2, -1, 0, 1, 2, 32767), 129, i16x8Bytes(-32768, 32767, 2, 1, 0, -1, -2, -32767)},
		{"i32x4.neg", i32x4Bytes(-2147483648, -123456789, 0, 123456789), 161, i32x4Bytes(-2147483648, 123456789, 0, -123456789)},
		{"i64x2.neg", i64x2Bytes(-9223372036854775808, -1234567890123), 193, i64x2Bytes(-9223372036854775808, 1234567890123)},
		{"i8x16.abs", i8x16Bytes(-128, -127, -2, -1, 0, 1, 2, 3, -4, 5, -6, 7, -8, 9, -10, 127), 96, i8x16Bytes(-128, 127, 2, 1, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 127)},
		{"i16x8.abs", i16x8Bytes(-32768, -32767, -2, -1, 0, 1, 2, 32767), 128, i16x8Bytes(-32768, 32767, 2, 1, 0, 1, 2, 32767)},
		{"i32x4.abs", i32x4Bytes(-2147483648, -123456789, 0, 123456789), 160, i32x4Bytes(-2147483648, 123456789, 0, 123456789)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte{0x00}
			body = append(body, v128ConstBytes(tc.in)...)
			body = append(body, simdOp(tc.sub)...)
			body = append(body, 0x0b)
			m := mod1(t, nil, []wasm.ValType{wasm.V128}, body)
			if got := runAmd64V128(t, m, nil); got != tc.want {
				t.Fatalf("%s = % x, want % x", tc.name, got, tc.want)
			}
		})
	}
}

func TestSIMDIntegerArithmeticComparisons(t *testing.T) {
	i8a := i8x16Bytes(120, -128, 1, -5, 0, 127, -1, 64, 10, 20, 30, 40, 50, 60, 70, 80)
	i8b := i8x16Bytes(10, 1, -2, -5, 0, -1, 1, 64, -10, 21, 30, 41, -50, 61, 71, 81)
	i16a := i16x8Bytes(30000, -32768, 1, -5, 0, 32767, -1, 1234)
	i16b := i16x8Bytes(10000, 1, -2, -5, 0, -1, 1, 4321)
	i32a := i32x4Bytes(0x7fffffff, -2147483648, -5, 123456789)
	i32b := i32x4Bytes(1, 1, -5, -123456789)
	i64a := i64x2Bytes(0x7fffffffffffffff, -5)
	i64b := i64x2Bytes(1, -5)

	cases := []struct {
		name string
		a    [16]byte
		b    [16]byte
		sub  uint32
		want [16]byte
	}{
		{"i8x16.add", i8a, i8b, 110, i8x16Bytes(-126, -127, -1, -10, 0, 126, 0, -128, 0, 41, 60, 81, 0, 121, -115, -95)},
		{"i8x16.sub", i8a, i8b, 113, i8x16Bytes(110, 127, 3, 0, 0, -128, -2, 0, 20, -1, 0, -1, 100, -1, -1, -1)},
		{"i8x16.eq", i8a, i8b, 35, cmpMaskBytes(1, false, false, false, true, true, false, false, true, false, false, true, false, false, false, false, false)},
		{"i8x16.ne", i8a, i8b, 36, cmpMaskBytes(1, true, true, true, false, false, true, true, false, true, true, false, true, true, true, true, true)},
		{"i8x16.gt_s", i8a, i8b, 39, cmpMaskBytes(1, true, false, true, false, false, true, false, false, true, false, false, false, true, false, false, false)},
		{"i8x16.gt_u", i8a, i8b, 40, cmpMaskBytes(1, true, true, false, false, false, false, true, false, false, false, false, false, false, false, false, false)},
		{"i8x16.min_s", i8a, i8b, 118, i8x16Bytes(10, -128, -2, -5, 0, -1, -1, 64, -10, 20, 30, 40, -50, 60, 70, 80)},
		{"i8x16.min_u", i8a, i8b, 119, i8x16Bytes(10, 1, 1, -5, 0, 127, 1, 64, 10, 20, 30, 40, 50, 60, 70, 80)},
		{"i8x16.max_s", i8a, i8b, 120, i8x16Bytes(120, 1, 1, -5, 0, 127, 1, 64, 10, 21, 30, 41, 50, 61, 71, 81)},
		{"i8x16.max_u", i8a, i8b, 121, i8x16Bytes(120, -128, -2, -5, 0, -1, -1, 64, -10, 21, 30, 41, -50, 61, 71, 81)},

		{"i16x8.add", i16a, i16b, 142, i16x8Bytes(-25536, -32767, -1, -10, 0, 32766, 0, 5555)},
		{"i16x8.sub", i16a, i16b, 145, i16x8Bytes(20000, 32767, 3, 0, 0, -32768, -2, -3087)},
		{"i16x8.mul", i16x8Bytes(30000, -32768, 12345, -12345, 0, 32767, -1, 256), i16x8Bytes(3, 2, -2, -3, 123, 2, -1, 256), 149, i16x8Bytes(24464, 0, -24690, -28501, 0, -2, 1, 0)},
		{"i16x8.eq", i16a, i16b, 45, cmpMaskBytes(2, false, false, false, true, true, false, false, false)},
		{"i16x8.ne", i16a, i16b, 46, cmpMaskBytes(2, true, true, true, false, false, true, true, true)},
		{"i16x8.gt_s", i16a, i16b, 49, cmpMaskBytes(2, true, false, true, false, false, true, false, false)},
		{"i16x8.gt_u", i16a, i16b, 50, cmpMaskBytes(2, true, true, false, false, false, false, true, false)},
		{"i16x8.min_s", i16a, i16b, 150, i16x8Bytes(10000, -32768, -2, -5, 0, -1, -1, 1234)},
		{"i16x8.min_u", i16a, i16b, 151, i16x8Bytes(10000, 1, 1, -5, 0, 32767, 1, 1234)},
		{"i16x8.max_s", i16a, i16b, 152, i16x8Bytes(30000, 1, 1, -5, 0, 32767, 1, 4321)},
		{"i16x8.max_u", i16a, i16b, 153, i16x8Bytes(30000, -32768, -2, -5, 0, -1, -1, 4321)},

		{"i32x4.add", i32a, i32b, 174, i32x4Bytes(-2147483648, -2147483647, -10, 0)},
		{"i32x4.sub", i32a, i32b, 177, i32x4Bytes(2147483646, 2147483647, 0, 246913578)},
		{"i32x4.mul", i32x4Bytes(2147483647, -2147483648, 123456789, -123456789), i32x4Bytes(2, 2, -3, -3), 181, i32x4Bytes(-2, 0, -370370367, 370370367)},
		{"i32x4.eq", i32a, i32b, 55, cmpMaskBytes(4, false, false, true, false)},
		{"i32x4.ne", i32a, i32b, 56, cmpMaskBytes(4, true, true, false, true)},
		{"i32x4.gt_s", i32a, i32b, 59, cmpMaskBytes(4, true, false, false, true)},
		{"i32x4.gt_u", i32a, i32b, 60, cmpMaskBytes(4, true, true, false, false)},
		{"i32x4.min_s", i32a, i32b, 182, i32x4Bytes(1, -2147483648, -5, -123456789)},
		{"i32x4.min_u", i32a, i32b, 183, i32x4Bytes(1, 1, -5, 123456789)},
		{"i32x4.max_s", i32a, i32b, 184, i32x4Bytes(2147483647, 1, -5, 123456789)},
		{"i32x4.max_u", i32a, i32b, 185, i32x4Bytes(2147483647, -2147483648, -5, -123456789)},

		{"i64x2.add", i64a, i64b, 206, i64x2Bytes(-9223372036854775808, -10)},
		{"i64x2.sub", i64a, i64b, 209, i64x2Bytes(9223372036854775806, 0)},
		{"i64x2.eq", i64a, i64b, 214, cmpMaskBytes(8, false, true)},
		{"i64x2.ne", i64a, i64b, 215, cmpMaskBytes(8, true, false)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := mod1(t, nil, []wasm.ValType{wasm.V128}, v128BinaryBody(tc.a, tc.b, tc.sub))
			if got := runAmd64V128(t, m, nil); got != tc.want {
				t.Fatalf("%s = % x, want % x", tc.name, got, tc.want)
			}
		})
	}
}

func TestSIMDBooleanReductionsBitmask(t *testing.T) {
	boolCases := []struct {
		name string
		in   [16]byte
		sub  uint32
		want int32
	}{
		{"v128.any_true zero", i8x16Bytes(0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0), 83, 0},
		{"v128.any_true low bit", i8x16Bytes(0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0), 83, 1},
		{"v128.any_true sign bit", i8x16Bytes(0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, -128), 83, 1},
		{"i8x16.all_true all nonzero", i8x16Bytes(1, -1, 2, -2, 3, -3, 4, -4, 5, -5, 6, -6, 7, -7, 8, -128), 99, 1},
		{"i8x16.all_true has zero", i8x16Bytes(1, -1, 2, -2, 3, -3, 4, -4, 0, -5, 6, -6, 7, -7, 8, -128), 99, 0},
		{"i16x8.all_true all nonzero lanes", i16x8Bytes(1, -1, 256, -256, 32767, -32768, 2, -2), 131, 1},
		{"i16x8.all_true has zero lane", i16x8Bytes(1, -1, 256, 0, 32767, -32768, 2, -2), 131, 0},
		{"i32x4.all_true all nonzero lanes", i32x4Bytes(1, -1, 65536, -65536), 163, 1},
		{"i32x4.all_true has zero lane", i32x4Bytes(1, -1, 0, -65536), 163, 0},
		{"i64x2.all_true all nonzero lanes", i64x2Bytes(1, -1), 195, 1},
		{"i64x2.all_true has zero lane", i64x2Bytes(1, 0), 195, 0},
	}
	for _, tc := range boolCases {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte{0x00}
			body = append(body, v128ConstBytes(tc.in)...)
			body = append(body, simdOp(tc.sub)...)
			body = append(body, 0x0b)
			m := mod1(t, nil, []wasm.ValType{wasm.I32}, body)
			if got := runAmd64(t, m); got != tc.want {
				t.Fatalf("result = %d, want %d", got, tc.want)
			}
		})
	}

	bitmaskCases := []struct {
		name string
		in   [16]byte
		sub  uint32
		want int32
	}{
		{"i8x16.bitmask", i8x16Bytes(-1, 0, -128, 127, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, -2), 100, 0x8005},
		{"i16x8.bitmask", i16x8Bytes(-1, 0, -32768, 32767, 1, -2, 123, -123), 132, 0xa5},
		{"i32x4.bitmask", i32x4Bytes(-1, 0, -2147483648, 2147483647), 164, 0x5},
		{"i64x2.bitmask", i64x2Bytes(1, -1), 196, 0x2},
	}
	for _, tc := range bitmaskCases {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte{0x00}
			body = append(body, v128ConstBytes(tc.in)...)
			body = append(body, simdOp(tc.sub)...)
			body = append(body, 0x0b)
			m := mod1(t, nil, []wasm.ValType{wasm.I32}, body)
			if got := runAmd64(t, m); got != tc.want {
				t.Fatalf("result = %#x, want %#x", uint32(got), uint32(tc.want))
			}
		})
	}
}

func TestSIMDPackedFloatArithmeticComparisons(t *testing.T) {
	f32aVals := []float32{1.5, -4, 9, -10}
	f32bVals := []float32{2.25, -8, -3, 2}
	f64aVals := []float64{1.5, -10}
	f64bVals := []float64{2.25, 2}
	f32a := f32x4Bytes(f32aVals...)
	f32b := f32x4Bytes(f32bVals...)
	f64a := f64x2Bytes(f64aVals...)
	f64b := f64x2Bytes(f64bVals...)
	nan32 := float32(math.NaN())
	f32nanA := f32x4Bytes(nan32, 1, nan32, 2)
	f32nanB := f32x4Bytes(1, nan32, nan32, 2)
	f64nanA := f64x2Bytes(math.NaN(), 2)
	f64nanB := f64x2Bytes(1, 2)

	cases := []struct {
		name string
		a    [16]byte
		b    [16]byte
		sub  uint32
		want [16]byte
	}{
		{"f32x4.add", f32a, f32b, 228, f32x4Bytes(f32aVals[0]+f32bVals[0], f32aVals[1]+f32bVals[1], f32aVals[2]+f32bVals[2], f32aVals[3]+f32bVals[3])},
		{"f32x4.sub", f32a, f32b, 229, f32x4Bytes(f32aVals[0]-f32bVals[0], f32aVals[1]-f32bVals[1], f32aVals[2]-f32bVals[2], f32aVals[3]-f32bVals[3])},
		{"f32x4.mul", f32a, f32b, 230, f32x4Bytes(f32aVals[0]*f32bVals[0], f32aVals[1]*f32bVals[1], f32aVals[2]*f32bVals[2], f32aVals[3]*f32bVals[3])},
		{"f32x4.div", f32a, f32b, 231, f32x4Bytes(f32aVals[0]/f32bVals[0], f32aVals[1]/f32bVals[1], f32aVals[2]/f32bVals[2], f32aVals[3]/f32bVals[3])},
		{"f32x4.eq", f32a, f32b, 65, cmpMaskBytes(4, false, false, false, false)},
		{"f32x4.ne", f32a, f32b, 66, cmpMaskBytes(4, true, true, true, true)},
		{"f32x4.lt", f32a, f32b, 67, cmpMaskBytes(4, true, false, false, true)},
		{"f32x4.gt", f32a, f32b, 68, cmpMaskBytes(4, false, true, true, false)},
		{"f32x4.le", f32a, f32b, 69, cmpMaskBytes(4, true, false, false, true)},
		{"f32x4.ge", f32a, f32b, 70, cmpMaskBytes(4, false, true, true, false)},
		{"f32x4.eq_nan", f32nanA, f32nanB, 65, cmpMaskBytes(4, false, false, false, true)},
		{"f32x4.ne_nan", f32nanA, f32nanB, 66, cmpMaskBytes(4, true, true, true, false)},
		{"f32x4.lt_nan", f32nanA, f32nanB, 67, cmpMaskBytes(4, false, false, false, false)},
		{"f32x4.ge_nan", f32nanA, f32nanB, 70, cmpMaskBytes(4, false, false, false, true)},

		{"f64x2.add", f64a, f64b, 240, f64x2Bytes(f64aVals[0]+f64bVals[0], f64aVals[1]+f64bVals[1])},
		{"f64x2.sub", f64a, f64b, 241, f64x2Bytes(f64aVals[0]-f64bVals[0], f64aVals[1]-f64bVals[1])},
		{"f64x2.mul", f64a, f64b, 242, f64x2Bytes(f64aVals[0]*f64bVals[0], f64aVals[1]*f64bVals[1])},
		{"f64x2.div", f64a, f64b, 243, f64x2Bytes(f64aVals[0]/f64bVals[0], f64aVals[1]/f64bVals[1])},
		{"f64x2.eq", f64a, f64b, 71, cmpMaskBytes(8, false, false)},
		{"f64x2.ne", f64a, f64b, 72, cmpMaskBytes(8, true, true)},
		{"f64x2.lt", f64a, f64b, 73, cmpMaskBytes(8, true, true)},
		{"f64x2.gt", f64a, f64b, 74, cmpMaskBytes(8, false, false)},
		{"f64x2.le", f64a, f64b, 75, cmpMaskBytes(8, true, true)},
		{"f64x2.ge", f64a, f64b, 76, cmpMaskBytes(8, false, false)},
		{"f64x2.eq_nan", f64nanA, f64nanB, 71, cmpMaskBytes(8, false, true)},
		{"f64x2.ne_nan", f64nanA, f64nanB, 72, cmpMaskBytes(8, true, false)},
		{"f64x2.lt_nan", f64nanA, f64nanB, 73, cmpMaskBytes(8, false, false)},
		{"f64x2.ge_nan", f64nanA, f64nanB, 76, cmpMaskBytes(8, false, true)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := mod1(t, nil, []wasm.ValType{wasm.V128}, v128BinaryBody(tc.a, tc.b, tc.sub))
			if got := runAmd64V128(t, m, nil); got != tc.want {
				t.Fatalf("%s = % x, want % x", tc.name, got, tc.want)
			}
		})
	}
}

func TestSIMDV128FunctionABIUsesSixteenByteSlots(t *testing.T) {
	arg := [16]byte{0xde, 0xad, 0xbe, 0xef, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}
	// i32 params/results remain 8-byte slots around a v128 param.
	body := []byte{0x00, 0x20, 0x01, 0xfd, 0x4d, 0x1a, 0x20, 0x02, 0x0b} // local.get v128; v128.not; drop; local.get trailing i32
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.V128, wasm.I32}, []wasm.ValType{wasm.I32}, body)
	cm, err := CompileModule(m)
	if err != nil {
		t.Fatalf("amd64 compile: %v", err)
	}
	eng, err := runtime.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	jm, err := runtime.NewJobMemory(65536)
	if err != nil {
		t.Fatal(err)
	}
	defer jm.Close()
	ar, err := runtime.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	defer ar.Close()
	code, entry, err := runtime.MapCode(cm.Code)
	if err != nil {
		t.Fatalf("map: %v", err)
	}
	defer runtime.Unmap(code)
	serArgs := ar.Alloc(256)
	results := ar.Alloc(256)
	trap := ar.Alloc(8)
	binary.LittleEndian.PutUint32(serArgs[0:4], 0x11111111)
	copy(serArgs[8:24], arg[:])
	binary.LittleEndian.PutUint32(serArgs[24:28], 0x76543210)
	if err := eng.Call(entry+uintptr(cm.Entry[0]), serArgs, jm.LinearMemory(), trap, results); err != nil {
		t.Fatalf("call: %v", err)
	}
	if got := binary.LittleEndian.Uint32(results[:4]); got != 0x76543210 {
		t.Fatalf("trailing i32 result = %#x, want 0x76543210", got)
	}
}
