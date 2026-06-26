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
