//go:build wago_guardpage && (amd64 || arm64) && (linux || darwin || windows)

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestPreparedDirectMemoryFreeEntryWithSignalBounds(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksSignalsBased), benchAddOneModule())
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !compiled.directPreparedAt(0) {
		t.Fatal("memory-free function did not retain the direct-entry proof")
	}
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	fn, err := in.PrepareFunction("f")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if !fn.directIntFast || !fn.directIsolated {
		t.Fatalf("signal-bounds direct/isolated selection = %v/%v, want true/true", fn.directIntFast, fn.directIsolated)
	}
	got, err := fn.Invoke1(I32(41))
	if err != nil || len(got) != 1 || AsI32(got[0]) != 42 {
		t.Fatalf("invoke = %v, %v; want [42], nil", got, err)
	}
}

func TestPreparedDirectSignalEntryClearsPriorExportTrap(t *testing.T) {
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("trap", 0, 0),
			wasmtest.ExportEntry("add", 0, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x28, 0x02, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x41, 0x01, 0x6a, 0x0b}),
		)),
	)
	compiled, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksSignalsBased), module)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	if _, err := in.Invoke("trap", I32(65536)); err == nil {
		t.Fatal("out-of-bounds load did not trap")
	}
	add, err := in.PrepareFunction("add")
	if err != nil {
		t.Fatalf("prepare add: %v", err)
	}
	if !add.directIntFast {
		t.Fatal("memory-free add did not select direct entry")
	}
	if got, err := add.Invoke1(I32(41)); err != nil || len(got) != 1 || AsI32(got[0]) != 42 {
		t.Fatalf("direct add after prior export trap = %v, %v; want [42], nil", got, err)
	}
}
