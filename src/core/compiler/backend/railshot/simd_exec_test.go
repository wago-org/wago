//go:build linux && amd64

package amd64

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoderamd64 "github.com/wago-org/wago/src/core/encoder/amd64"
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

func simdMemarg(sub, align, off uint32) []byte {
	body := []byte{0xfd}
	body = append(body, wasmtest.ULEB(sub)...)
	body = append(body, wasmtest.ULEB(align)...)
	body = append(body, wasmtest.ULEB(off)...)
	return body
}

func v128BinaryBody(a, b [16]byte, sub uint32) []byte {
	body := []byte{0x00}
	body = append(body, v128ConstBytes(a)...)
	body = append(body, v128ConstBytes(b)...)
	body = append(body, simdOp(sub)...)
	body = append(body, 0x0b)
	return body
}

func v128TernaryBody(a, b, c [16]byte, sub uint32) []byte {
	body := []byte{0x00}
	body = append(body, v128ConstBytes(a)...)
	body = append(body, v128ConstBytes(b)...)
	body = append(body, v128ConstBytes(c)...)
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

func f32x4Bits(v ...uint32) [16]byte {
	var out [16]byte
	for i, x := range v {
		binary.LittleEndian.PutUint32(out[i*4:], x)
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

func f64x2Bits(v ...uint64) [16]byte {
	var out [16]byte
	for i, x := range v {
		binary.LittleEndian.PutUint64(out[i*8:], x)
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

func runAmd64ResultBuffer(t *testing.T, m *wasm.Module, setup func(*runtime.JobMemory, []byte, uintptr, *encoderamd64.CompiledModule)) []byte {
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
	if setup != nil {
		setup(jm, serArgs, entry, cm)
	}
	if err := eng.Call(entry+uintptr(cm.Entry[0]), serArgs, jm.LinearMemory(), trap, results); err != nil {
		t.Fatalf("call: %v", err)
	}
	return append([]byte(nil), results...)
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

func TestSIMDV128InternalCallMixedSignature(t *testing.T) {
	want := [16]byte{0, 1, 4, 9, 16, 25, 36, 49, 64, 81, 100, 121, 144, 169, 196, 225}
	caller := []byte{0x00, 0x41, 0x07}
	caller = append(caller, v128ConstBytes(want)...)
	caller = append(caller, 0x42, 0x09, 0x10, 0x01, 0x0b) // i64.const 9; call 1; end
	callee := []byte{0x00, 0x20, 0x01, 0x0b}              // return the middle v128 param
	m := modFuncs(t,
		funcDef{results: []wasm.ValType{wasm.V128}, body: caller},
		funcDef{params: []wasm.ValType{wasm.I32, wasm.V128, wasm.I64}, results: []wasm.ValType{wasm.V128}, body: callee},
	)
	if got := runAmd64V128(t, m, nil); got != want {
		t.Fatalf("v128 internal call result = % x, want % x", got, want)
	}
}

func TestSIMDV128IndirectCallMixedSignature(t *testing.T) {
	want := [16]byte{0, 1, 4, 9, 16, 25, 36, 49, 64, 81, 100, 121, 144, 169, 196, 225}
	caller := []byte{0x00}
	caller = append(caller, v128ConstBytes(want)...)
	caller = append(caller, 0x41, 0x00)             // table index
	caller = append(caller, 0x11, 0x01, 0x00, 0x0b) // call_indirect type 1 table 0; end
	callee := []byte{0x00, 0x20, 0x00, 0x0b}        // return the v128 param

	func0 := append(wasmtest.ULEB(uint32(len(caller))), caller...)
	func1 := append(wasmtest.ULEB(uint32(len(callee))), callee...)
	modBytes := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.V128}),
			wasmtest.FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x01})), // funcref table min 1
		wasmtest.Section(10, wasmtest.Vec(func0, func1)),
	)
	m, err := wasm.DecodeModule(modBytes)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	out := runAmd64ResultBuffer(t, m, func(jm *runtime.JobMemory, serArgs []byte, entry uintptr, cm *encoderamd64.CompiledModule) {
		desc := serArgs[128:152]
		binary.LittleEndian.PutUint32(desc[0:], 1)
		binary.LittleEndian.PutUint64(desc[8:], uint64(entry)+uint64(cm.Entry[1]))
		binary.LittleEndian.PutUint32(desc[16:], m.CanonicalTypeID(1))
		jm.SetTablePtr(uintptr(unsafe.Pointer(&desc[0])))
	})
	var got [16]byte
	copy(got[:], out[:16])
	if got != want {
		t.Fatalf("v128 indirect call result = % x, want % x", got, want)
	}
}

func TestSIMDV128MultiResultWrapperSlots(t *testing.T) {
	vec := [16]byte{0xde, 0xad, 0xbe, 0xef, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}
	callee := append([]byte{0x00, 0x41}, wasmtest.SLEB32(0x12345678)...)
	callee = append(callee, v128ConstBytes(vec)...)
	callee = append(callee, 0x42)
	callee = append(callee, wasmtest.SLEB64(0x1122334455667788)...)
	callee = append(callee, 0x0b)
	m := modFuncs(t,
		funcDef{results: []wasm.ValType{wasm.I32, wasm.V128, wasm.I64}, body: []byte{0x00, 0x10, 0x01, 0x0b}},
		funcDef{results: []wasm.ValType{wasm.I32, wasm.V128, wasm.I64}, body: callee},
	)
	out := runAmd64ResultBuffer(t, m, nil)
	if got := binary.LittleEndian.Uint32(out[0:4]); got != 0x12345678 {
		t.Fatalf("multi-result i32 slot = %#x, want 0x12345678", got)
	}
	var gotVec [16]byte
	copy(gotVec[:], out[8:24])
	if gotVec != vec {
		t.Fatalf("multi-result v128 slot = % x, want % x", gotVec, vec)
	}
	if got := binary.LittleEndian.Uint64(out[24:32]); got != 0x1122334455667788 {
		t.Fatalf("multi-result i64 slot = %#x, want 0x1122334455667788", got)
	}
}

func TestSIMDI8x16Swizzle(t *testing.T) {
	src := i8x16Bytes(0, 11, 22, 33, 44, 55, 66, 77, 88, 99, 111, 122, -123, -112, -101, -90)
	idx := [16]byte{15, 14, 13, 12, 0, 1, 2, 3, 16, 17, 31, 127, 128, 129, 254, 255}
	want := [16]byte{166, 155, 144, 133, 0, 11, 22, 33, 0, 0, 0, 0, 0, 0, 0, 0}
	body := v128BinaryBody(src, idx, 14)
	m := mod1(t, nil, []wasm.ValType{wasm.V128}, body)
	if got := runAmd64V128(t, m, nil); got != want {
		t.Fatalf("i8x16.swizzle = % x, want % x", got, want)
	}
}

func TestSIMDI8x16RelaxedSwizzle(t *testing.T) {
	src := i8x16Bytes(0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15)
	idx := i8x16Bytes(0, 1, 15, 16, 17, 31, 32, 127, -128, -127, -2, -1, 2, 3, 4, 5)
	want := i8x16Bytes(0, 1, 15, 0, 1, 15, 0, 15, 0, 0, 0, 0, 2, 3, 4, 5)
	m := mod1(t, nil, []wasm.ValType{wasm.V128}, v128BinaryBody(src, idx, 256))
	if got := runAmd64V128(t, m, nil); got != want {
		t.Fatalf("i8x16.relaxed_swizzle = % x, want % x", got, want)
	}
}

func TestSIMDRelaxedLaneSelect(t *testing.T) {
	a := i8x16Bytes(0x10, 0x11, 0x12, 0x13, 0x20, 0x21, 0x22, 0x23, 0x30, 0x31, 0x32, 0x33, 0x40, 0x41, 0x42, 0x43)
	b := i8x16Bytes(-16, -15, -14, -13, -32, -31, -30, -29, -48, -47, -46, -45, -64, -63, -62, -61)
	cases := []struct {
		name string
		sub  uint32
		mask [16]byte
	}{
		{"i8x16.relaxed_laneselect", 265, cmpMaskBytes(1, true, false, true, false, false, true, false, true, true, false, false, true, false, true, true, false)},
		{"i16x8.relaxed_laneselect", 266, cmpMaskBytes(2, true, false, true, false, false, true, false, true)},
		{"i32x4.relaxed_laneselect", 267, cmpMaskBytes(4, true, false, false, true)},
		{"i64x2.relaxed_laneselect", 268, cmpMaskBytes(8, false, true)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var want [16]byte
			for i := range want {
				want[i] = (a[i] & tc.mask[i]) | (b[i] &^ tc.mask[i])
			}
			body := []byte{0x00}
			body = append(body, v128ConstBytes(a)...)
			body = append(body, v128ConstBytes(b)...)
			body = append(body, v128ConstBytes(tc.mask)...)
			body = append(body, simdOp(tc.sub)...)
			body = append(body, 0x0b)
			m := mod1(t, nil, []wasm.ValType{wasm.V128}, body)
			if got := runAmd64V128(t, m, nil); got != want {
				t.Fatalf("%s = % x, want % x", tc.name, got, want)
			}
		})
	}
}

func TestSIMDRelaxedMaddNmadd(t *testing.T) {
	f32a := f32x4Bytes(2, -3, 5, 10)
	f32b := f32x4Bytes(4, 7, -2, 0.25)
	f32c := f32x4Bytes(1, 100, 3, -8)
	f64a := f64x2Bytes(2, -3)
	f64b := f64x2Bytes(4, 7)
	f64c := f64x2Bytes(1, 100)

	cases := []struct {
		name string
		a    [16]byte
		b    [16]byte
		c    [16]byte
		sub  uint32
		want [16]byte
	}{
		{"f32x4.relaxed_madd", f32a, f32b, f32c, 261, f32x4Bytes(9, 79, -7, -5.5)},
		{"f32x4.relaxed_nmadd", f32a, f32b, f32c, 262, f32x4Bytes(-7, 121, 13, -10.5)},
		{"f64x2.relaxed_madd", f64a, f64b, f64c, 263, f64x2Bytes(9, 79)},
		{"f64x2.relaxed_nmadd", f64a, f64b, f64c, 264, f64x2Bytes(-7, 121)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := mod1(t, nil, []wasm.ValType{wasm.V128}, v128TernaryBody(tc.a, tc.b, tc.c, tc.sub))
			if got := runAmd64V128(t, m, nil); got != tc.want {
				t.Fatalf("%s = % x, want % x", tc.name, got, tc.want)
			}
		})
	}
}

func TestSIMDRelaxedTruncations(t *testing.T) {
	cases := []struct {
		name string
		src  [16]byte
		sub  uint32
		want [16]byte
	}{
		{
			name: "i32x4.relaxed_trunc_f32x4_s",
			src:  f32x4Bytes(float32(math.NaN()), float32(math.Inf(1)), float32(math.Inf(-1)), -3.9),
			sub:  257,
			want: i32x4Bytes(0, 0x7fffffff, -0x80000000, -3),
		},
		{
			name: "i32x4.relaxed_trunc_f32x4_u",
			src:  f32x4Bytes(float32(math.NaN()), -1, float32(math.Inf(1)), 42.9),
			sub:  258,
			want: i32x4Bytes(0, 0, -1, 42),
		},
		{
			name: "i32x4.relaxed_trunc_f64x2_s_zero",
			src:  f64x2Bytes(math.NaN(), math.Inf(1)),
			sub:  259,
			want: i32x4Bytes(0, 0x7fffffff, 0, 0),
		},
		{
			name: "i32x4.relaxed_trunc_f64x2_u_zero",
			src:  f64x2Bytes(-1, math.Inf(1)),
			sub:  260,
			want: i32x4Bytes(0, -1, 0, 0),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := mod1(t, nil, []wasm.ValType{wasm.V128}, append(append(append([]byte{0x00}, v128ConstBytes(tc.src)...), simdOp(tc.sub)...), 0x0b))
			if got := runAmd64V128(t, m, nil); got != tc.want {
				t.Fatalf("%s = % x, want % x", tc.name, got, tc.want)
			}
		})
	}
}

func f32LaneBits(v [16]byte, lane int) uint32 {
	return binary.LittleEndian.Uint32(v[lane*4:])
}

func f64LaneBits(v [16]byte, lane int) uint64 {
	return binary.LittleEndian.Uint64(v[lane*8:])
}

func requireF32Bits(t *testing.T, got [16]byte, lane int, want uint32) {
	t.Helper()
	if bits := f32LaneBits(got, lane); bits != want {
		t.Fatalf("f32 lane %d bits = 0x%08x, want 0x%08x (vector % x)", lane, bits, want, got)
	}
}

func requireF32NaN(t *testing.T, got [16]byte, lane int) {
	t.Helper()
	if bits := f32LaneBits(got, lane); !math.IsNaN(float64(math.Float32frombits(bits))) {
		t.Fatalf("f32 lane %d bits = 0x%08x, want NaN (vector % x)", lane, bits, got)
	}
}

func requireF64Bits(t *testing.T, got [16]byte, lane int, want uint64) {
	t.Helper()
	if bits := f64LaneBits(got, lane); bits != want {
		t.Fatalf("f64 lane %d bits = 0x%016x, want 0x%016x (vector % x)", lane, bits, want, got)
	}
}

func requireF64NaN(t *testing.T, got [16]byte, lane int) {
	t.Helper()
	if bits := f64LaneBits(got, lane); !math.IsNaN(math.Float64frombits(bits)) {
		t.Fatalf("f64 lane %d bits = 0x%016x, want NaN (vector % x)", lane, bits, got)
	}
}

func TestSIMDCorePackedFloatConversions(t *testing.T) {
	truncCases := []struct {
		name string
		src  [16]byte
		sub  uint32
		want [16]byte
	}{
		{
			name: "i32x4.trunc_sat_f32x4_s",
			src:  f32x4Bytes(float32(math.NaN()), float32(math.Inf(1)), float32(math.Inf(-1)), -1.9),
			sub:  248,
			want: i32x4Bytes(0, math.MaxInt32, math.MinInt32, -1),
		},
		{
			name: "i32x4.trunc_sat_f32x4_u",
			src:  f32x4Bytes(float32(math.NaN()), -1.0, float32(math.Inf(1)), 1.9),
			sub:  249,
			want: i32x4Bytes(0, 0, -1, 1),
		},
		{
			name: "i32x4.trunc_sat_f64x2_s_zero",
			src:  f64x2Bytes(math.Inf(1), -1.9),
			sub:  252,
			want: i32x4Bytes(math.MaxInt32, -1, 0, 0),
		},
		{
			name: "i32x4.trunc_sat_f64x2_u_zero",
			src:  f64x2Bytes(math.NaN(), math.Inf(1)),
			sub:  253,
			want: i32x4Bytes(0, -1, 0, 0),
		},
	}
	for _, tc := range truncCases {
		t.Run(tc.name, func(t *testing.T) {
			body := append(append(append([]byte{0x00}, v128ConstBytes(tc.src)...), simdOp(tc.sub)...), 0x0b)
			m := mod1(t, nil, []wasm.ValType{wasm.V128}, body)
			if got := runAmd64V128(t, m, nil); got != tc.want {
				t.Fatalf("%s = % x, want % x", tc.name, got, tc.want)
			}
		})
	}

	convertCases := []struct {
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
	for _, tc := range convertCases {
		t.Run(tc.name, func(t *testing.T) {
			body := append(append(append([]byte{0x00}, v128ConstBytes(tc.src)...), simdOp(tc.sub)...), 0x0b)
			m := mod1(t, nil, []wasm.ValType{wasm.V128}, body)
			if got := runAmd64V128(t, m, nil); got != tc.want {
				t.Fatalf("%s = % x, want % x", tc.name, got, tc.want)
			}
		})
	}
}

func TestSIMDCorePackedFloatLaneWidthConversions(t *testing.T) {
	t.Run("f32x4.demote_f64x2_zero low lanes and zero high lanes", func(t *testing.T) {
		m := mod1(t, nil, []wasm.ValType{wasm.V128}, append(append(append([]byte{0x00}, v128ConstBytes(f64x2Bytes(1.5, -2.75))...), simdOp(94)...), 0x0b))
		got := runAmd64V128(t, m, nil)
		requireF32Bits(t, got, 0, math.Float32bits(1.5))
		requireF32Bits(t, got, 1, math.Float32bits(-2.75))
		requireF32Bits(t, got, 2, math.Float32bits(0))
		requireF32Bits(t, got, 3, math.Float32bits(0))
	})

	t.Run("f32x4.demote_f64x2_zero preserves special predicates", func(t *testing.T) {
		m := mod1(t, nil, []wasm.ValType{wasm.V128}, append(append(append([]byte{0x00}, v128ConstBytes(f64x2Bytes(math.Copysign(0, -1), math.NaN()))...), simdOp(94)...), 0x0b))
		got := runAmd64V128(t, m, nil)
		requireF32Bits(t, got, 0, math.Float32bits(float32(math.Copysign(0, -1))))
		requireF32NaN(t, got, 1)
		requireF32Bits(t, got, 2, math.Float32bits(0))
		requireF32Bits(t, got, 3, math.Float32bits(0))
	})

	t.Run("f64x2.promote_low_f32x4 ignores high f32 lanes", func(t *testing.T) {
		src := f32x4Bytes(float32(math.Copysign(0, -1)), float32(math.Inf(1)), 1234, float32(math.NaN()))
		m := mod1(t, nil, []wasm.ValType{wasm.V128}, append(append(append([]byte{0x00}, v128ConstBytes(src)...), simdOp(95)...), 0x0b))
		got := runAmd64V128(t, m, nil)
		requireF64Bits(t, got, 0, math.Float64bits(math.Copysign(0, -1)))
		requireF64Bits(t, got, 1, math.Float64bits(math.Inf(1)))
	})

	t.Run("f64x2.promote_low_f32x4 preserves NaN predicate", func(t *testing.T) {
		src := f32x4Bits(math.Float32bits(float32(math.NaN())), math.Float32bits(-2.5), math.Float32bits(float32(math.Inf(-1))), math.Float32bits(99))
		m := mod1(t, nil, []wasm.ValType{wasm.V128}, append(append(append([]byte{0x00}, v128ConstBytes(src)...), simdOp(95)...), 0x0b))
		got := runAmd64V128(t, m, nil)
		requireF64NaN(t, got, 0)
		requireF64Bits(t, got, 1, math.Float64bits(-2.5))
	})
}

func TestSIMDRelaxedQ15mulr(t *testing.T) {
	a := i16x8Bytes(32767, -32768, -32768, 16384, -16384, 12345, -12345, 30000)
	b := i16x8Bytes(32767, -32768, 32767, 16384, 16384, -23456, -23456, 2)
	// Deterministic relaxed choice: raw PMULHRSW. Unlike core q15mulr_sat_s,
	// the min*min lane remains -32768 instead of being patched to +32767.
	want := i16x8Bytes(32766, -32768, -32767, 8192, -8192, -8837, 8837, 2)
	m := mod1(t, nil, []wasm.ValType{wasm.V128}, v128BinaryBody(a, b, 273))
	if got := runAmd64V128(t, m, nil); got != want {
		t.Fatalf("i16x8.relaxed_q15mulr_s = % x, want % x", got, want)
	}
}

func TestSIMDI8x16Shuffle(t *testing.T) {
	a := i8x16Bytes(0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15)
	b := i8x16Bytes(100, 101, 102, 103, 104, 105, 106, 107, 108, 109, 110, 111, 112, 113, 114, 115)
	lanes := [16]byte{0, 16, 1, 17, 15, 31, 8, 24, 4, 20, 5, 21, 7, 23, 10, 26}
	want := [16]byte{0, 100, 1, 101, 15, 115, 8, 108, 4, 104, 5, 105, 7, 107, 10, 110}
	body := []byte{0x00}
	body = append(body, v128ConstBytes(a)...)
	body = append(body, v128ConstBytes(b)...)
	body = append(body, simdOp(13)...)
	body = append(body, lanes[:]...)
	body = append(body, 0x0b)
	m := mod1(t, nil, []wasm.ValType{wasm.V128}, body)
	if got := runAmd64V128(t, m, nil); got != want {
		t.Fatalf("i8x16.shuffle = % x, want % x", got, want)
	}
}

func TestSIMDV128LoadExtends(t *testing.T) {
	cases := []struct {
		name  string
		sub   uint32
		align uint32
		data  []byte
		want  [16]byte
	}{
		{"v128.load8x8_s", 1, 3, []byte{0x00, 0x01, 0x7f, 0x80, 0xff, 0xa5, 0x34, 0xfe}, i16x8Bytes(0, 1, 127, -128, -1, -91, 52, -2)},
		{"v128.load8x8_u", 2, 3, []byte{0x00, 0x01, 0x7f, 0x80, 0xff, 0xa5, 0x34, 0xfe}, i16x8Bytes(0, 1, 127, 128, 255, 165, 52, 254)},
		{"v128.load16x4_s", 3, 3, []byte{0x00, 0x00, 0xff, 0x7f, 0x00, 0x80, 0x34, 0xff}, i32x4Bytes(0, 32767, -32768, -204)},
		{"v128.load16x4_u", 4, 3, []byte{0x00, 0x00, 0xff, 0x7f, 0x00, 0x80, 0x34, 0xff}, i32x4Bytes(0, 32767, 32768, 65332)},
		{"v128.load32x2_s", 5, 3, []byte{0xff, 0xff, 0xff, 0x7f, 0x00, 0x00, 0x00, 0x80}, i64x2Bytes(2147483647, -2147483648)},
		{"v128.load32x2_u", 6, 3, []byte{0xff, 0xff, 0xff, 0x7f, 0x00, 0x00, 0x00, 0x80}, i64x2Bytes(2147483647, 2147483648)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			const addr = 52
			const off = 9
			body := []byte{0x00, 0x41, addr}
			body = append(body, simdMemarg(tc.sub, tc.align, off)...)
			body = append(body, 0x0b)
			m := modMem(t, 1, nil, []wasm.ValType{wasm.V128}, body)
			got, _, err := runMemAmd64V128(t, m, func(mem []byte) { copy(mem[addr+off:], tc.data) })
			if err != nil {
				t.Fatalf("call: %v", err)
			}
			if got != tc.want {
				t.Fatalf("%s = % x, want % x", tc.name, got, tc.want)
			}
		})
	}

	t.Run("load extends trap on eight-byte source", func(t *testing.T) {
		body := []byte{0x00, 0x41, 0xff, 0xff, 0x03} // i32.const 65535
		body = append(body, simdMemarg(1, 0, 0)...)
		body = append(body, 0x0b)
		m := modMem(t, 1, nil, []wasm.ValType{wasm.V128}, body)
		if _, _, err := runMemAmd64V128(t, m, nil); err == nil {
			t.Fatal("expected v128.load8x8_s out-of-bounds trap")
		}
	})
}

func TestSIMDV128LoadSplats(t *testing.T) {
	cases := []struct {
		name  string
		sub   uint32
		size  int
		align uint32
		data  []byte
		want  [16]byte
	}{
		{"v128.load8_splat", 7, 1, 0, []byte{0xa5}, [16]byte{0xa5, 0xa5, 0xa5, 0xa5, 0xa5, 0xa5, 0xa5, 0xa5, 0xa5, 0xa5, 0xa5, 0xa5, 0xa5, 0xa5, 0xa5, 0xa5}},
		{"v128.load16_splat", 8, 2, 1, []byte{0x34, 0x12}, [16]byte{0x34, 0x12, 0x34, 0x12, 0x34, 0x12, 0x34, 0x12, 0x34, 0x12, 0x34, 0x12, 0x34, 0x12, 0x34, 0x12}},
		{"v128.load32_splat", 9, 4, 2, []byte{0x78, 0x56, 0x34, 0x12}, [16]byte{0x78, 0x56, 0x34, 0x12, 0x78, 0x56, 0x34, 0x12, 0x78, 0x56, 0x34, 0x12, 0x78, 0x56, 0x34, 0x12}},
		{"v128.load64_splat", 10, 8, 3, []byte{0x88, 0x77, 0x66, 0x55, 0x44, 0x33, 0x22, 0x11}, [16]byte{0x88, 0x77, 0x66, 0x55, 0x44, 0x33, 0x22, 0x11, 0x88, 0x77, 0x66, 0x55, 0x44, 0x33, 0x22, 0x11}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			const addr = 48
			const off = 7
			body := []byte{0x00, 0x41, addr}
			body = append(body, simdMemarg(tc.sub, tc.align, off)...)
			body = append(body, 0x0b)
			m := modMem(t, 1, nil, []wasm.ValType{wasm.V128}, body)
			got, _, err := runMemAmd64V128(t, m, func(mem []byte) { copy(mem[addr+off:], tc.data) })
			if err != nil {
				t.Fatalf("call: %v", err)
			}
			if got != tc.want {
				t.Fatalf("%s = % x, want % x", tc.name, got, tc.want)
			}
		})
	}

	t.Run("load splat traps use scalar width", func(t *testing.T) {
		body := []byte{0x00, 0x41, 0xff, 0xff, 0x03} // i32.const 65535
		body = append(body, simdMemarg(10, 0, 0)...)
		body = append(body, 0x0b)
		m := modMem(t, 1, nil, []wasm.ValType{wasm.V128}, body)
		if _, _, err := runMemAmd64V128(t, m, nil); err == nil {
			t.Fatal("expected v128.load64_splat out-of-bounds trap")
		}
	})
}

func TestSIMDV128LoadZero(t *testing.T) {
	cases := []struct {
		name  string
		sub   uint32
		size  int
		align uint32
		data  []byte
		want  [16]byte
	}{
		{"v128.load32_zero", 92, 4, 2, []byte{0xef, 0xcd, 0xab, 0x89}, [16]byte{0xef, 0xcd, 0xab, 0x89}},
		{"v128.load64_zero", 93, 8, 3, []byte{0x88, 0x77, 0x66, 0x55, 0x44, 0x33, 0x22, 0x11}, [16]byte{0x88, 0x77, 0x66, 0x55, 0x44, 0x33, 0x22, 0x11}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			const addr = 40
			const off = 11
			body := []byte{0x00, 0x41, addr}
			body = append(body, simdMemarg(tc.sub, tc.align, off)...)
			body = append(body, 0x0b)
			m := modMem(t, 1, nil, []wasm.ValType{wasm.V128}, body)
			got, _, err := runMemAmd64V128(t, m, func(mem []byte) {
				for i := range mem[:128] {
					mem[i] = 0xff
				}
				copy(mem[addr+off:], tc.data)
			})
			if err != nil {
				t.Fatalf("call: %v", err)
			}
			if got != tc.want {
				t.Fatalf("%s = % x, want % x", tc.name, got, tc.want)
			}
		})
	}

	t.Run("load zero traps use scalar width", func(t *testing.T) {
		body := []byte{0x00, 0x41, 0xff, 0xff, 0x03} // i32.const 65535
		body = append(body, simdMemarg(92, 0, 0)...)
		body = append(body, 0x0b)
		m := modMem(t, 1, nil, []wasm.ValType{wasm.V128}, body)
		if _, _, err := runMemAmd64V128(t, m, nil); err == nil {
			t.Fatal("expected v128.load32_zero out-of-bounds trap")
		}
	})
}

func TestSIMDV128LaneMemoryOps(t *testing.T) {
	base := [16]byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}
	laneMemarg := func(sub uint32, align, off uint32, lane byte) []byte {
		body := []byte{0xfd}
		body = append(body, wasmtest.ULEB(sub)...)
		body = append(body, wasmtest.ULEB(align)...)
		body = append(body, wasmtest.ULEB(off)...)
		body = append(body, lane)
		return body
	}
	t.Run("load lanes replace only selected lane", func(t *testing.T) {
		cases := []struct {
			name  string
			sub   uint32
			size  int
			align uint32
			lane  byte
		}{
			{"v128.load8_lane", 84, 1, 0, 13},
			{"v128.load16_lane", 85, 2, 1, 6},
			{"v128.load32_lane", 86, 4, 2, 2},
			{"v128.load64_lane", 87, 8, 3, 1},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				const addr = 32
				const off = 5
				body := []byte{0x00, 0x41, addr}
				body = append(body, v128ConstBytes(base)...)
				body = append(body, laneMemarg(tc.sub, tc.align, off, tc.lane)...)
				body = append(body, 0x0b)
				m := modMem(t, 1, nil, []wasm.ValType{wasm.V128}, body)
				var want [16]byte
				copy(want[:], base[:])
				got, _, err := runMemAmd64V128(t, m, func(mem []byte) {
					for i := 0; i < tc.size; i++ {
						mem[addr+off+i] = byte(0xa0 + i + tc.size)
						want[int(tc.lane)*tc.size+i] = mem[addr+off+i]
					}
				})
				if err != nil {
					t.Fatalf("call: %v", err)
				}
				if got != want {
					t.Fatalf("%s = % x, want % x", tc.name, got, want)
				}
			})
		}
	})

	t.Run("store lanes write only selected lane", func(t *testing.T) {
		cases := []struct {
			name  string
			sub   uint32
			size  int
			align uint32
			lane  byte
		}{
			{"v128.store8_lane", 88, 1, 0, 14},
			{"v128.store16_lane", 89, 2, 1, 5},
			{"v128.store32_lane", 90, 4, 2, 3},
			{"v128.store64_lane", 91, 8, 3, 1},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				const addr = 40
				const off = 3
				body := []byte{0x00, 0x41, addr}
				body = append(body, v128ConstBytes(base)...)
				body = append(body, laneMemarg(tc.sub, tc.align, off, tc.lane)...)
				body = append(body, 0x0b)
				m := modMem(t, 1, nil, nil, body)
				_, mem, err := runMemAmd64V128(t, m, func(mem []byte) {
					for i := range mem[:64] {
						mem[i] = 0xaa
					}
				})
				if err != nil {
					t.Fatalf("call: %v", err)
				}
				for i := 0; i < tc.size; i++ {
					want := base[int(tc.lane)*tc.size+i]
					if got := mem[addr+off+i]; got != want {
						t.Fatalf("%s byte %d = 0x%02x, want 0x%02x", tc.name, i, got, want)
					}
				}
				if mem[addr+off-1] != 0xaa || mem[addr+off+tc.size] != 0xaa {
					t.Fatalf("%s clobbered neighboring bytes: before=0x%02x after=0x%02x", tc.name, mem[addr+off-1], mem[addr+off+tc.size])
				}
			})
		}
	})

	t.Run("lane memory traps use lane width", func(t *testing.T) {
		loadBody := []byte{0x00, 0x41, 0xff, 0xff, 0x03} // i32.const 65535
		loadBody = append(loadBody, v128ConstBytes(base)...)
		loadBody = append(loadBody, laneMemarg(85, 0, 0, 0)...)
		loadBody = append(loadBody, 0x0b)
		loadMod := modMem(t, 1, nil, []wasm.ValType{wasm.V128}, loadBody)
		if _, _, err := runMemAmd64V128(t, loadMod, nil); err == nil {
			t.Fatal("expected v128.load16_lane out-of-bounds trap")
		}

		storeBody := []byte{0x00, 0x41, 0xff, 0xff, 0x03} // i32.const 65535
		storeBody = append(storeBody, v128ConstBytes(base)...)
		storeBody = append(storeBody, laneMemarg(89, 0, 0, 0)...)
		storeBody = append(storeBody, 0x0b)
		storeMod := modMem(t, 1, nil, nil, storeBody)
		if _, _, err := runMemAmd64V128(t, storeMod, nil); err == nil {
			t.Fatal("expected v128.store16_lane out-of-bounds trap")
		}
	})
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
		{"i64x2.abs", i64x2Bytes(-9223372036854775808, -1234567890123), 192, i64x2Bytes(-9223372036854775808, 1234567890123)},
		{"i8x16.abs", i8x16Bytes(-128, -127, -2, -1, 0, 1, 2, 3, -4, 5, -6, 7, -8, 9, -10, 127), 96, i8x16Bytes(-128, 127, 2, 1, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 127)},
		{"i8x16.popcnt", i8x16Bytes(0x00, 0x01, 0x03, 0x07, 0x0f, 0x10, 0x55, 0x7f, -128, -127, -86, -1, 0x24, 0x42, 0x66, 0x7e), 98, i8x16Bytes(0, 1, 2, 3, 4, 1, 4, 7, 1, 2, 4, 8, 2, 2, 4, 6)},
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

func TestSIMDIntegerExtends(t *testing.T) {
	cases := []struct {
		name string
		in   [16]byte
		sub  uint32
		want [16]byte
	}{
		{"i16x8.extadd_pairwise_i8x16_s", i8x16Bytes(1, -2, 127, -128, -1, -1, 100, 50, -100, 20, 0, 0, 64, 63, -64, -64), 124, i16x8Bytes(-1, -1, -2, 150, -80, 0, 127, -128)},
		{"i16x8.extadd_pairwise_i8x16_u", i8x16Bytes(1, -2, 127, -128, -1, -1, 100, 50, -100, 20, 0, 0, 64, 63, -64, -64), 125, i16x8Bytes(255, 255, 510, 150, 176, 0, 127, 384)},
		{"i32x4.extadd_pairwise_i16x8_s", i16x8Bytes(1, -2, 32767, -32768, -1, -1, 20000, 12345), 126, i32x4Bytes(-1, -1, -2, 32345)},
		{"i32x4.extadd_pairwise_i16x8_u", i16x8Bytes(1, -2, 32767, -32768, -1, -1, 20000, 12345), 127, i32x4Bytes(65535, 65535, 131070, 32345)},
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

func TestSIMDIntegerExtmul(t *testing.T) {
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
			if got := runAmd64V128(t, m, nil); got != tc.want {
				t.Fatalf("%s = % x, want % x", tc.name, got, tc.want)
			}
		})
	}
}

func TestSIMDIntegerI8x16Shifts(t *testing.T) {
	input := i8x16Bytes(1, 0x40, -2, -128, 0x12, -0x12, 0x7f, -1, 3, -3, 0x55, -0x55, 0x20, -0x20, 0, 5)
	lanes := []int8{1, 0x40, -2, -128, 0x12, -0x12, 0x7f, -1, 3, -3, 0x55, -0x55, 0x20, -0x20, 0, 5}

	want := func(count uint32, op string) [16]byte {
		shift := count & 7
		out := make([]int8, len(lanes))
		for i, v := range lanes {
			switch op {
			case "shl":
				out[i] = int8(uint8(v) << shift)
			case "shr_s":
				out[i] = v >> shift
			case "shr_u":
				out[i] = int8(uint8(v) >> shift)
			}
		}
		return i8x16Bytes(out...)
	}

	cases := []struct {
		name  string
		sub   uint32
		op    string
		count uint32
	}{
		{"shl-0", 107, "shl", 0}, {"shl-7", 107, "shl", 7}, {"shl-8-wraps", 107, "shl", 8}, {"shl-11-wraps", 107, "shl", 11},
		{"shr_s-0", 108, "shr_s", 0}, {"shr_s-7", 108, "shr_s", 7}, {"shr_s-8-wraps", 108, "shr_s", 8}, {"shr_s-11-wraps", 108, "shr_s", 11},
		{"shr_u-0", 109, "shr_u", 0}, {"shr_u-7", 109, "shr_u", 7}, {"shr_u-8-wraps", 109, "shr_u", 8}, {"shr_u-11-wraps", 109, "shr_u", 11},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte{0x00}
			body = append(body, v128ConstBytes(input)...)
			body = append(body, 0x41)
			body = append(body, wasmtest.SLEB32(int32(tc.count))...)
			body = append(body, simdOp(tc.sub)...)
			body = append(body, 0x0b)
			m := mod1(t, nil, []wasm.ValType{wasm.V128}, body)
			got := runAmd64V128(t, m, nil)
			if got != want(tc.count, tc.op) {
				t.Fatalf("got % x want % x", got, want(tc.count, tc.op))
			}
		})
	}
}

func TestSIMDIntegerI16x8Shifts(t *testing.T) {
	input := i16x8Bytes(1, 0x4001, -2, -32768, 0x1234, -0x1234, 0x7fff, -1)
	lanes := []int16{1, 0x4001, -2, -32768, 0x1234, -0x1234, 0x7fff, -1}

	want := func(count uint32, op string) [16]byte {
		shift := count & 15
		out := make([]int16, len(lanes))
		for i, v := range lanes {
			switch op {
			case "shl":
				out[i] = int16(uint16(v) << shift)
			case "shr_s":
				out[i] = v >> shift
			case "shr_u":
				out[i] = int16(uint16(v) >> shift)
			}
		}
		return i16x8Bytes(out...)
	}

	cases := []struct {
		name  string
		sub   uint32
		op    string
		count uint32
	}{
		{"shl-0", 139, "shl", 0}, {"shl-15", 139, "shl", 15}, {"shl-16-wraps", 139, "shl", 16}, {"shl-19-wraps", 139, "shl", 19},
		{"shr_s-0", 140, "shr_s", 0}, {"shr_s-15", 140, "shr_s", 15}, {"shr_s-16-wraps", 140, "shr_s", 16}, {"shr_s-19-wraps", 140, "shr_s", 19},
		{"shr_u-0", 141, "shr_u", 0}, {"shr_u-15", 141, "shr_u", 15}, {"shr_u-16-wraps", 141, "shr_u", 16}, {"shr_u-19-wraps", 141, "shr_u", 19},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte{0x00}
			body = append(body, v128ConstBytes(input)...)
			body = append(body, 0x41)
			body = append(body, wasmtest.SLEB32(int32(tc.count))...)
			body = append(body, simdOp(tc.sub)...)
			body = append(body, 0x0b)
			m := mod1(t, nil, []wasm.ValType{wasm.V128}, body)
			got := runAmd64V128(t, m, nil)
			if got != want(tc.count, tc.op) {
				t.Fatalf("got % x want % x", got, want(tc.count, tc.op))
			}
		})
	}
}

func TestSIMDIntegerI32x4Shifts(t *testing.T) {
	input := i32x4Bytes(1, 0x40000001, -2, -2147483648)
	lanes := []int32{1, 0x40000001, -2, -2147483648}

	want := func(count uint32, op string) [16]byte {
		shift := count & 31
		out := make([]int32, len(lanes))
		for i, v := range lanes {
			switch op {
			case "shl":
				out[i] = int32(uint32(v) << shift)
			case "shr_s":
				out[i] = v >> shift
			case "shr_u":
				out[i] = int32(uint32(v) >> shift)
			}
		}
		return i32x4Bytes(out...)
	}

	cases := []struct {
		name  string
		sub   uint32
		op    string
		count uint32
	}{
		{"shl-0", 171, "shl", 0}, {"shl-31", 171, "shl", 31}, {"shl-32-wraps", 171, "shl", 32}, {"shl-35-wraps", 171, "shl", 35},
		{"shr_s-0", 172, "shr_s", 0}, {"shr_s-31", 172, "shr_s", 31}, {"shr_s-32-wraps", 172, "shr_s", 32}, {"shr_s-35-wraps", 172, "shr_s", 35},
		{"shr_u-0", 173, "shr_u", 0}, {"shr_u-31", 173, "shr_u", 31}, {"shr_u-32-wraps", 173, "shr_u", 32}, {"shr_u-35-wraps", 173, "shr_u", 35},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte{0x00}
			body = append(body, v128ConstBytes(input)...)
			body = append(body, 0x41)
			body = append(body, wasmtest.SLEB32(int32(tc.count))...)
			body = append(body, simdOp(tc.sub)...)
			body = append(body, 0x0b)
			m := mod1(t, nil, []wasm.ValType{wasm.V128}, body)
			got := runAmd64V128(t, m, nil)
			if got != want(tc.count, tc.op) {
				t.Fatalf("got % x want % x", got, want(tc.count, tc.op))
			}
		})
	}
}

func TestSIMDIntegerI64x2Shifts(t *testing.T) {
	input := i64x2Bytes(0x4000000000000001, -2)
	lanes := []int64{0x4000000000000001, -2}

	want := func(count uint32, op string) [16]byte {
		shift := count & 63
		out := make([]int64, len(lanes))
		for i, v := range lanes {
			switch op {
			case "shl":
				out[i] = int64(uint64(v) << shift)
			case "shr_s":
				out[i] = v >> shift
			case "shr_u":
				out[i] = int64(uint64(v) >> shift)
			}
		}
		return i64x2Bytes(out...)
	}

	cases := []struct {
		name  string
		sub   uint32
		op    string
		count uint32
	}{
		{"shl-0", 203, "shl", 0}, {"shl-63", 203, "shl", 63}, {"shl-64-wraps", 203, "shl", 64}, {"shl-67-wraps", 203, "shl", 67},
		{"shr_s-0", 204, "shr_s", 0}, {"shr_s-63", 204, "shr_s", 63}, {"shr_s-64-wraps", 204, "shr_s", 64}, {"shr_s-67-wraps", 204, "shr_s", 67},
		{"shr_u-0", 205, "shr_u", 0}, {"shr_u-63", 205, "shr_u", 63}, {"shr_u-64-wraps", 205, "shr_u", 64}, {"shr_u-67-wraps", 205, "shr_u", 67},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte{0x00}
			body = append(body, v128ConstBytes(input)...)
			body = append(body, 0x41)
			body = append(body, wasmtest.SLEB32(int32(tc.count))...)
			body = append(body, simdOp(tc.sub)...)
			body = append(body, 0x0b)
			m := mod1(t, nil, []wasm.ValType{wasm.V128}, body)
			got := runAmd64V128(t, m, nil)
			if got != want(tc.count, tc.op) {
				t.Fatalf("got % x want % x", got, want(tc.count, tc.op))
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
		{"i8x16.narrow_i16x8_s", i16x8Bytes(200, -200, 127, -128, 0, 50, -50, 30000), i16x8Bytes(-32768, 32767, 1, -1, 128, -129, 100, -100), 101, i8x16Bytes(127, -128, 127, -128, 0, 50, -50, 127, -128, 127, 1, -1, 127, -128, 100, -100)},
		{"i8x16.narrow_i16x8_u", i16x8Bytes(0, 1, 255, 256, -1, -32768, 128, 300), i16x8Bytes(42, 999, -2, 257, 255, 254, 0, 32767), 102, i8x16Bytes(0, 1, -1, -1, -1, -1, -128, -1, 42, -1, -1, -1, -1, -2, 0, -1)},
		{"i8x16.add", i8a, i8b, 110, i8x16Bytes(-126, -127, -1, -10, 0, 126, 0, -128, 0, 41, 60, 81, 0, 121, -115, -95)},
		{"i8x16.add_sat_s", i8x16Bytes(120, 100, -120, -100, 127, -128, 1, -1, 0, 50, -50, 10, -10, 60, -60, 20), i8x16Bytes(10, 50, -20, -50, 1, -1, -2, 1, 0, -100, 100, -20, 20, 70, -70, -20), 111, i8x16Bytes(127, 127, -128, -128, 127, -128, -1, 0, 0, -50, 50, -10, 10, 127, -128, 0)},
		{"i8x16.add_sat_u", i8x16Bytes(120, 100, -120, -100, 127, -128, 1, -1, 0, 50, -50, 10, -10, 60, -60, 20), i8x16Bytes(10, 50, -20, -50, 1, -1, -2, 1, 0, -100, 100, -20, 20, 70, -70, -20), 112, i8x16Bytes(-126, -106, -1, -1, -128, -1, -1, -1, 0, -50, -1, -10, -1, -126, -1, -1)},
		{"i8x16.sub", i8a, i8b, 113, i8x16Bytes(110, 127, 3, 0, 0, -128, -2, 0, 20, -1, 0, -1, 100, -1, -1, -1)},
		{"i8x16.sub_sat_s", i8x16Bytes(120, 100, -120, -100, 127, -128, 1, -1, 0, 50, -50, 10, -10, 60, -60, 20), i8x16Bytes(10, 50, -20, -50, 1, -1, -2, 1, 0, -100, 100, -20, 20, 70, -70, -20), 114, i8x16Bytes(110, 50, -100, -50, 126, -127, 3, -2, 0, 127, -128, 30, -30, -10, 10, 40)},
		{"i8x16.sub_sat_u", i8x16Bytes(120, 100, -120, -100, 127, -128, 1, -1, 0, 50, -50, 10, -10, 60, -60, 20), i8x16Bytes(10, 50, -20, -50, 1, -1, -2, 1, 0, -100, 100, -20, 20, 70, -70, -20), 115, i8x16Bytes(110, 50, 0, 0, 126, 0, 0, -2, 0, 0, 106, 0, -30, 0, 10, 0)},
		{"i8x16.eq", i8a, i8b, 35, cmpMaskBytes(1, false, false, false, true, true, false, false, true, false, false, true, false, false, false, false, false)},
		{"i8x16.ne", i8a, i8b, 36, cmpMaskBytes(1, true, true, true, false, false, true, true, false, true, true, false, true, true, true, true, true)},
		{"i8x16.lt_s", i8a, i8b, 37, cmpMaskBytes(1, false, true, false, false, false, false, true, false, false, true, false, true, false, true, true, true)},
		{"i8x16.lt_u", i8a, i8b, 38, cmpMaskBytes(1, false, false, true, false, false, true, false, false, true, true, false, true, true, true, true, true)},
		{"i8x16.gt_s", i8a, i8b, 39, cmpMaskBytes(1, true, false, true, false, false, true, false, false, true, false, false, false, true, false, false, false)},
		{"i8x16.gt_u", i8a, i8b, 40, cmpMaskBytes(1, true, true, false, false, false, false, true, false, false, false, false, false, false, false, false, false)},
		{"i8x16.le_s", i8a, i8b, 41, cmpMaskBytes(1, false, true, false, true, true, false, true, true, false, true, true, true, false, true, true, true)},
		{"i8x16.le_u", i8a, i8b, 42, cmpMaskBytes(1, false, false, true, true, true, true, false, true, true, true, true, true, true, true, true, true)},
		{"i8x16.ge_s", i8a, i8b, 43, cmpMaskBytes(1, true, false, true, true, true, true, false, true, true, false, true, false, true, false, false, false)},
		{"i8x16.ge_u", i8a, i8b, 44, cmpMaskBytes(1, true, true, false, true, true, false, true, true, false, false, true, false, false, false, false, false)},
		{"i8x16.min_s", i8a, i8b, 118, i8x16Bytes(10, -128, -2, -5, 0, -1, -1, 64, -10, 20, 30, 40, -50, 60, 70, 80)},
		{"i8x16.min_u", i8a, i8b, 119, i8x16Bytes(10, 1, 1, -5, 0, 127, 1, 64, 10, 20, 30, 40, 50, 60, 70, 80)},
		{"i8x16.max_s", i8a, i8b, 120, i8x16Bytes(120, 1, 1, -5, 0, 127, 1, 64, 10, 21, 30, 41, 50, 61, 71, 81)},
		{"i8x16.max_u", i8a, i8b, 121, i8x16Bytes(120, -128, -2, -5, 0, -1, -1, 64, -10, 21, 30, 41, -50, 61, 71, 81)},
		{"i8x16.avgr_u", i8x16Bytes(0, 1, -1, -1, -128, 127, 10, 11, -56, -55, 0, -2, 100, 50, 33, -6), i8x16Bytes(0, 2, 0, -1, 0, -128, 20, 20, -56, -54, 1, 1, 101, 51, 34, 6), 123, i8x16Bytes(0, 2, -128, -1, 64, -128, 15, 16, -56, -54, 1, -128, 101, 51, 34, -128)},

		{"i16x8.add", i16a, i16b, 142, i16x8Bytes(-25536, -32767, -1, -10, 0, 32766, 0, 5555)},
		{"i16x8.add_sat_s", i16x8Bytes(30000, 20000, -30000, -20000, 32767, -32768, 1, -1), i16x8Bytes(10000, 20000, -10000, -20000, 1, -1, -2, 1), 143, i16x8Bytes(32767, 32767, -32768, -32768, 32767, -32768, -1, 0)},
		{"i16x8.add_sat_u", i16x8Bytes(30000, 20000, -30000, -20000, 32767, -32768, 1, -1), i16x8Bytes(10000, 20000, -10000, -20000, 1, -1, -2, 1), 144, i16x8Bytes(-25536, -25536, -1, -1, -32768, -1, -1, -1)},
		{"i16x8.sub", i16a, i16b, 145, i16x8Bytes(20000, 32767, 3, 0, 0, -32768, -2, -3087)},
		{"i16x8.sub_sat_s", i16x8Bytes(30000, 20000, -30000, -20000, 32767, -32768, 1, -1), i16x8Bytes(10000, 20000, -10000, -20000, 1, -1, -2, 1), 146, i16x8Bytes(20000, 0, -20000, 0, 32766, -32767, 3, -2)},
		{"i16x8.sub_sat_u", i16x8Bytes(30000, 20000, -30000, -20000, 32767, -32768, 1, -1), i16x8Bytes(10000, 20000, -10000, -20000, 1, -1, -2, 1), 147, i16x8Bytes(20000, 0, 0, 0, 32766, 0, 0, -2)},
		{"i16x8.q15mulr_sat_s", i16x8Bytes(32767, -32768, -32768, 16384, -16384, 12345, -12345, 30000), i16x8Bytes(32767, -32768, 32767, 16384, 16384, -23456, -23456, 2), 130, i16x8Bytes(32766, 32767, -32767, 8192, -8192, -8837, 8837, 2)},
		{"i16x8.narrow_i32x4_s", i32x4Bytes(40000, -40000, 32767, -32768), i32x4Bytes(0, -1, 123456789, -123456789), 133, i16x8Bytes(32767, -32768, 32767, -32768, 0, -1, 32767, -32768)},
		{"i16x8.narrow_i32x4_u", i32x4Bytes(0, 1, 65535, 65536), i32x4Bytes(-1, -2147483648, 32768, 40000), 134, i16x8Bytes(0, 1, -1, -1, -1, -1, -32768, -25536)},
		{"i16x8.mul", i16x8Bytes(30000, -32768, 12345, -12345, 0, 32767, -1, 256), i16x8Bytes(3, 2, -2, -3, 123, 2, -1, 256), 149, i16x8Bytes(24464, 0, -24690, -28501, 0, -2, 1, 0)},
		{"i16x8.eq", i16a, i16b, 45, cmpMaskBytes(2, false, false, false, true, true, false, false, false)},
		{"i16x8.ne", i16a, i16b, 46, cmpMaskBytes(2, true, true, true, false, false, true, true, true)},
		{"i16x8.lt_s", i16a, i16b, 47, cmpMaskBytes(2, false, true, false, false, false, false, true, true)},
		{"i16x8.lt_u", i16a, i16b, 48, cmpMaskBytes(2, false, false, true, false, false, true, false, true)},
		{"i16x8.gt_s", i16a, i16b, 49, cmpMaskBytes(2, true, false, true, false, false, true, false, false)},
		{"i16x8.gt_u", i16a, i16b, 50, cmpMaskBytes(2, true, true, false, false, false, false, true, false)},
		{"i16x8.le_s", i16a, i16b, 51, cmpMaskBytes(2, false, true, false, true, true, false, true, true)},
		{"i16x8.le_u", i16a, i16b, 52, cmpMaskBytes(2, false, false, true, true, true, true, false, true)},
		{"i16x8.ge_s", i16a, i16b, 53, cmpMaskBytes(2, true, false, true, true, true, true, false, false)},
		{"i16x8.ge_u", i16a, i16b, 54, cmpMaskBytes(2, true, true, false, true, true, false, true, false)},
		{"i16x8.min_s", i16a, i16b, 150, i16x8Bytes(10000, -32768, -2, -5, 0, -1, -1, 1234)},
		{"i16x8.min_u", i16a, i16b, 151, i16x8Bytes(10000, 1, 1, -5, 0, 32767, 1, 1234)},
		{"i16x8.max_s", i16a, i16b, 152, i16x8Bytes(30000, 1, 1, -5, 0, 32767, 1, 4321)},
		{"i16x8.max_u", i16a, i16b, 153, i16x8Bytes(30000, -32768, -2, -5, 0, -1, -1, 4321)},
		{"i16x8.avgr_u", i16x8Bytes(0, 1, -1, -1, -32768, 32767, 1000, -536), i16x8Bytes(0, 2, 0, -1, 0, -32768, 2000, 1000), 155, i16x8Bytes(0, 2, -32768, -1, 16384, -32768, 1500, -32536)},

		{"i32x4.add", i32a, i32b, 174, i32x4Bytes(-2147483648, -2147483647, -10, 0)},
		{"i32x4.sub", i32a, i32b, 177, i32x4Bytes(2147483646, 2147483647, 0, 246913578)},
		{"i32x4.mul", i32x4Bytes(2147483647, -2147483648, 123456789, -123456789), i32x4Bytes(2, 2, -3, -3), 181, i32x4Bytes(-2, 0, -370370367, 370370367)},
		{"i32x4.eq", i32a, i32b, 55, cmpMaskBytes(4, false, false, true, false)},
		{"i32x4.ne", i32a, i32b, 56, cmpMaskBytes(4, true, true, false, true)},
		{"i32x4.lt_s", i32a, i32b, 57, cmpMaskBytes(4, false, true, false, false)},
		{"i32x4.lt_u", i32a, i32b, 58, cmpMaskBytes(4, false, false, false, true)},
		{"i32x4.gt_s", i32a, i32b, 59, cmpMaskBytes(4, true, false, false, true)},
		{"i32x4.gt_u", i32a, i32b, 60, cmpMaskBytes(4, true, true, false, false)},
		{"i32x4.le_s", i32a, i32b, 61, cmpMaskBytes(4, false, true, true, false)},
		{"i32x4.le_u", i32a, i32b, 62, cmpMaskBytes(4, false, false, true, true)},
		{"i32x4.ge_s", i32a, i32b, 63, cmpMaskBytes(4, true, false, true, true)},
		{"i32x4.ge_u", i32a, i32b, 64, cmpMaskBytes(4, true, true, true, false)},
		{"i32x4.min_s", i32a, i32b, 182, i32x4Bytes(1, -2147483648, -5, -123456789)},
		{"i32x4.min_u", i32a, i32b, 183, i32x4Bytes(1, 1, -5, 123456789)},
		{"i32x4.max_s", i32a, i32b, 184, i32x4Bytes(2147483647, 1, -5, 123456789)},
		{"i32x4.max_u", i32a, i32b, 185, i32x4Bytes(2147483647, -2147483648, -5, -123456789)},

		{"i64x2.add", i64a, i64b, 206, i64x2Bytes(-9223372036854775808, -10)},
		{"i64x2.sub", i64a, i64b, 209, i64x2Bytes(9223372036854775806, 0)},
		{"i64x2.mul", i64x2Bytes(0x4000000000000001, -3), i64x2Bytes(2, 0x4000000000000000), 213, i64x2Bytes(-9223372036854775806, 0x4000000000000000)},
		{"i64x2.eq", i64a, i64b, 214, cmpMaskBytes(8, false, true)},
		{"i64x2.ne", i64a, i64b, 215, cmpMaskBytes(8, true, false)},
		{"i64x2.lt_s", i64x2Bytes(-9223372036854775808, 0), i64x2Bytes(-1, 0x7fffffffffffffff), 216, cmpMaskBytes(8, true, true)},
		{"i64x2.gt_s", i64x2Bytes(0x7fffffffffffffff, -1), i64x2Bytes(0, -9223372036854775808), 217, cmpMaskBytes(8, true, true)},
		{"i64x2.le_s", i64x2Bytes(-5, 42), i64x2Bytes(-5, -42), 218, cmpMaskBytes(8, true, false)},
		{"i64x2.ge_s", i64x2Bytes(-5, 42), i64x2Bytes(-5, -42), 219, cmpMaskBytes(8, true, true)},
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

func TestSIMDV128ControlFlow(t *testing.T) {
	a := i8x16Bytes(0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15)
	b := i8x16Bytes(16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31)

	t.Run("block result branch moves full v128", func(t *testing.T) {
		body := []byte{0x00, 0x02, 0x7b} // block (result v128)
		body = append(body, v128ConstBytes(a)...)
		body = append(body, 0x41, 0x01, 0x0d, 0x00) // i32.const 1; br_if 0 carrying a
		body = append(body, 0x1a)                   // drop a on the not-taken path
		body = append(body, v128ConstBytes(b)...)
		body = append(body, 0x0b, 0x0b)
		m := mod1(t, nil, []wasm.ValType{wasm.V128}, body)
		if got := runAmd64V128(t, m, nil); got != a {
			t.Fatalf("block branch result = % x, want % x", got, a)
		}
	})

	t.Run("if param/result preserves v128 passthrough", func(t *testing.T) {
		body := []byte{0x00}
		body = append(body, v128ConstBytes(a)...)
		body = append(body, 0x41, 0x00, 0x04, 0x00) // i32.const 0; if (type 0) (param v128) (result v128)
		body = append(body, 0x1a)                   // then-only replacement would drop a and return b
		body = append(body, v128ConstBytes(b)...)
		body = append(body, 0x0b, 0x0b)
		m := modWithExtraBlockType(t, body)
		if got := runAmd64V128(t, m, nil); got != a {
			t.Fatalf("if passthrough result = % x, want % x", got, a)
		}
	})

	t.Run("loop param/result backedge carries full v128", func(t *testing.T) {
		body := []byte{0x01, 0x01, 0x7f}            // one i32 local
		body = append(body, 0x41, 0x01, 0x21, 0x00) // local.set 0 = 1
		body = append(body, 0x02, 0x7b)             // block (result v128)
		body = append(body, v128ConstBytes(a)...)
		body = append(body, 0x03, 0x00)                               // loop (type 0) (param v128) (result v128)
		body = append(body, 0x20, 0x00, 0x45)                         // local.get 0; i32.eqz
		body = append(body, 0x0d, 0x01)                               // br_if 1 carrying current v128 to the block result
		body = append(body, 0x20, 0x00, 0x41, 0x01, 0x6b, 0x21, 0x00) // local0--
		body = append(body, 0x0c, 0x00)                               // br 0 carrying current v128 to loop param
		body = append(body, 0x0b, 0x0b, 0x0b)
		m := modWithExtraBlockType(t, body)
		if got := runAmd64V128(t, m, nil); got != a {
			t.Fatalf("loop backedge result = % x, want % x", got, a)
		}
	})
}

func modWithExtraBlockType(t *testing.T, body []byte) *wasm.Module {
	t.Helper()
	entry := append(wasmtest.ULEB(uint32(len(body))), body...)
	b := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.V128}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(entry)),
	)
	m, err := wasm.DecodeModule(b)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return m
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

func TestSIMDCorePackedFloatRounding(t *testing.T) {
	negZero32 := math.Float32bits(float32(math.Copysign(0, -1)))
	negZero64 := math.Float64bits(math.Copysign(0, -1))
	nan32 := uint32(0x7fc00001)
	nan64 := uint64(0x7ff8000000000001)

	f32Cases := []struct {
		name    string
		src     [16]byte
		sub     uint32
		want    [4]uint32
		wantNaN [4]bool
	}{
		{
			name:    "f32x4.ceil",
			src:     f32x4Bits(math.Float32bits(-1.2), math.Float32bits(float32(math.Copysign(0.4, -1))), math.Float32bits(1.2), nan32),
			sub:     103,
			want:    [4]uint32{math.Float32bits(-1), negZero32, math.Float32bits(2), 0},
			wantNaN: [4]bool{false, false, false, true},
		},
		{
			name:    "f32x4.floor",
			src:     f32x4Bits(math.Float32bits(1.2), math.Float32bits(0.4), math.Float32bits(-1.2), nan32),
			sub:     104,
			want:    [4]uint32{math.Float32bits(1), math.Float32bits(0), math.Float32bits(-2), 0},
			wantNaN: [4]bool{false, false, false, true},
		},
		{
			name:    "f32x4.trunc",
			src:     f32x4Bits(math.Float32bits(1.9), math.Float32bits(-1.9), math.Float32bits(float32(math.Copysign(0.4, -1))), nan32),
			sub:     105,
			want:    [4]uint32{math.Float32bits(1), math.Float32bits(-1), negZero32, 0},
			wantNaN: [4]bool{false, false, false, true},
		},
		{
			name:    "f32x4.nearest",
			src:     f32x4Bits(math.Float32bits(1.5), math.Float32bits(2.5), math.Float32bits(float32(math.Copysign(0.5, -1))), nan32),
			sub:     106,
			want:    [4]uint32{math.Float32bits(2), math.Float32bits(2), negZero32, 0},
			wantNaN: [4]bool{false, false, false, true},
		},
	}
	for _, tc := range f32Cases {
		t.Run(tc.name, func(t *testing.T) {
			body := append([]byte{0x00}, v128ConstBytes(tc.src)...)
			body = append(body, simdOp(tc.sub)...)
			body = append(body, 0x0b)
			m := mod1(t, nil, []wasm.ValType{wasm.V128}, body)
			got := runAmd64V128(t, m, nil)
			requireF32x4BitsOrNaN(t, tc.name, got, tc.want, tc.wantNaN)
		})
	}

	f64Cases := []struct {
		name    string
		src     [16]byte
		sub     uint32
		want    [2]uint64
		wantNaN [2]bool
	}{
		{
			name: "f64x2.ceil",
			src:  f64x2Bits(math.Float64bits(math.Copysign(0.4, -1)), math.Float64bits(1.2)),
			sub:  116,
			want: [2]uint64{negZero64, math.Float64bits(2)},
		},
		{
			name:    "f64x2.floor",
			src:     f64x2Bits(nan64, math.Float64bits(-1.2)),
			sub:     117,
			want:    [2]uint64{0, math.Float64bits(-2)},
			wantNaN: [2]bool{true, false},
		},
		{
			name: "f64x2.trunc",
			src:  f64x2Bits(math.Float64bits(1.9), math.Float64bits(math.Copysign(0.4, -1))),
			sub:  122,
			want: [2]uint64{math.Float64bits(1), negZero64},
		},
		{
			name: "f64x2.nearest",
			src:  f64x2Bits(math.Float64bits(2.5), math.Float64bits(math.Copysign(0.5, -1))),
			sub:  148,
			want: [2]uint64{math.Float64bits(2), negZero64},
		},
	}
	for _, tc := range f64Cases {
		t.Run(tc.name, func(t *testing.T) {
			body := append([]byte{0x00}, v128ConstBytes(tc.src)...)
			body = append(body, simdOp(tc.sub)...)
			body = append(body, 0x0b)
			m := mod1(t, nil, []wasm.ValType{wasm.V128}, body)
			got := runAmd64V128(t, m, nil)
			requireF64x2BitsOrNaN(t, tc.name, got, tc.want, tc.wantNaN)
		})
	}
}

func TestSIMDPackedFloatUnary(t *testing.T) {
	negZero32 := float32(math.Copysign(0, -1))
	negZero64 := math.Copysign(0, -1)
	cases := []struct {
		name string
		in   [16]byte
		sub  uint32
		want [16]byte
	}{
		{"f32x4.abs", f32x4Bytes(negZero32, -4, 9, -16), 224, f32x4Bytes(0, 4, 9, 16)},
		{"f32x4.neg", f32x4Bytes(0, -4, 9, negZero32), 225, f32x4Bytes(negZero32, 4, -9, 0)},
		{"f32x4.sqrt", f32x4Bytes(0, 4, 9, 16), 227, f32x4Bytes(0, 2, 3, 4)},
		{"f64x2.abs", f64x2Bytes(negZero64, -9), 236, f64x2Bytes(0, 9)},
		{"f64x2.neg", f64x2Bytes(0, -9), 237, f64x2Bytes(negZero64, 9)},
		{"f64x2.sqrt", f64x2Bytes(0, 9), 239, f64x2Bytes(0, 3)},
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

func TestSIMDCorePackedFloatMinMax(t *testing.T) {
	negZero32 := math.Float32bits(float32(math.Copysign(0, -1)))
	posZero32 := math.Float32bits(0)
	nan32a := uint32(0x7fc00001)
	nan32b := uint32(0x7fc00002)
	negZero64 := math.Float64bits(math.Copysign(0, -1))
	posZero64 := math.Float64bits(0)
	nan64a := uint64(0x7ff8000000000001)
	nan64b := uint64(0x7ff8000000000002)

	f32a := f32x4Bits(math.Float32bits(3), negZero32, nan32a, math.Float32bits(4))
	f32b := f32x4Bits(math.Float32bits(2), posZero32, math.Float32bits(5), nan32b)
	f32Cases := []struct {
		name    string
		sub     uint32
		want    [4]uint32
		wantNaN [4]bool
	}{
		{"f32x4.min", 232, [4]uint32{math.Float32bits(2), negZero32, 0, 0}, [4]bool{false, false, true, true}},
		{"f32x4.max", 233, [4]uint32{math.Float32bits(3), posZero32, 0, 0}, [4]bool{false, false, true, true}},
		{"f32x4.pmin", 234, [4]uint32{math.Float32bits(2), negZero32, 0, math.Float32bits(4)}, [4]bool{false, false, true, false}},
		{"f32x4.pmax", 235, [4]uint32{math.Float32bits(3), negZero32, 0, math.Float32bits(4)}, [4]bool{false, false, true, false}},
	}
	for _, tc := range f32Cases {
		t.Run(tc.name, func(t *testing.T) {
			m := mod1(t, nil, []wasm.ValType{wasm.V128}, v128BinaryBody(f32a, f32b, tc.sub))
			got := runAmd64V128(t, m, nil)
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
		{"f64x2.min", f64a, f64b, 244, [2]uint64{0, negZero64}, [2]bool{true, false}},
		{"f64x2.max", f64a, f64b, 245, [2]uint64{0, posZero64}, [2]bool{true, false}},
		{"f64x2.pmin_first_nan", f64a, f64b, 246, [2]uint64{0, negZero64}, [2]bool{true, false}},
		{"f64x2.pmax_first_nan", f64a, f64b, 247, [2]uint64{0, negZero64}, [2]bool{true, false}},
		{"f64x2.pmin_second_nan", f64x2Bits(math.Float64bits(4), negZero64), f64x2Bits(nan64b, posZero64), 246, [2]uint64{math.Float64bits(4), negZero64}, [2]bool{false, false}},
		{"f64x2.pmax_second_nan", f64x2Bits(math.Float64bits(4), negZero64), f64x2Bits(nan64b, posZero64), 247, [2]uint64{math.Float64bits(4), negZero64}, [2]bool{false, false}},
	}
	for _, tc := range f64Cases {
		t.Run(tc.name, func(t *testing.T) {
			m := mod1(t, nil, []wasm.ValType{wasm.V128}, v128BinaryBody(tc.a, tc.b, tc.sub))
			got := runAmd64V128(t, m, nil)
			requireF64x2BitsOrNaN(t, tc.name, got, tc.want, tc.wantNaN)
		})
	}
}

func TestSIMDRelaxedPackedFloatMinMax(t *testing.T) {
	negZero32 := math.Float32bits(float32(math.Copysign(0, -1)))
	posZero32 := math.Float32bits(0)
	nan32a := uint32(0x7fc00001)
	nan32b := uint32(0x7fc00002)
	negZero64 := math.Float64bits(math.Copysign(0, -1))
	posZero64 := math.Float64bits(0)
	nan64a := uint64(0x7ff8000000000001)
	nan64b := uint64(0x7ff8000000000002)

	cases := []struct {
		name string
		a    [16]byte
		b    [16]byte
		sub  uint32
		want [16]byte
	}{
		{
			name: "f32x4.relaxed_min",
			a:    f32x4Bits(math.Float32bits(3), negZero32, nan32a, math.Float32bits(4)),
			b:    f32x4Bits(math.Float32bits(2), posZero32, math.Float32bits(5), nan32b),
			sub:  269,
			want: f32x4Bits(math.Float32bits(2), posZero32, math.Float32bits(5), nan32b),
		},
		{
			name: "f32x4.relaxed_max",
			a:    f32x4Bits(math.Float32bits(3), negZero32, nan32a, math.Float32bits(4)),
			b:    f32x4Bits(math.Float32bits(2), posZero32, math.Float32bits(5), nan32b),
			sub:  270,
			want: f32x4Bits(math.Float32bits(3), posZero32, math.Float32bits(5), nan32b),
		},
		{
			name: "f64x2.relaxed_min numeric and zero",
			a:    f64x2Bits(math.Float64bits(3), negZero64),
			b:    f64x2Bits(math.Float64bits(2), posZero64),
			sub:  271,
			want: f64x2Bits(math.Float64bits(2), posZero64),
		},
		{
			name: "f64x2.relaxed_max numeric and zero",
			a:    f64x2Bits(math.Float64bits(3), negZero64),
			b:    f64x2Bits(math.Float64bits(2), posZero64),
			sub:  272,
			want: f64x2Bits(math.Float64bits(3), posZero64),
		},
		{
			name: "f64x2.relaxed_min nan",
			a:    f64x2Bits(nan64a, math.Float64bits(4)),
			b:    f64x2Bits(math.Float64bits(5), nan64b),
			sub:  271,
			want: f64x2Bits(math.Float64bits(5), nan64b),
		},
		{
			name: "f64x2.relaxed_max nan",
			a:    f64x2Bits(nan64a, math.Float64bits(4)),
			b:    f64x2Bits(math.Float64bits(5), nan64b),
			sub:  272,
			want: f64x2Bits(math.Float64bits(5), nan64b),
		},
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
