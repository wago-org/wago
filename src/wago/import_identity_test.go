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

func exactImportIdentityModule() []byte {
	entry := func(module, name string, tail ...byte) []byte {
		out := append(wasmtest.Name(module), wasmtest.Name(name)...)
		return append(out, tail...)
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(
			entry("a.b", "c", 0x00, 0x00), // function, type 0
			entry("a", "b.c", 0x00, 0x00), // same legacy key, distinct identity
			entry("", "empty.module", 0x00, 0x00),
			entry("empty.name", "", 0x00, 0x00),
			entry("nul\x00.mod", "field\x00.name", 0x00, 0x00),
			entry("table.mod", "table.名", 0x01, 0x70, 0x00, 0x00),
			entry("memory.mod", "memory.名", 0x02, 0x00, 0x00),
			wasmtest.GlobalImportEntry("global.mod", "global.名", wasm.I32, false),
			entry("tag.mod", "tag.名", 0x04, 0x00, 0x00), // tag attribute 0, type 0
		)),
	)
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
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	rt := NewRuntime(WithRuntimeConfig(cfg))
	defer rt.Close()

	module, err := rt.Compile(exactImportIdentityModule())
	if err != nil {
		t.Fatalf("Runtime.Compile: %v", err)
	}
	want := []importIdentity{
		{module: "a.b", name: "c", kind: ImportFunc},
		{module: "a", name: "b.c", kind: ImportFunc},
		{module: "", name: "empty.module", kind: ImportFunc},
		{module: "empty.name", name: "", kind: ImportFunc},
		{module: "nul\x00.mod", name: "field\x00.name", kind: ImportFunc},
		{module: "global.mod", name: "global.名", kind: ImportGlobal},
		{module: "memory.mod", name: "memory.名", kind: ImportMemory},
		{module: "table.mod", name: "table.名", kind: ImportTable},
		{module: "tag.mod", name: "tag.名", kind: ImportTag},
	}
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
		importIdentity{module: metadata.Tags[0].ImportModule, name: metadata.Tags[0].ImportName, kind: ImportTag},
	)
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
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), exactImportIdentityModule())
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

	// Explicit flat Imports remain a deliberate compatibility boundary: callers
	// supplying one directly chose the legacy namespace themselves.
	instance, err := rt.Instantiate(context.Background(), module, WithImports(Imports{flatKey: HostFunc(func(HostModule, []uint64, []uint64) {})}))
	if err != nil {
		t.Fatalf("explicit legacy override: %v", err)
	}
	if err := instance.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCompiledCodecBoundsDecodedImportDirectoryAllocation(t *testing.T) {
	const count = 3
	var prefix [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(prefix[:], uint64(count))
	encoded := make([]byte, n+2*count)
	copy(encoded, prefix[:n])
	r := compiledReader{data: encoded}
	if _, _, err := r.importDirectoryWithAllocationLimit(1); err == nil || !strings.Contains(err.Error(), "decoded directory allocation limit") {
		t.Fatalf("oversized import directory error = %v", err)
	}
}
