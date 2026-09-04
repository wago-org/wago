//go:build amd64 && wago_guardpage && !tinygo

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func TestAMD64SignalBackedPreparedCallClosureBypassesGuardActivation(t *testing.T) {
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x10, 0x01, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x41, 0x07, 0x6a, 0x0b}),
		)),
	)
	compiled, err := Compile(NewRuntimeConfig().
		WithBoundsChecks(BoundsChecksSignalsBased).
		WithCompiler(CompilerDragline).
		WithTarget(TargetNative), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	prepared, err := instance.PrepareFunction("run")
	if err != nil {
		t.Fatal(err)
	}
	if !prepared.privateFast || !prepared.isolatedFast || !prepared.privateLifetime {
		t.Fatalf("signal-backed closure selected private=%t isolated=%t lifetime=%t", prepared.privateFast, prepared.isolatedFast, prepared.privateLifetime)
	}
	results, err := prepared.Invoke1(35)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0] != 42 {
		t.Fatalf("run(35) = %v, want [42]", results)
	}
}

func TestAMD64SignalBackedPreparedMemoryClosureKeepsGuardActivation(t *testing.T) {
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x10, 0x01, 0x0b}),
			wasmtest.Code([]byte{0x41, 0x00, 0x28, 0x02, 0x00, 0x0b}),
		)),
	)
	compiled, err := Compile(NewRuntimeConfig().
		WithBoundsChecks(BoundsChecksSignalsBased).
		WithCompiler(CompilerDragline).
		WithTarget(TargetNative), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	prepared, err := instance.PrepareFunction("run")
	if err != nil {
		t.Fatal(err)
	}
	if prepared.privateFast || prepared.isolatedFast || prepared.privateLifetime {
		t.Fatalf("memory closure selected private=%t isolated=%t lifetime=%t", prepared.privateFast, prepared.isolatedFast, prepared.privateLifetime)
	}
	results, err := prepared.Invoke0()
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0] != 0 {
		t.Fatalf("run() = %v, want [0]", results)
	}
}
