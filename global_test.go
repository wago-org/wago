//go:build linux && amd64

package wago

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func uleb(v uint32) []byte {
	var out []byte
	for {
		b := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			b |= 0x80
		}
		out = append(out, b)
		if v == 0 {
			return out
		}
	}
}

func sleb32(v int32) []byte {
	var out []byte
	more := true
	for more {
		b := byte(v & 0x7f)
		v >>= 7
		sign := b&0x40 != 0
		more = !((v == 0 && !sign) || (v == -1 && sign))
		if more {
			b |= 0x80
		}
		out = append(out, b)
	}
	return out
}

func sleb64(v int64) []byte {
	var out []byte
	more := true
	for more {
		b := byte(v & 0x7f)
		v >>= 7
		sign := b&0x40 != 0
		more = !((v == 0 && !sign) || (v == -1 && sign))
		if more {
			b |= 0x80
		}
		out = append(out, b)
	}
	return out
}

func section(id byte, payload []byte) []byte {
	out := []byte{id}
	out = append(out, uleb(uint32(len(payload)))...)
	out = append(out, payload...)
	return out
}

func wasmModule(sections ...[]byte) []byte {
	out := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	for _, s := range sections {
		out = append(out, s...)
	}
	return out
}

func vec(items ...[]byte) []byte {
	out := uleb(uint32(len(items)))
	for _, it := range items {
		out = append(out, it...)
	}
	return out
}

func name(s string) []byte { return append(uleb(uint32(len(s))), []byte(s)...) }

func globalEntry(t wasm.ValType, mutable bool, init []byte) []byte {
	mut := byte(0)
	if mutable {
		mut = 1
	}
	out := []byte{byte(t), mut}
	out = append(out, init...)
	return out
}

func exportEntry(n string, kind byte, idx uint32) []byte {
	out := name(n)
	out = append(out, kind)
	out = append(out, uleb(idx)...)
	return out
}

func funcType(params, results []wasm.ValType) []byte {
	out := []byte{0x60}
	out = append(out, uleb(uint32(len(params)))...)
	for _, p := range params {
		out = append(out, byte(p))
	}
	out = append(out, uleb(uint32(len(results)))...)
	for _, r := range results {
		out = append(out, byte(r))
	}
	return out
}

func code(body []byte) []byte {
	fn := append([]byte{0x00}, body...) // zero local decls
	return append(uleb(uint32(len(fn))), fn...)
}

func TestCompileGlobalMetadataNumericInits(t *testing.T) {
	f32bits := uint32(0x7fc12345)
	f64bits := uint64(0x7ff80000deadbeef)
	f32 := make([]byte, 4)
	binary.LittleEndian.PutUint32(f32, f32bits)
	f64 := make([]byte, 8)
	binary.LittleEndian.PutUint64(f64, f64bits)
	mod := wasmModule(
		section(6, vec(
			globalEntry(wasm.I32, false, append(append([]byte{0x41}, sleb32(-1)...), 0x0b)),
			globalEntry(wasm.I64, true, append(append([]byte{0x42}, sleb64(-2)...), 0x0b)),
			globalEntry(wasm.F32, false, append(append([]byte{0x43}, f32...), 0x0b)),
			globalEntry(wasm.F64, true, append(append([]byte{0x44}, f64...), 0x0b)),
		)),
		section(7, vec(exportEntry("g32", 3, 0), exportEntry("g64", 3, 1))),
	)
	c, err := Compile(mod)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Globals) != 4 {
		t.Fatalf("globals = %d, want 4", len(c.Globals))
	}
	want := []GlobalDef{{Type: wasm.I32, Bits: math.MaxUint32}, {Type: wasm.I64, Mutable: true, Bits: ^uint64(1)}, {Type: wasm.F32, Bits: uint64(f32bits)}, {Type: wasm.F64, Mutable: true, Bits: f64bits}}
	for i := range want {
		if c.Globals[i] != want[i] {
			t.Fatalf("global %d = %+v, want %+v", i, c.Globals[i], want[i])
		}
	}
	if c.GlobalExports["g32"] != 0 || c.GlobalExports["g64"] != 1 {
		t.Fatalf("global exports = %#v", c.GlobalExports)
	}
	if len(c.Exports) != 0 {
		t.Fatalf("function exports = %#v, want empty", c.Exports)
	}
}

func TestCompileRejectsGlobalInitializerTypeMismatch(t *testing.T) {
	mod := wasmModule(section(6, vec(globalEntry(wasm.I32, false, []byte{0x42, 0x00, 0x0b}))))
	if _, err := Compile(mod); err == nil || !bytes.Contains([]byte(err.Error()), []byte("validate")) {
		t.Fatalf("Compile mismatch error = %v, want validate error", err)
	}
}

func TestInstantiateInitializesGlobalSlots(t *testing.T) {
	c := &Compiled{Globals: []GlobalDef{
		{Type: wasm.I32, Bits: 0x11223344},
		{Type: wasm.I64, Mutable: true, Bits: 0x0123456789abcdef},
	}}
	in, err := Instantiate(c, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if len(in.globals) != 16 {
		t.Fatalf("globals bytes = %d, want 16", len(in.globals))
	}
	if got := binary.LittleEndian.Uint64(in.globals[0:]); got != 0x11223344 {
		t.Fatalf("global 0 slot = %#x, want %#x", got, uint64(0x11223344))
	}
	if got := binary.LittleEndian.Uint64(in.globals[8:]); got != 0x0123456789abcdef {
		t.Fatalf("global 1 slot = %#x, want %#x", got, uint64(0x0123456789abcdef))
	}
}

func TestInstantiateGlobalStorageIsPerInstance(t *testing.T) {
	c := &Compiled{Globals: []GlobalDef{{Type: wasm.I32, Mutable: true, Bits: 7}}}
	in1, err := Instantiate(c, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer in1.Close()
	in2, err := Instantiate(c, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer in2.Close()
	binary.LittleEndian.PutUint64(in1.globals, 99)
	if got := binary.LittleEndian.Uint64(in2.globals); got != 7 {
		t.Fatalf("instance 2 global = %d, want initial 7", got)
	}
}

func TestGlobalGetSetEndToEnd(t *testing.T) {
	mod := wasmModule(
		section(1, vec(funcType(nil, []wasm.ValType{wasm.I32}), funcType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		section(3, vec([]byte{0x00}, []byte{0x01})),
		section(6, vec(globalEntry(wasm.I32, true, []byte{0x41, 0x29, 0x0b}))),
		section(7, vec(exportEntry("get", 0, 0), exportEntry("inc", 0, 1))),
		section(10, vec(
			code([]byte{0x23, 0x00, 0x0b}),
			code([]byte{0x23, 0x00, 0x20, 0x00, 0x6a, 0x24, 0x00, 0x23, 0x00, 0x0b}),
		)),
	)
	c, err := Compile(mod)
	if err != nil {
		t.Fatal(err)
	}
	in, err := Instantiate(c, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	res, err := in.Invoke("get")
	if err != nil {
		t.Fatal(err)
	}
	if got := res[0].AsI32(); got != 41 {
		t.Fatalf("get = %d, want 41", got)
	}
	res, err = in.Invoke("inc", I32(1))
	if err != nil {
		t.Fatal(err)
	}
	if got := res[0].AsI32(); got != 42 {
		t.Fatalf("inc = %d, want 42", got)
	}
}

func TestGlobalNumericRoundTrips(t *testing.T) {
	f32bits := uint32(0x3fc00000) // 1.5
	f64bits := math.Float64bits(2.25)
	f32 := make([]byte, 4)
	binary.LittleEndian.PutUint32(f32, f32bits)
	f64 := make([]byte, 8)
	binary.LittleEndian.PutUint64(f64, f64bits)
	mod := wasmModule(
		section(1, vec(
			funcType(nil, []wasm.ValType{wasm.I64}),
			funcType([]wasm.ValType{wasm.F32}, []wasm.ValType{wasm.F32}),
			funcType([]wasm.ValType{wasm.F64}, []wasm.ValType{wasm.F64}),
		)),
		section(3, vec([]byte{0x00}, []byte{0x01}, []byte{0x02})),
		section(6, vec(
			globalEntry(wasm.I32, false, []byte{0x41, 0x01, 0x0b}),
			globalEntry(wasm.I64, true, append(append([]byte{0x42}, sleb64(0x0102030405060708)...), 0x0b)),
			globalEntry(wasm.F32, true, append(append([]byte{0x43}, f32...), 0x0b)),
			globalEntry(wasm.F64, true, append(append([]byte{0x44}, f64...), 0x0b)),
		)),
		section(7, vec(exportEntry("g64", 0, 0), exportEntry("f32", 0, 1), exportEntry("f64", 0, 2))),
		section(10, vec(
			code([]byte{0x23, 0x01, 0x0b}),
			code([]byte{0x20, 0x00, 0x24, 0x02, 0x23, 0x02, 0x0b}),
			code([]byte{0x20, 0x00, 0x24, 0x03, 0x23, 0x03, 0x0b}),
		)),
	)
	c, err := Compile(mod)
	if err != nil {
		t.Fatal(err)
	}
	in, err := Instantiate(c, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if res, err := in.Invoke("g64"); err != nil || res[0].AsI64() != 0x0102030405060708 {
		t.Fatalf("g64 = %v, %v", res, err)
	}
	if res, err := in.Invoke("f32", F32(3.5)); err != nil || math.Float32bits(res[0].AsF32()) != math.Float32bits(3.5) {
		t.Fatalf("f32 = %v, %v", res, err)
	}
	if res, err := in.Invoke("f64", F64(4.5)); err != nil || math.Float64bits(res[0].AsF64()) != math.Float64bits(4.5) {
		t.Fatalf("f64 = %v, %v", res, err)
	}
}

func TestGlobalsArePerInstanceThroughWasm(t *testing.T) {
	mod := wasmModule(
		section(1, vec(funcType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		section(3, vec([]byte{0x00})),
		section(6, vec(globalEntry(wasm.I32, true, []byte{0x41, 0x00, 0x0b}))),
		section(7, vec(exportEntry("add", 0, 0))),
		section(10, vec(code([]byte{0x23, 0x00, 0x20, 0x00, 0x6a, 0x24, 0x00, 0x23, 0x00, 0x0b}))),
	)
	c, err := Compile(mod)
	if err != nil {
		t.Fatal(err)
	}
	in1, err := Instantiate(c, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer in1.Close()
	in2, err := Instantiate(c, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer in2.Close()
	if res, err := in1.Invoke("add", I32(5)); err != nil || res[0].AsI32() != 5 {
		t.Fatalf("in1 add = %v, %v", res, err)
	}
	if res, err := in2.Invoke("add", I32(7)); err != nil || res[0].AsI32() != 7 {
		t.Fatalf("in2 add = %v, %v", res, err)
	}
	if res, err := in1.Invoke("add", I32(0)); err != nil || res[0].AsI32() != 5 {
		t.Fatalf("in1 persisted = %v, %v", res, err)
	}
}
