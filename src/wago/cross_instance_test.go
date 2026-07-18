//go:build linux && (amd64 || riscv64)

package wago

import (
	"context"
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func crossInstanceTrapCode(err error) TrapCode {
	var trap *TrapError
	if errors.As(err, &trap) {
		return trap.Code
	}
	return TrapNone
}

func funcImportEntry(module, name string, typeIdx uint32) []byte {
	out := append(wasmtest.Name(module), wasmtest.Name(name)...)
	out = append(out, 0x00) // ExternFunc
	return append(out, wasmtest.ULEB(typeIdx)...)
}

// TestCrossInstanceMemoryShared: A owns a memory with data; B imports A's memory,
// writes into it, and A observes the write (shared bytes).
func TestCrossInstanceMemoryShared(t *testing.T) {
	t.Setenv("WAGO_BOUNDS", "explicit") // pin the explicit-bounds path (guard-page sharing is covered in memory_guardpage_test.go)
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
	inA, err := Instantiate(MustCompile(modA), InstantiateOptions{})
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
	inB, err := Instantiate(MustCompile(modB), InstantiateOptions{Imports: Imports{"env.mem": memImport}})
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
	inA, err := Instantiate(MustCompile(modA), InstantiateOptions{})
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
	inB, err := Instantiate(MustCompile(modB), InstantiateOptions{Imports: Imports{"env.g": gImport}})
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
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("f", 0, 0),
			wasmtest.ExportEntry("trap", 0, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 0x2a, 0x0b}), // i32.const 42; end
			wasmtest.Code([]byte{0x00, 0x0b}),       // unreachable; end
		)),
	)
	inA, err := Instantiate(MustCompile(modA), InstantiateOptions{})
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
	cB, err := Compile(nil, modB)
	if err != nil {
		t.Fatalf("compile B: %v", err)
	}
	if !forceSyncHostImports && !cB.needsLink {
		t.Fatalf("B should need link (returning import)")
	}
	inB, err := Instantiate(cB, InstantiateOptions{Imports: Imports{"env.f": fExport}})
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
	if _, err := inA.Invoke("trap"); crossInstanceTrapCode(err) != TrapUnreachable {
		t.Fatalf("producer trap after cross-instance entry = %v, want unreachable", err)
	}
	preparedTrap, err := inA.PrepareFunction("trap")
	if err != nil {
		t.Fatalf("prepare producer trap: %v", err)
	}
	if _, err := inB.Invoke("call"); err != nil {
		t.Fatalf("second cross-instance call: %v", err)
	}
	if _, err := preparedTrap.Invoke(); crossInstanceTrapCode(err) != TrapUnreachable {
		t.Fatalf("prepared producer trap after cross-instance entry = %v, want unreachable", err)
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
	inA, err := Instantiate(MustCompile(modA), InstantiateOptions{})
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
	inB, err := Instantiate(MustCompile(modB), InstantiateOptions{Imports: Imports{"env.add": addExport}})
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

func TestCrossInstanceIndirectCallReloadsModulePinnedGlobal(t *testing.T) {
	set77Body := append([]byte{0x41}, wasmtest.SLEB32(77)...)
	set77Body = append(set77Body, 0x24, 0x00, 0x0b) // i32.const 77; global.set 0; end

	// The caller's block/loop contains three global.get operations under one loop,
	// giving imported mutable global 0 enough static hotness for the module-global
	// pin heuristic. The indirect call then crosses to A and mutates the same cell;
	// the final global.get must reload the caller's module-pinned register from the
	// shared cell instead of observing the stale prologue value.
	modA := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 0x05, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("g", 3, 0),
			wasmtest.ExportEntry("set77", 0, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(set77Body))),
	)
	inA, err := Instantiate(MustCompile(modA), InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate A: %v", err)
	}
	defer inA.Close()
	setExport, err := inA.ExportedFunc("set77")
	if err != nil {
		t.Fatalf("export set77: %v", err)
	}
	gExport, err := inA.ExportedGlobalObject("g")
	if err != nil {
		t.Fatalf("export g: %v", err)
	}

	globalImport := wasmtest.GlobalImportEntry("env", "g", wasm.I32, true)
	body := []byte{
		0x02, 0x40, // block
		0x03, 0x40, // loop
		0x23, 0x00, 0x1a, // global.get 0; drop
		0x23, 0x00, 0x1a, // global.get 0; drop
		0x23, 0x00, 0x1a, // global.get 0; drop
		0x0c, 0x01, // br 1 (exit block after one iteration)
		0x0b,       // end loop
		0x0b,       // end block
		0x41, 0x00, // i32.const 0 (table index)
		0x11, 0x00, 0x00, // call_indirect type 0 table 0
		0x23, 0x00, // global.get 0
		0x0b, // end
	}
	modB := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(funcImportEntry("env", "set", 0), globalImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x01})), // funcref table min=1
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 1))),
		wasmtest.Section(9, wasmtest.Vec([]byte{0x00, 0x41, 0x00, 0x0b, 0x01, 0x00})), // elem (i32.const 0) [imported set]
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	inB, err := Instantiate(MustCompile(modB), InstantiateOptions{Imports: Imports{"env.set": setExport, "env.g": gExport}})
	if err != nil {
		t.Fatalf("instantiate B: %v", err)
	}
	defer inB.Close()
	res, err := inB.Invoke("call")
	if err != nil {
		t.Fatalf("invoke B.call: %v", err)
	}
	if got := AsI32(res[0]); got != 77 {
		t.Fatalf("B.call = %d, want 77 from cross-instance indirect callee", got)
	}
}

func TestCrossInstanceCallMultiValueImport(t *testing.T) {
	wantI64 := int64(0x1020304050607080)
	wantI32 := int32(0x10203040)

	modA := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(
			[]wasm.ValType{wasm.I32, wasm.I64},
			[]wasm.ValType{wasm.I64, wasm.I32},
		))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("reorder", 0, 0))),
		// local.get 1; local.get 0; end
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x01, 0x20, 0x00, 0x0b}))),
	)
	inA, err := Instantiate(MustCompile(modA), nil)
	if err != nil {
		t.Fatalf("instantiate A: %v", err)
	}
	defer inA.Close()
	reorderExport, err := inA.ExportedFunc("reorder")
	if err != nil {
		t.Fatalf("export reorder: %v", err)
	}

	imp := funcImportEntry("env", "reorder", 0)
	body := []byte{0x41}
	body = append(body, wasmtest.SLEB32(wantI32)...)
	body = append(body, 0x42)
	body = append(body, wasmtest.SLEB64(wantI64)...)
	body = append(body, 0x10, 0x00, 0x0b) // call imported reorder; end
	modB := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I64}, []wasm.ValType{wasm.I64, wasm.I32}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I64, wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	inB, err := Instantiate(MustCompile(modB), Imports{"env.reorder": reorderExport})
	if err != nil {
		t.Fatalf("instantiate B: %v", err)
	}
	defer inB.Close()

	raw, err := inB.Invoke("call")
	if err != nil {
		t.Fatalf("invoke cross-instance multi-value call: %v", err)
	}
	if len(raw) != 2 {
		t.Fatalf("cross-instance call returned %d slot(s), want 2", len(raw))
	}
	if got := AsI64(raw[0]); got != wantI64 {
		t.Fatalf("cross-instance i64 result = %d, want %d", got, wantI64)
	}
	if got := AsI32(raw[1]); got != wantI32 {
		t.Fatalf("cross-instance i32 result = %d, want %d", got, wantI32)
	}

	out, err := inB.Call(context.Background(), "call")
	if err != nil {
		t.Fatalf("typed call cross-instance multi-value: %v", err)
	}
	if len(out) != 2 || out[0].Type() != ValI64 || out[0].I64() != wantI64 || out[1].Type() != ValI32 || out[1].I32() != wantI32 {
		t.Fatalf("typed cross-instance call = %v, want i64(%d), i32(%d)", out, wantI64, wantI32)
	}
}

func TestCrossInstanceCallV128(t *testing.T) {
	if !hostSupportsSIMD() {
		t.Skip("host SIMD unavailable")
	}
	vec := V128{0xde, 0xad, 0xbe, 0xef, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}
	modA := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("id", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x0b}))), // local.get 0; end
	)
	inA, err := Instantiate(MustCompile(modA), InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate A: %v", err)
	}
	defer inA.Close()
	idExport, err := inA.ExportedFunc("id")
	if err != nil {
		t.Fatalf("export id: %v", err)
	}

	imp := funcImportEntry("env", "id", 0)
	body := append([]byte{0xfd, 0x0c}, vec[:]...) // v128.const vec
	body = append(body, 0x10, 0x00, 0x0b)         // call 0; end
	modB := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.V128}),
		)),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	cB := MustCompile(modB)
	if !cB.needsLink {
		t.Fatal("v128 function import should need link")
	}
	inB, err := Instantiate(cB, InstantiateOptions{Imports: Imports{"env.id": idExport}})
	if err != nil {
		t.Fatalf("instantiate B: %v", err)
	}
	defer inB.Close()
	res, err := inB.Invoke("call")
	if err != nil {
		t.Fatalf("invoke call: %v", err)
	}
	if got := hostV128FromSlots(res[0], res[1]); got != vec {
		t.Fatalf("cross-instance v128 result = % x, want % x", got, vec)
	}

	// Re-exporting the imported function exercises Instance.invokeLocal's public
	// slot accounting for v128 params/results.
	modReexport := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128}))),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("id", 0, 0))),
	)
	inReexport, err := Instantiate(MustCompile(modReexport), InstantiateOptions{Imports: Imports{"env.id": idExport}})
	if err != nil {
		t.Fatalf("instantiate re-export: %v", err)
	}
	defer inReexport.Close()
	lo, hi := hostV128Slots(vec)
	res, err = inReexport.Invoke("id", lo, hi)
	if err != nil {
		t.Fatalf("invoke re-exported id: %v", err)
	}
	if got := hostV128FromSlots(res[0], res[1]); got != vec {
		t.Fatalf("re-exported v128 result = % x, want % x", got, vec)
	}
}
