//go:build linux && amd64

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func funcImportEntry(module, name string, typeIdx uint32) []byte {
	out := append(wasmtest.Name(module), wasmtest.Name(name)...)
	out = append(out, 0x00) // ExternFunc
	return append(out, wasmtest.ULEB(typeIdx)...)
}

// TestCrossInstanceMemoryShared: A owns a memory with data; B imports A's memory,
// writes into it, and A observes the write (shared bytes).
func TestCrossInstanceMemoryShared(t *testing.T) {
	// A: memory 1; data at offset 10 = {1,2,3}; load(a)->i32 = i32.load8_u; store(a,v).
	modA := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})), // 1 memory, min 1
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("load", 0, 0),
			wasmtest.ExportEntry("store", 0, 1),
			wasmtest.ExportEntry("mem", 2, 0), // memory export
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x2d, 0x00, 0x00, 0x0b}),             // local.get 0; i32.load8_u; end
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x3a, 0x00, 0x00, 0x0b}), // local.get0; local.get1; i32.store8; end
		)),
		// data: offset 10, bytes {1,2,3}
		wasmtest.Section(11, wasmtest.Vec(append([]byte{0x00, 0x41, 0x0a, 0x0b, 0x03}, 0x01, 0x02, 0x03))),
	)
	inA, err := Instantiate(MustCompile(modA), nil)
	if err != nil {
		t.Fatalf("instantiate A: %v", err)
	}
	defer inA.Close()
	memImport, err := inA.ExportedMemory("mem")
	if err != nil {
		t.Fatalf("export mem: %v", err)
	}

	// B imports env.mem; write(a,v) = i32.store8; load(a)->i32.
	memEntry := append(wasmtest.Name("env"), wasmtest.Name("mem")...)
	memEntry = append(memEntry, 0x02, 0x00, 0x01) // ExternMem, min 1
	modB := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(memEntry)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("write", 0, 0),
			wasmtest.ExportEntry("load", 0, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x3a, 0x00, 0x00, 0x0b}), // store8
			wasmtest.Code([]byte{0x20, 0x00, 0x2d, 0x00, 0x00, 0x0b}),             // load8_u
		)),
	)
	inB, err := Instantiate(MustCompile(modB), Imports{"env.mem": memImport})
	if err != nil {
		t.Fatalf("instantiate B: %v", err)
	}
	defer inB.Close()

	// B sees A's data (byte 11 = 2).
	if r, _ := inB.Invoke("load", I32(11)); AsI32(r[0]) != 2 {
		t.Fatalf("B.load(11) = %d, want 2 (A's data)", AsI32(r[0]))
	}
	// B writes byte 11 = 99 -> A observes.
	if _, err := inB.Invoke("write", I32(11), I32(99)); err != nil {
		t.Fatal(err)
	}
	if r, _ := inA.Invoke("load", I32(11)); AsI32(r[0]) != 99 {
		t.Fatalf("A.load(11) = %d, want 99 (B's write)", AsI32(r[0]))
	}
	// A writes byte 20 = 55 -> B observes.
	if _, err := inA.Invoke("store", I32(20), I32(55)); err != nil {
		t.Fatal(err)
	}
	if r, _ := inB.Invoke("load", I32(20)); AsI32(r[0]) != 55 {
		t.Fatalf("B.load(20) = %d, want 55 (A's write)", AsI32(r[0]))
	}
}

// TestCrossInstanceGlobalShared: A exports a mutable i32 global g (=10) plus
// get/set functions; B imports A.g and reads/writes it. The two instances share
// one cell, so writes are mutually visible.
func TestCrossInstanceGlobalShared(t *testing.T) {
	// A: global0 = (mut i32) 10; getg()->i32 = global.get 0; setg(i32) = global.set 0.
	modA := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x7f, 0x01, 0x41, 0x0a, 0x0b})), // (mut i32) (i32.const 10)
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("g", 3, 0),
			wasmtest.ExportEntry("getg", 0, 0),
			wasmtest.ExportEntry("setg", 0, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x23, 0x00, 0x0b}),             // global.get 0; end
			wasmtest.Code([]byte{0x20, 0x00, 0x24, 0x00, 0x0b}), // local.get 0; global.set 0; end
		)),
	)
	inA, err := Instantiate(MustCompile(modA), nil)
	if err != nil {
		t.Fatalf("instantiate A: %v", err)
	}
	defer inA.Close()
	gImport, err := inA.ExportedGlobalObject("g")
	if err != nil {
		t.Fatalf("export g: %v", err)
	}

	// B imports env.g (mut i32); read()->i32 = global.get 0; write(i32) = global.set 0.
	gEntry := append(wasmtest.Name("env"), wasmtest.Name("g")...)
	gEntry = append(gEntry, 0x03, 0x7f, 0x01) // ExternGlobal, i32, mutable
	modB := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil),
		)),
		wasmtest.Section(2, wasmtest.Vec(gEntry)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("read", 0, 0),
			wasmtest.ExportEntry("write", 0, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x23, 0x00, 0x0b}),             // global.get 0; end
			wasmtest.Code([]byte{0x20, 0x00, 0x24, 0x00, 0x0b}), // local.get 0; global.set 0; end
		)),
	)
	inB, err := Instantiate(MustCompile(modB), Imports{"env.g": gImport})
	if err != nil {
		t.Fatalf("instantiate B: %v", err)
	}
	defer inB.Close()

	// B sees A's initial value.
	if r, _ := inB.Invoke("read"); AsI32(r[0]) != 10 {
		t.Fatalf("B.read = %d, want 10", AsI32(r[0]))
	}
	// B writes -> A observes (shared cell).
	if _, err := inB.Invoke("write", I32(99)); err != nil {
		t.Fatal(err)
	}
	if r, _ := inA.Invoke("getg"); AsI32(r[0]) != 99 {
		t.Fatalf("A.getg = %d, want 99 (B's write)", AsI32(r[0]))
	}
	// A writes -> B observes.
	if _, err := inA.Invoke("setg", I32(7)); err != nil {
		t.Fatal(err)
	}
	if r, _ := inB.Invoke("read"); AsI32(r[0]) != 7 {
		t.Fatalf("B.read = %d, want 7 (A's write)", AsI32(r[0]))
	}
}

// TestCrossInstanceCallNoArgs: instance A exports f()->i32 = 42; instance B
// imports env.f and calls it, returning its result. Exercises the native
// context-swap end to end.
func TestCrossInstanceCallNoArgs(t *testing.T) {
	modA := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0x2a, 0x0b}))), // i32.const 42; end
	)
	inA, err := Instantiate(MustCompile(modA), nil)
	if err != nil {
		t.Fatalf("instantiate A: %v", err)
	}
	defer inA.Close()
	fExport, err := inA.ExportedFunc("f")
	if err != nil {
		t.Fatalf("export f: %v", err)
	}

	imp := funcImportEntry("env", "f", 0)
	modB := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0x00, 0x0b}))), // call 0; end
	)
	cB, err := Compile(modB)
	if err != nil {
		t.Fatalf("compile B: %v", err)
	}
	if !cB.needsLink {
		t.Fatalf("B should need link (returning import)")
	}
	inB, err := Instantiate(cB, Imports{"env.f": fExport})
	if err != nil {
		t.Fatalf("instantiate B: %v", err)
	}
	defer inB.Close()
	r, err := inB.Invoke("call")
	if err != nil {
		t.Fatalf("invoke call: %v", err)
	}
	if AsI32(r[0]) != 42 {
		t.Fatalf("cross-instance call returned %d, want 42", AsI32(r[0]))
	}
}

// TestCrossInstanceCallArgs: A exports add(i32,i32)->i32; B calls it as
// addBoth() = add(20, 22). Exercises argument marshaling across the swap.
func TestCrossInstanceCallArgs(t *testing.T) {
	modA := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("add", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x6a, 0x0b}))), // local.get 0; local.get 1; i32.add; end
	)
	inA, err := Instantiate(MustCompile(modA), nil)
	if err != nil {
		t.Fatalf("instantiate A: %v", err)
	}
	defer inA.Close()
	addExport, err := inA.ExportedFunc("add")
	if err != nil {
		t.Fatalf("export add: %v", err)
	}

	// B: type0 = (i32,i32)->i32 (the import); type1 = ()->i32 (addBoth).
	imp := funcImportEntry("env", "add", 0)
	modB := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))), // local func addBoth, type 1
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("addBoth", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0x14, 0x41, 0x16, 0x10, 0x00, 0x0b}))), // i32.const 20; i32.const 22; call 0; end
	)
	inB, err := Instantiate(MustCompile(modB), Imports{"env.add": addExport})
	if err != nil {
		t.Fatalf("instantiate B: %v", err)
	}
	defer inB.Close()
	r, err := inB.Invoke("addBoth")
	if err != nil {
		t.Fatalf("invoke addBoth: %v", err)
	}
	if AsI32(r[0]) != 42 {
		t.Fatalf("cross-instance add returned %d, want 42", AsI32(r[0]))
	}
}
