package wago

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

type importIdentity struct {
	module string
	name   string
	kind   ImportKind
}

func exactImportIdentityModule(includeTag bool) []byte {
	entry := func(module, name string, tail ...byte) []byte {
		out := append(wasmtest.Name(module), wasmtest.Name(name)...)
		return append(out, tail...)
	}
	imports := [][]byte{
		entry("a.b", "c", 0x00, 0x00), // function, type 0
		entry("a", "bc", 0x00, 0x00),
		entry("", "empty.module", 0x00, 0x00),
		entry("empty.name", "", 0x00, 0x00),
		entry("nul\x00.mod", "field\x00.name", 0x00, 0x00),
		entry("table.mod", "table.名", 0x01, 0x70, 0x00, 0x00),
		entry("memory.mod", "memory.名", 0x02, 0x00, 0x00),
		wasmtest.GlobalImportEntry("global.mod", "global.名", wasm.I32, false),
	}
	if includeTag {
		imports = append(imports, entry("tag.mod", "tag.名", 0x04, 0x00, 0x00)) // tag attribute 0, type 0
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(imports...)),
	)
}

func exactImportIdentities(includeTag bool) []importIdentity {
	identities := []importIdentity{
		{module: "a.b", name: "c", kind: ImportFunc},
		{module: "a", name: "bc", kind: ImportFunc},
		{module: "", name: "empty.module", kind: ImportFunc},
		{module: "empty.name", name: "", kind: ImportFunc},
		{module: "nul\x00.mod", name: "field\x00.name", kind: ImportFunc},
		{module: "global.mod", name: "global.名", kind: ImportGlobal},
		{module: "memory.mod", name: "memory.名", kind: ImportMemory},
		{module: "table.mod", name: "table.名", kind: ImportTable},
	}
	if includeTag {
		identities = append(identities, importIdentity{module: "tag.mod", name: "tag.名", kind: ImportTag})
	}
	return identities
}

func moduleImportIdentities(module *Module) []importIdentity {
	imports := module.Imports()
	out := make([]importIdentity, len(imports))
	for i, spec := range imports {
		out[i] = importIdentity{module: spec.Module, name: spec.Name, kind: spec.Kind}
	}
	return out
}

func TestRuntimeCompilePreservesExactImportIdentitiesAcrossArtifact(t *testing.T) {
	features := CoreFeaturesV3 & SupportedFeatures()
	includeTag := features.IsEnabled(CoreFeatureExceptionHandling)
	cfg := NewRuntimeConfig().WithCoreFeatures(features).WithBoundsChecks(BoundsChecksExplicit)
	rt := NewRuntime(WithRuntimeConfig(cfg))
	defer rt.Close()

	module, err := rt.Compile(exactImportIdentityModule(includeTag))
	if err != nil {
		t.Fatalf("Runtime.Compile: %v", err)
	}
	want := exactImportIdentities(includeTag)
	if got := moduleImportIdentities(module); !reflect.DeepEqual(got, want) {
		t.Fatalf("source import identities = %#v, want %#v", got, want)
	}

	metadata := module.Metadata()
	gotMetadata := make([]importIdentity, 0, len(want))
	for i := 0; i < metadata.FuncImportCount; i++ {
		gotMetadata = append(gotMetadata, importIdentity{module: metadata.Functions[i].ImportModule, name: metadata.Functions[i].ImportName, kind: ImportFunc})
	}
	gotMetadata = append(gotMetadata,
		importIdentity{module: metadata.Globals[0].ImportModule, name: metadata.Globals[0].ImportName, kind: ImportGlobal},
		importIdentity{module: metadata.Memories[0].ImportModule, name: metadata.Memories[0].ImportName, kind: ImportMemory},
		importIdentity{module: metadata.Tables[0].ImportModule, name: metadata.Tables[0].ImportName, kind: ImportTable},
	)
	if includeTag {
		gotMetadata = append(gotMetadata, importIdentity{module: metadata.Tags[0].ImportModule, name: metadata.Tags[0].ImportName, kind: ImportTag})
	}
	if !reflect.DeepEqual(gotMetadata, want) {
		t.Fatalf("source metadata identities = %#v, want %#v", gotMetadata, want)
	}

}

func TestRuntimePluginBindingRequiresExactImportIdentity(t *testing.T) {
	const flatKey = "a.b.c"
	rt := NewRuntime()
	defer rt.Close()
	bindingKey := importBindingMapKey("a", "b.c")
	rt.imports[bindingKey] = slotHostFunc(func(HostModule, []uint64, []uint64) {})
	rt.importMeta[bindingKey] = &registeredImport{
		module: "a", name: "b.c",
		cap: Capability("host.exact"), hasCap: true,
	}

	moduleBytes := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("a.b"), wasmtest.Name("c")...), 0x00, 0x00))),
	)
	module, err := rt.Compile(moduleBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	imports := module.Imports()
	if len(imports) != 1 || imports[0].Module != "a.b" || imports[0].Name != "c" || imports[0].Provided || imports[0].HasCapability {
		t.Fatalf("colliding plugin import metadata = %#v", imports)
	}
	if _, err := rt.Instantiate(context.Background(), module); !errors.Is(err, ErrMissingImport) {
		t.Fatalf("colliding plugin binding error = %v, want ErrMissingImport", err)
	}

	if instance, err := rt.Instantiate(context.Background(), module, WithImports(testImports(flatKey, slotHostFunc(func(HostModule, []uint64, []uint64) {})))); err == nil || instance != nil || !errors.Is(err, ErrMissingImport) {
		t.Fatalf("distinct exact override = %v, %v; want ErrMissingImport", instance, err)
	}

	instance, err := rt.Instantiate(context.Background(), module, WithImport("a.b", "c", slotHostFunc(func(HostModule, []uint64, []uint64) {})))
	if err != nil {
		t.Fatalf("exact override: %v", err)
	}
	if err := instance.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeAcceptsDistinctExactImportIdentities(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	moduleBytes := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(
			importEntry("a.b", "c", 0, 0),
			importEntry("a", "b.c", 0, 0),
		)),
	)
	module, err := rt.Compile(moduleBytes)
	if err != nil {
		t.Fatalf("compile exact identities: %v", err)
	}
	defer module.Close()
	imports := NewImports()
	imports.HostFunc("a.b", "c", func() {})
	imports.HostFunc("a", "b.c", func() {})
	instance, err := rt.Instantiate(context.Background(), module, WithImports(imports))
	if err != nil {
		t.Fatalf("instantiate exact identities: %v", err)
	}
	instance.Close()
}

func TestRuntimeDistinctExactOverrideDoesNotSatisfyDeclaration(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	module, err := rt.Compile(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(importEntry("a.b", "c", 0, 0))),
	))
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	if instance, err := rt.Instantiate(context.Background(), module,
		WithImport("a", "b.c", slotHostFunc(func(HostModule, []uint64, []uint64) {})),
	); err == nil || instance != nil || !errors.Is(err, ErrMissingImport) {
		t.Fatalf("distinct exact override = %v, %v; want ErrMissingImport", instance, err)
	}
}

func TestRuntimeRetainsDistinctUndeclaredExactOverrides(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	module, err := rt.Compile(wasmtest.Module())
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	first, second := new(int), new(int)
	instance, err := rt.Instantiate(context.Background(), module,
		WithImport("a.b", "c", first),
		WithImport("a", "b.c", second),
	)
	if err != nil {
		t.Fatalf("distinct undeclared exact overrides: %v", err)
	}
	defer instance.Close()
	got := instance.Imports()
	if value, ok := got.Lookup("a.b", "c"); !ok || value != first {
		t.Fatalf("first exact override = %v, %v", value, ok)
	}
	if value, ok := got.Lookup("a", "b.c"); !ok || value != second {
		t.Fatalf("second exact override = %v, %v", value, ok)
	}
}

func TestRuntimeRetainsUndeclaredExactImport(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	module, err := rt.Compile(wasmtest.Module())
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	marker := new(int)
	instance, err := rt.Instantiate(context.Background(), module, WithImport("unused.mod", "value.name", marker))
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if got := instance.Imports().bindings[importBindingMapKey("unused.mod", "value.name")]; got != marker {
		t.Fatalf("undeclared exact import = %v, want %p", got, marker)
	}
}

func TestRuntimeProvidedImportsSortsExactIdentities(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	rt.importMeta[importBindingMapKey("a", "z")] = &registeredImport{module: "a", name: "z"}
	rt.importMeta[importBindingMapKey("a!", "z")] = &registeredImport{module: "a!", name: "z"}

	imports := rt.ProvidedImports()
	if len(imports) != 2 || imports[0].Module != "a" || imports[0].Name != "z" || imports[1].Module != "a!" || imports[1].Name != "z" {
		t.Fatalf("ProvidedImports order = %#v, want exact identities [(a, z), (a!, z)]", imports)
	}
}

func TestRuntimeReservedExactOverrideIgnoresCollidingIdentity(t *testing.T) {
	const flatKey = "wago_timer.a.b"
	rt := NewRuntime()
	defer rt.Close()
	bindingKey := importBindingMapKey("wago_timer.a", "b")
	rt.imports[bindingKey] = slotHostFunc(func(HostModule, []uint64, []uint64) {})
	rt.importMeta[bindingKey] = &registeredImport{module: "wago_timer.a", name: "b"}
	module, err := rt.Compile(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("wago_timer"), wasmtest.Name("a.b")...), 0x00, 0x00))),
	))
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	instance, err := rt.Instantiate(context.Background(), module, WithImport("wago_timer", "a.b", slotHostFunc(func(HostModule, []uint64, []uint64) {})))
	if err != nil {
		t.Fatalf("exact override of distinct reserved identity: %v", err)
	}
	instance.Close()
}

func TestRuntimeRejectsMixedExactAndFlatOverride(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	module, err := rt.Compile(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(importEntry("env", "f", 0, 0))),
	))
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	first := slotHostFunc(func(HostModule, []uint64, []uint64) {})
	second := slotHostFunc(func(HostModule, []uint64, []uint64) {})
	for _, opts := range [][]InstantiateOption{
		{WithImport("env", "f", first), WithImports(testImports("env.f", second))},
		{WithImports(testImports("env.f", first)), WithImport("env", "f", second)},
	} {
		if instance, err := rt.Instantiate(context.Background(), module, opts...); err == nil || instance != nil || !strings.Contains(err.Error(), "both WithImport and WithImports") {
			t.Fatalf("mixed override = %v, %v; want explicit rejection", instance, err)
		}
	}
}
