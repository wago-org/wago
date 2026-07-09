//go:build (linux && (amd64 || arm64)) || (darwin && arm64)

package wago

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

// importMemModule imports "env.mem" (memory 1) and exports
// store(addr,val) and load(addr)->i32 over it.
func importMemModule() []byte {
	memImport := append(wasmtest.Name("env"), wasmtest.Name("mem")...)
	memImport = append(memImport, 0x02, 0x00, 0x01) // memory import, min 1 page
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(memImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("store", 0, 0),
			wasmtest.ExportEntry("load", 0, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x36, 0x02, 0x00, 0x0b}), // local.get0; local.get1; i32.store
			wasmtest.Code([]byte{0x20, 0x00, 0x28, 0x02, 0x00, 0x0b}),             // local.get0; i32.load
		)),
	)
}

// growMemModule declares its own exported memory (min 1, max 10 pages) plus
// grow(pages)->prev and store(ptr,val).
func growMemModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), // grow
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, nil),            // store
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x01, 0x01, 0x0a})), // memory: flags=has-max, min=1, max=10
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("memory", 0x02, 0),
			wasmtest.ExportEntry("grow", 0x00, 0),
			wasmtest.ExportEntry("store", 0x00, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x40, 0x00, 0x0b}),                   // local.get 0; memory.grow 0
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x36, 0x02, 0x00, 0x0b}), // local.get 0; local.get 1; i32.store
		)),
	)
}

func TestImportedMemoryShared(t *testing.T) {
	t.Setenv("WAGO_BOUNDS", "explicit") // pin the explicit-bounds path (guard-page imports are covered in memory_guardpage_test.go)
	c, err := Compile(nil, importMemModule())
	if err != nil {
		t.Fatalf("compile (memory import should be accepted now): %v", err)
	}
	mem, err := NewMemory(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer mem.Close()
	in, err := Instantiate(c, InstantiateOptions{Imports: Imports{"env.mem": mem}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()

	// wasm writes -> host observes.
	if _, err := in.Invoke("store", I32(8), I32(0xCAFE)); err != nil {
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint32(mem.Bytes()[8:]); got != 0xCAFE {
		t.Fatalf("host sees mem[8] = %#x, want 0xCAFE", got)
	}
	// host writes -> wasm observes.
	binary.LittleEndian.PutUint32(mem.Bytes()[16:], 0x1234)
	r, err := in.Invoke("load", I32(16))
	if err != nil {
		t.Fatal(err)
	}
	if AsI32(r[0]) != 0x1234 {
		t.Fatalf("wasm load = %#x, want 0x1234", AsI32(r[0]))
	}
	// inst.Memory() is the same object the host imported.
	if in.Memory() != mem {
		t.Fatal("inst.Memory() is not the imported memory")
	}
}

func TestMemoryGrowExported(t *testing.T) {
	t.Setenv("WAGO_BOUNDS", "explicit")
	c, err := Compile(nil, growMemModule())
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()

	r, err := in.Invoke("grow", I32(4))
	if err != nil {
		t.Fatalf("grow: %v", err)
	}
	if prev := AsI32(r[0]); prev != 1 {
		t.Fatalf("memory.grow returned %d, want previous count 1", prev)
	}
	if got := len(in.Memory().Bytes()); got != 5*65536 {
		t.Fatalf("after grow, Bytes() len = %d, want %d", got, 5*65536)
	}
}

func TestImportedMemoryMissing(t *testing.T) {
	c, err := Compile(nil, importMemModule())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Instantiate(c, InstantiateOptions{}); err == nil {
		t.Fatal("Instantiate without the imported memory should fail")
	}
}

func TestImportedMemorySingleInstance(t *testing.T) {
	t.Setenv("WAGO_BOUNDS", "explicit") // pin the explicit-bounds path (guard-page imports are covered in memory_guardpage_test.go)
	c, _ := Compile(nil, importMemModule())
	mem, _ := NewMemory(1, 1)
	defer mem.Close()
	in, err := Instantiate(c, InstantiateOptions{Imports: Imports{"env.mem": mem}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err := Instantiate(c, InstantiateOptions{Imports: Imports{"env.mem": mem}}); err == nil {
		t.Fatal("a second instance importing the same in-use memory should fail")
	}
}

func TestImportedMemorySurvivesMarshalLoad(t *testing.T) {
	t.Setenv("WAGO_BOUNDS", "explicit") // pin explicit: serializing a signals-based module is unsupported (see TestSignalsBasedNotSerializable)
	c, err := Compile(nil, importMemModule())
	if err != nil {
		t.Fatal(err)
	}
	blob, err := c.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(blob)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Instantiate(loaded, InstantiateOptions{}); err == nil {
		t.Fatal("loaded module should still require imported memory")
	}
	mem, err := NewMemory(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer mem.Close()
	in, err := Instantiate(loaded, InstantiateOptions{Imports: Imports{"env.mem": mem}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err := in.Invoke("store", I32(4), I32(0x55AA)); err != nil {
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint32(mem.Bytes()[4:]); got != 0x55AA {
		t.Fatalf("host sees mem[4] = %#x, want 0x55AA", got)
	}
}
