//go:build wago_guardpage && arm64 && !tinygo && (linux || darwin)

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func TestDraglineSignalBackedLeafUsesDirectPreparedEntry(t *testing.T) {
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
		t.Fatal("signal-backed integer leaf did not publish its direct leaf entry")
	}
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	prepared, err := instance.PrepareFunction("run")
	if err != nil {
		t.Fatal(err)
	}
	if !prepared.directIntFast || !prepared.directLeafIntFast {
		t.Fatalf("signal-backed integer leaf selected direct=%t leaf=%t", prepared.directIntFast, prepared.directLeafIntFast)
	}
	result, err := prepared.Invoke1(I32(5))
	if err != nil || len(result) != 1 || AsI32(result[0]) != 12 {
		t.Fatalf("prepared run(5) = %v, %v; want 12", result, err)
	}
}

func TestDraglineSignalBackedContextFreeLoopUsesPrivatePreparedEntry(t *testing.T) {
	body := []byte{
		0x02, 0x40, 0x03, 0x40,
		0x20, 0x00, 0x45, 0x0d, 0x01,
		0x20, 0x01, 0x20, 0x00, 0x6a, 0x21, 0x01,
		0x20, 0x00, 0x41, 0x01, 0x6b, 0x21, 0x00,
		0x0c, 0x00, 0x0b, 0x0b, 0x20, 0x01, 0x0b,
	}
	function := append([]byte{0x01, 0x01, 0x7f}, body...)
	code := append(wasmtest.ULEB(uint32(len(function))), function...)
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(code)),
	)
	compiled, err := Compile(NewRuntimeConfig().
		WithBoundsChecks(BoundsChecksSignalsBased).
		WithCompiler(CompilerDragline).
		WithTarget(TargetNative), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	if !contextFreeLoopPreparedEntry(compiled.InternalEntry[0]) {
		t.Fatal("signal-backed context-free loop did not publish its private-wrapper proof")
	}
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	prepared, err := instance.PrepareFunction("run")
	if err != nil {
		t.Fatal(err)
	}
	if !prepared.privateFast || !prepared.isolatedFast || !prepared.privateLifetime || prepared.directLeafIntFast || prepared.directTrapIntFast {
		t.Fatalf("signal-backed loop selected private=%t isolated=%t lifetime=%t leaf=%t trap=%t", prepared.privateFast, prepared.isolatedFast, prepared.privateLifetime, prepared.directLeafIntFast, prepared.directTrapIntFast)
	}
	result, err := prepared.Invoke1(I32(10))
	if err != nil || len(result) != 1 || AsI32(result[0]) != 55 {
		t.Fatalf("prepared run(10) = %v, %v; want 55", result, err)
	}
}
