package shared

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func compileEmbeddedLinkTestModule(t *testing.T, bytes []byte) *EmbeddedModule {
	t.Helper()
	m, err := wasm.DecodeModule(bytes)
	if err != nil {
		t.Fatal(err)
	}
	cm, err := CompileEmbeddedModule(m, EmbeddedModuleOptions{}, "test", 1, []byte{0}, func(int, *wasm.CompType, []wasm.LocalRun, []byte) ([]byte, error) {
		return []byte{0}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return cm
}

func embeddedLinkProvider(t *testing.T) *EmbeddedModule {
	t.Helper()
	return compileEmbeddedLinkTestModule(t, wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 1, 2, 3})),
		wasmtest.Section(5, wasmtest.Vec([]byte{1, 1, 2})),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I64, true, []byte{0x42, 0, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("f", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("table", byte(wasm.ExternTable), 0),
			wasmtest.ExportEntry("memory", byte(wasm.ExternMem), 0),
			wasmtest.ExportEntry("global", byte(wasm.ExternGlobal), 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 1, 0x0b}))),
	))
}

func embeddedLinkConsumer(t *testing.T) *EmbeddedModule {
	t.Helper()
	functionImport := append(wasmtest.Name("provider"), wasmtest.Name("f")...)
	functionImport = append(functionImport, 0, 0)
	tableImport := append(wasmtest.Name("provider"), wasmtest.Name("table")...)
	tableImport = append(tableImport, 1, 0x70, 1, 1, 4)
	memoryImport := append(wasmtest.Name("provider"), wasmtest.Name("memory")...)
	memoryImport = append(memoryImport, 2, 1, 1, 3)
	return compileEmbeddedLinkTestModule(t, wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(
			functionImport,
			tableImport,
			memoryImport,
			wasmtest.GlobalImportEntry("provider", "global", wasm.I64, true),
		)),
	))
}

func TestResolveEmbeddedLinksValidatesCompleteContracts(t *testing.T) {
	provider := embeddedLinkProvider(t)
	consumer := embeddedLinkConsumer(t)
	plan, err := ResolveEmbeddedLinks([]EmbeddedNamedModule{{Name: "provider", Module: provider}, {Name: "consumer", Module: consumer}})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Bindings) != 4 {
		t.Fatalf("bindings=%+v", plan.Bindings)
	}
	for i, binding := range plan.Bindings {
		if binding.ConsumerModule != 1 || binding.ImportIndex != i || binding.ProviderModule != 0 || binding.ExportIndex != i {
			t.Fatalf("binding %d=%+v", i, binding)
		}
	}
	if consumer.Imports[0].FunctionTypeID != provider.FunctionSignatures[0].TypeID {
		t.Fatalf("consumer type=%#x provider=%#x", consumer.Imports[0].FunctionTypeID, provider.FunctionSignatures[0].TypeID)
	}
}

func TestResolveEmbeddedLinksRejectsMismatches(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(provider, consumer *EmbeddedModule)
	}{
		{name: "function", mutate: func(_ *EmbeddedModule, consumer *EmbeddedModule) {
			consumer.Imports[0].Results = []wasm.ValType{wasm.I64}
		}},
		{name: "table limits", mutate: func(provider, _ *EmbeddedModule) { provider.Table.Maximum = 5 }},
		{name: "memory limits", mutate: func(provider, _ *EmbeddedModule) { provider.Memory.Minimum = 0 }},
		{name: "global mutability", mutate: func(provider, _ *EmbeddedModule) { provider.Globals[0].Mutable = false }},
		{name: "kind", mutate: func(provider, _ *EmbeddedModule) { provider.Exports[0].Kind = wasm.ExternGlobal }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := embeddedLinkProvider(t)
			consumer := embeddedLinkConsumer(t)
			test.mutate(provider, consumer)
			if _, err := ResolveEmbeddedLinks([]EmbeddedNamedModule{{Name: "provider", Module: provider}, {Name: "consumer", Module: consumer}}); err == nil {
				t.Fatal("mismatch accepted")
			}
		})
	}
}

func TestResolveEmbeddedLinksRejectsMissingAndDuplicateModules(t *testing.T) {
	consumer := embeddedLinkConsumer(t)
	if _, err := ResolveEmbeddedLinks([]EmbeddedNamedModule{{Name: "consumer", Module: consumer}}); err == nil {
		t.Fatal("missing provider accepted")
	}
	provider := embeddedLinkProvider(t)
	if _, err := ResolveEmbeddedLinks([]EmbeddedNamedModule{{Name: "provider", Module: provider}, {Name: "provider", Module: consumer}}); err == nil {
		t.Fatal("duplicate module name accepted")
	}
}
