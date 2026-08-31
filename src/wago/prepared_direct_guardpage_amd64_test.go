//go:build wago_guardpage && amd64 && !tinygo

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func TestDraglineAMD64SignalBackedLeafUsesDirectPreparedEntry(t *testing.T) {
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x41, 0x07, 0x6a, 0x0b}))),
	)
	compiled, err := Compile(NewRuntimeConfig().
		WithBoundsChecks(BoundsChecksSignalsBased).
		WithCompiler(CompilerDragline).
		WithTarget(TargetNative), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	if !directLeafPreparedEntry(compiled.InternalEntry[0]) {
		t.Fatal("signal-backed AMD64 integer leaf did not publish its direct leaf entry")
	}
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	result, err := instance.Invoke("run", I32(5))
	if err != nil || len(result) != 1 || AsI32(result[0]) != 12 {
		t.Fatalf("wrapper run(5) = %v, %v; want 12", result, err)
	}
	prepared, err := instance.PrepareFunction("run")
	if err != nil {
		t.Fatal(err)
	}
	if !prepared.directIntFast || !prepared.directLeafIntFast {
		t.Fatalf("signal-backed AMD64 integer leaf selected direct=%t leaf=%t", prepared.directIntFast, prepared.directLeafIntFast)
	}
	result, err = prepared.Invoke1(I32(5))
	if err != nil || len(result) != 1 || AsI32(result[0]) != 12 {
		t.Fatalf("prepared run(5) = %v, %v; want 12", result, err)
	}
}
