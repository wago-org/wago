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
