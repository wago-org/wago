//go:build linux && amd64 && !tinygo

package wago

import (
	"context"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

// A funcref written into a shared table embeds the producing instance's code
// pointer and home memory. If the producer closes after a successful table.set,
// the descriptor it left behind must stay backed by live code: the table retains
// the producer until the entry is overwritten or the table closes. Regression
// for the use-after-free where a closed producer's arena was freed while another
// importer could still call_indirect its funcref.
func sharedTableClearerModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(tableTestImportTable("env", "t", 1, 1))),
		tableTestFuncSection(0, 0),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("clear", 0, 0),
			wasmtest.ExportEntry("trap", 0, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(tableTestBody(tableTestI32Const(0), tableTestRefNullFunc(), []byte{0x26, 0x00})),
			wasmtest.Code(tableTestBody(tableTestI32Const(1), tableTestRefNullFunc(), []byte{0x26, 0x00})),
		)),
	)
}

func TestClosedProducerFuncrefInSharedTableStaysCallable(t *testing.T) {
	tbl, err := NewTable(1, 1)
	if err != nil {
		t.Fatalf("NewTable: %v", err)
	}
	defer tbl.Close()

	setterMod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(2, wasmtest.Vec(tableTestImportTable("env", "t", 1, 1))),
		tableTestFuncSection(0, 1),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("set0", 0, 1))),
		wasmtest.Section(9, wasmtest.Vec(tableTestDeclarativeElem(0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(tableTestBody(tableTestI32Const(123))),
			wasmtest.Code(tableTestBody(tableTestI32Const(0), tableTestRefFunc(0), []byte{0x26, 0x00})),
		)),
	)
	callerMod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(tableTestImportTable("env", "t", 1, 1))),
		tableTestFuncSection(1),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("callAt", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(tableTestBody(tableTestLocalGet(0), tableTestCallIndirect(0, 0))))),
	)

	setter := tableTestInstantiateWithImports(t, setterMod, Imports{"env.t": tbl})
	caller := tableTestInstantiateWithImports(t, callerMod, Imports{"env.t": tbl})
	defer caller.Close()

	if _, err := setter.Invoke("set0"); err != nil {
		t.Fatalf("set0: %v", err)
	}

	// Close the producer before the reader calls. The table still holds the
	// producer's local funcref, so the producer must remain a retained root.
	if err := setter.Close(); err != nil {
		t.Fatalf("setter Close: %v", err)
	}
	if !setter.hasResourceRoots() {
		t.Fatal("producer not retained by shared table after close; its funcref would dangle")
	}
	if got := tableTestCallI32(t, caller, "callAt", I32(0)); got != 123 {
		t.Fatalf("callAt(0) after producer close = %d, want 123 (producer retained)", got)
	}
}

func TestSharedTableOverwriteReleasesClosedProducerAtomically(t *testing.T) {
	tbl, err := NewTable(1, 1)
	if err != nil {
		t.Fatalf("NewTable: %v", err)
	}
	defer tbl.Close()

	setterMod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(tableTestImportTable("env", "t", 1, 1))),
		tableTestFuncSection(0, 0),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("set", 0, 1))),
		wasmtest.Section(9, wasmtest.Vec(tableTestDeclarativeElem(0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(tableTestBody()),
			wasmtest.Code(tableTestBody(tableTestI32Const(0), tableTestRefFunc(0), []byte{0x26, 0x00})),
		)),
	)
	setter := tableTestInstantiateWithImports(t, setterMod, Imports{"env.t": tbl})
	clearer := tableTestInstantiateWithImports(t, sharedTableClearerModule(), Imports{"env.t": tbl})
	defer clearer.Close()

	if _, err := setter.Invoke("set"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := setter.Close(); err != nil {
		t.Fatalf("setter Close: %v", err)
	}
	if !setter.hasResourceRoots() {
		t.Fatal("closed producer was not retained by the shared table")
	}

	if _, err := clearer.Invoke("trap"); err == nil {
		t.Fatal("out-of-bounds table.set unexpectedly succeeded")
	}
	if !setter.hasResourceRoots() {
		t.Fatal("trapping table.set released the producer root despite leaving the descriptor unchanged")
	}
	if _, err := clearer.Invoke("clear"); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if setter.hasResourceRoots() {
		t.Fatal("successful table.set overwrite did not release the closed producer root")
	}
	if setter.hasPhysicalResources() {
		t.Fatal("closed producer resources remained mapped after its final table root was overwritten")
	}
}

// The single-slot analog for funcref globals: a producer that writes its local
// funcref into an imported mutable funcref global via global.set and then closes
// must be retained by the global's owner until the value is overwritten or the
// global closes.
func TestClosedProducerFuncrefInSharedGlobalIsRetained(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()

	g, err := rt.NewFuncRefGlobal(FuncRef{}, true)
	if err != nil {
		t.Fatalf("NewFuncRefGlobal: %v", err)
	}

	producer := mustCompileWat(rt, t, `(module
		(import "env" "g" (global (mut funcref)))
		(func $f)
		(elem declare func $f)
		(func (export "store") (global.set 0 (ref.func $f))))`)
	in, err := rt.Instantiate(context.Background(), producer, WithImports(Imports{"env.g": g}))
	if err != nil {
		t.Fatalf("instantiate producer: %v", err)
	}
	if _, err := in.Invoke("store"); err != nil {
		t.Fatalf("store: %v", err)
	}

	if err := in.Close(); err != nil {
		t.Fatalf("producer Close: %v", err)
	}
	if !in.hasResourceRoots() {
		t.Fatal("producer not retained by funcref global after close; its descriptor would dangle")
	}

	// Closing the global (now that its only importer is gone) releases the root.
	if err := g.Close(); err != nil {
		t.Fatalf("global Close: %v", err)
	}
	if in.hasResourceRoots() {
		t.Fatal("producer root not released after global close")
	}
}

func TestSharedGlobalHostOverwriteReleasesClosedProducer(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()

	g, err := rt.NewFuncRefGlobal(FuncRef{}, true)
	if err != nil {
		t.Fatalf("NewFuncRefGlobal: %v", err)
	}
	defer g.Close()

	producer := mustCompileWat(rt, t, `(module
		(import "env" "g" (global (mut funcref)))
		(func $f)
		(elem declare func $f)
		(func (export "store") (global.set 0 (ref.func $f))))`)
	in, err := rt.Instantiate(context.Background(), producer, WithImports(Imports{"env.g": g}))
	if err != nil {
		t.Fatalf("instantiate producer: %v", err)
	}
	if _, err := in.Invoke("store"); err != nil {
		t.Fatalf("store: %v", err)
	}
	if err := in.Close(); err != nil {
		t.Fatalf("producer Close: %v", err)
	}
	if !in.hasResourceRoots() {
		t.Fatal("closed producer was not retained by the shared global")
	}

	if err := g.SetValue(ValueFuncRef(NullFuncRef())); err != nil {
		t.Fatalf("SetValue(null): %v", err)
	}
	if in.hasResourceRoots() {
		t.Fatal("host global overwrite did not release the closed producer root")
	}
	if in.hasPhysicalResources() {
		t.Fatal("closed producer resources remained mapped after its final global root was overwritten")
	}
}
