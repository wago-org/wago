package wago

import (
	"context"
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

type callerInvokerPlugin struct {
	invoker *CallerInvoker
}

func (p *callerInvokerPlugin) Register(reg *Registrar) error {
	var err error
	p.invoker, err = reg.HostCallerInvoker()
	if err != nil {
		return err
	}
	imports, err := reg.HostImports()
	if err != nil {
		return err
	}
	module, err := imports.Module("env")
	if err != nil {
		return err
	}
	module.Func("outer", func(caller HostModule, params, results []uint64) {
		nested, err := p.invoker.Invoke(context.Background(), caller, "callback", params...)
		if err != nil {
			panic(HostTrap{Err: err})
		}
		copy(results, nested)
	}).Params(ValI32).Results(ValI32)
	return nil
}

func TestCallerInvokerReentersOnlyActiveGuest(t *testing.T) {
	plugin := &callerInvokerPlugin{}
	definition := testDefinition("example.com/caller-invoker")
	definition.Authorities = []AuthorityRequest{
		{Name: AuthorityHostCallerInvoke, Mode: AuthorityRequired, Reason: "test active re-entry"},
		{Name: AuthorityHostImportDefine, Mode: AuthorityRequired, Reason: "test import", Scope: AuthorityScope{Modules: []string{"env"}}},
	}
	provider := PluginProvider{Definition: definition, New: func() Plugin { return plugin }}
	rt := NewRuntime()
	defer rt.Close()
	if err := rt.LoadPlugins(context.Background(), testSet(t, provider)); err != nil {
		t.Fatal(err)
	}
	module, err := rt.Compile(callerInvokerModule())
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	instance, err := rt.Instantiate(context.Background(), module)
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	results, err := instance.Invoke("run", 41)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || AsI32(results[0]) != 42 {
		t.Fatalf("run(41) = %v, want 42", results)
	}
	if _, err := plugin.invoker.Invoke(context.Background(), nil, "callback", 1); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("retained invocation = %v, want permission denied", err)
	}
}

func callerInvokerModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(importEntry("env", "outer", 0, 0))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("run", 0, 1),
			wasmtest.ExportEntry("callback", 0, 2),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x10, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x41, 0x01, 0x6a, 0x0b}),
		)),
	)
}
