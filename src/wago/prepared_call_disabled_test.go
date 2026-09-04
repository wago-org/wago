package wago

import (
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	wruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/wasmtest"
)

func TestPreparedCallDisabledClearsTrapBeforeNextInvoke(t *testing.T) {
	savedPrepared := preparedCallEnabled
	savedPrivate := invokePrivateEntryEnabled
	preparedCallEnabled = false
	invokePrivateEntryEnabled = false
	defer func() {
		preparedCallEnabled = savedPrepared
		invokePrivateEntryEnabled = savedPrivate
	}()

	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.FuncRef}),
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(4, wasmtest.Vec([]byte{wasm.MustEncodeValType(wasm.FuncRef), 0x00, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("get", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("nop", byte(wasm.ExternFunc), 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x25, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x0b}),
		)),
	)
	compiled, err := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2).Compile(module)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer instance.Close()

	if _, err := instance.Invoke("get", I32(1)); err == nil {
		t.Fatal("out-of-bounds table.get did not trap")
	} else {
		var trap *wruntime.TrapError
		if !errors.As(err, &trap) || trap.Code != wruntime.TrapTableOutOfBounds {
			t.Fatalf("table.get error = %v, want table out-of-bounds trap", err)
		}
	}
	if _, err := instance.Invoke("nop"); err != nil {
		t.Fatalf("successful call after trap reused the stale trap: %v", err)
	}
	prepared, err := instance.PrepareFunction("nop")
	if err != nil {
		t.Fatalf("prepare nop: %v", err)
	}
	if prepared.privateFast {
		t.Fatal("WAGO_PREPARED_CALL=0 retained the private prepared-call path")
	}
}
