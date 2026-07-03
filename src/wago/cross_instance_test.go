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
