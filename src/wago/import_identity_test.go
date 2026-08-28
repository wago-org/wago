package wago

import (
	"context"
	"encoding/binary"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
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
		entry("a", "b.c", 0x00, 0x00), // same legacy key, distinct identity
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
		{module: "a", name: "b.c", kind: ImportFunc},
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

	artifact, err := module.c.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}
	if err := module.Close(); err != nil {
		t.Fatalf("close source module: %v", err)
	}
	var compiled Compiled
	if err := compiled.UnmarshalBinary(artifact); err != nil {
		t.Fatalf("UnmarshalBinary: %v", err)
	}
	loaded, err := rt.Module(&compiled)
	if err != nil {
		t.Fatalf("Runtime.Module: %v", err)
	}
	defer loaded.Close()
	if got := moduleImportIdentities(loaded); !reflect.DeepEqual(got, want) {
		t.Fatalf("artifact import identities = %#v, want %#v", got, want)
	}
}

func TestCompiledCodecRejectsMalformedImportModuleEnd(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig(), exactImportIdentityModule(false))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	defer compiled.Close()
	metadata, err := marshalCompiledMetadata(compiled)
	if err != nil {
		t.Fatalf("marshalCompiledMetadata: %v", err)
	}

	r := compiledReader{data: metadata}
	if _, err := r.intSlice(); err != nil {
		t.Fatal(err)
	}
	if _, err := r.intSlice(); err != nil {
		t.Fatal(err)
	}
	if _, err := r.uvar(); err != nil {
		t.Fatal(err)
	}
	if _, err := r.stringSlice(); err != nil {
		t.Fatal(err)
	}
	moduleEndOffset := len(metadata) - len(r.data)
	metadata[moduleEndOffset]++ // Move the one-byte boundary from '.' onto 'c'.

	var decoded Compiled
	if err := unmarshalCompiledMetadata(&decoded, metadata); err == nil || !strings.Contains(err.Error(), "does not identify the import-key separator") {
		t.Fatalf("malformed import module-name end error = %v", err)
	}
}

func TestRuntimePluginBindingRequiresExactImportIdentity(t *testing.T) {
	const flatKey = "a.b.c"
	rt := NewRuntime()
	defer rt.Close()
	rt.imports[flatKey] = HostFunc(func(HostModule, []uint64, []uint64) {})
	rt.importMeta[flatKey] = &registeredImport{
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

	if instance, err := rt.Instantiate(context.Background(), module, WithImports(Imports{flatKey: HostFunc(func(HostModule, []uint64, []uint64) {})})); err == nil || instance != nil || !strings.Contains(err.Error(), "use WithImport") {
		t.Fatalf("ambiguous flat override = %v, %v; want exact-import error", instance, err)
	}

	instance, err := rt.Instantiate(context.Background(), module, WithImport("a.b", "c", HostFunc(func(HostModule, []uint64, []uint64) {})))
	if err != nil {
		t.Fatalf("exact override: %v", err)
	}
	if err := instance.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeRejectsCollidingExactImportIdentities(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	module, err := rt.Compile(exactImportIdentityModule(false))
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	if instance, err := rt.Instantiate(context.Background(), module,
		WithImport("a.b", "c", HostFunc(func(HostModule, []uint64, []uint64) {})),
		WithImport("a", "b.c", HostFunc(func(HostModule, []uint64, []uint64) {})),
	); err == nil || instance != nil || !strings.Contains(err.Error(), "cannot be bound safely") {
		t.Fatalf("colliding exact imports = %v, %v; want rejection", instance, err)
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
	if got := instance.Imports()["unused.mod.value.name"]; got != marker {
		t.Fatalf("undeclared exact import = %v, want %p", got, marker)
	}
}

func TestRuntimeReservedExactOverrideIgnoresCollidingIdentity(t *testing.T) {
	const flatKey = "wago_timer.a.b"
	rt := NewRuntime()
	defer rt.Close()
	rt.imports[flatKey] = HostFunc(func(HostModule, []uint64, []uint64) {})
	rt.importMeta[flatKey] = &registeredImport{module: "wago_timer.a", name: "b"}
	module, err := rt.Compile(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("wago_timer"), wasmtest.Name("a.b")...), 0x00, 0x00))),
	))
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	instance, err := rt.Instantiate(context.Background(), module, WithImport("wago_timer", "a.b", HostFunc(func(HostModule, []uint64, []uint64) {})))
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
	first := HostFunc(func(HostModule, []uint64, []uint64) {})
	second := HostFunc(func(HostModule, []uint64, []uint64) {})
	for _, opts := range [][]InstantiateOption{
		{WithImport("env", "f", first), WithImports(Imports{"env.f": second})},
		{WithImports(Imports{"env.f": first}), WithImport("env", "f", second)},
	} {
		if instance, err := rt.Instantiate(context.Background(), module, opts...); err == nil || instance != nil || !strings.Contains(err.Error(), "both WithImport and WithImports") {
			t.Fatalf("mixed override = %v, %v; want explicit rejection", instance, err)
		}
	}
}

func TestCompiledCodecBoundsDecodedImportDirectoryAllocation(t *testing.T) {
	const count = 3
	var prefix [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(prefix[:], uint64(count))
	encoded := make([]byte, n+2*count)
	copy(encoded, prefix[:n])
	r := compiledReader{data: encoded}
	if _, _, err := r.importDirectoryWithAllocationLimit(count, 1); err == nil || !strings.Contains(err.Error(), "decoded directory allocation limit") {
		t.Fatalf("oversized import directory error = %v", err)
	}
}

func TestCompiledCodecRejectsImportCountMismatchBeforeAllocation(t *testing.T) {
	const encodedCount = 3
	var prefix [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(prefix[:], encodedCount)
	encoded := make([]byte, n+2*encodedCount)
	copy(encoded, prefix[:n])
	r := compiledReader{data: encoded}
	if _, _, err := r.importDirectoryWithAllocationLimit(0, maxImportDirectoryAllocationBytes); err == nil || !strings.Contains(err.Error(), "directory count 3 != NumImports 0") {
		t.Fatalf("mismatched import count error = %v", err)
	}
}
