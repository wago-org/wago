//go:build linux && amd64

package amd64

import (
	"bytes"
	"encoding/binary"
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
