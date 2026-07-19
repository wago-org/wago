package shared

import (
	"encoding/binary"
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/embedded32"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func embeddedFirmwareLinkProvider(t *testing.T) *EmbeddedModule {
	t.Helper()
	m := compileEmbeddedLinkTestModule(t, wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I64, false, []byte{0x42, 40, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("f", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("g", byte(wasm.ExternGlobal), 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x41, 1, 0x6a, 0x0b}))),
	))
	m.Functions[0].CallOffset = m.Functions[0].Offset
	m.Functions[0].HasCallEntry = true
	return m
}

func embeddedFirmwareLinkConsumer(t *testing.T) *EmbeddedModule {
	t.Helper()
	functionImport := append(wasmtest.Name("provider"), wasmtest.Name("f")...)
	functionImport = append(functionImport, 0, 0)
	m := compileEmbeddedLinkTestModule(t, wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(
			functionImport,
			wasmtest.GlobalImportEntry("provider", "g", wasm.I64, false),
		)),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I64, false, []byte{0x23, 0, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x10, 0, 0x0b}))),
	))
	m.Functions[0].CallOffset = m.Functions[0].Offset
	m.Functions[0].HasCallEntry = true
	return m
}

func linkedFirmwareTestOptions(count int) EmbeddedLinkedFirmwareOptions {
	modules := make([]EmbeddedFirmwareOptions, count)
	for i := range modules {
		modules[i] = EmbeddedFirmwareOptions{
			NativeStackLimit:    0x20080000,
			FunctionAddressMask: 1,
			HelperEntries:       [4]uint32{0x1001, 0x2001, 0x3001, 0x4001},
		}
	}
	return EmbeddedLinkedFirmwareOptions{BaseAddress: 0x20000000, Modules: modules}
}

func TestBuildEmbeddedLinkedFirmwareImagePublishesFunctionContextsAndGlobalAliases(t *testing.T) {
	provider := embeddedFirmwareLinkProvider(t)
	consumer := embeddedFirmwareLinkConsumer(t)
	plan, err := ResolveEmbeddedLinks([]EmbeddedNamedModule{{Name: "provider", Module: provider}, {Name: "consumer", Module: consumer}})
	if err != nil {
		t.Fatal(err)
	}
	opts := linkedFirmwareTestOptions(2)
	size, err := EmbeddedLinkedFirmwareImageSize(plan, opts)
	if err != nil {
		t.Fatal(err)
	}
	dst := make([]byte, size)
	image, err := BuildEmbeddedLinkedFirmwareImage(dst, plan, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(image.Modules) != 2 || image.Modules[0].Name != "provider" || image.Modules[1].Name != "consumer" {
		t.Fatalf("modules=%+v", image.Modules)
	}
	word := func(address uint32) uint32 {
		offset := address - image.BaseAddress
		return binary.LittleEndian.Uint32(image.Bytes[offset : offset+4])
	}
	providerImage := image.Modules[0].Image
	consumerImage := image.Modules[1].Image
	imports := word(consumerImage.ContextAddress + embedded32.ContextImportsBaseOffset)
	if imports == 0 {
		t.Fatal("import descriptors not published")
	}
	wantEntry := providerImage.CodeAddress | 1
	if got := word(imports + embedded32.ImportFunctionEntryOffset); got != wantEntry {
		t.Fatalf("import entry=%#x want %#x", got, wantEntry)
	}
	if got := word(imports + embedded32.ImportFunctionContextOffset); got != providerImage.ContextAddress {
		t.Fatalf("import context=%#x want %#x", got, providerImage.ContextAddress)
	}
	directory := word(consumerImage.ContextAddress + embedded32.ContextImportedGlobalsBaseOffset)
	if directory == 0 || word(directory) != providerImage.GlobalsAddress {
		t.Fatalf("global directory=%#x cell=%#x want %#x", directory, word(directory), providerImage.GlobalsAddress)
	}
	if low, high := word(consumerImage.GlobalsAddress), word(consumerImage.GlobalsAddress+4); low != 40 || high != 0 {
		t.Fatalf("consumer initialized global={%#x,%#x}", low, high)
	}
	if len(consumerImage.TransportFunctions) != 1 || consumerImage.TransportFunctions[0].Address != consumerImage.CodeAddress|1 {
		t.Fatalf("consumer transport functions=%+v", consumerImage.TransportFunctions)
	}
}

func TestBuildEmbeddedLinkedFirmwareImagePreflightsCapacity(t *testing.T) {
	provider := embeddedFirmwareLinkProvider(t)
	consumer := embeddedFirmwareLinkConsumer(t)
	plan, err := ResolveEmbeddedLinks([]EmbeddedNamedModule{{Name: "provider", Module: provider}, {Name: "consumer", Module: consumer}})
	if err != nil {
		t.Fatal(err)
	}
	opts := linkedFirmwareTestOptions(2)
	size, err := EmbeddedLinkedFirmwareImageSize(plan, opts)
	if err != nil {
		t.Fatal(err)
	}
	dst := make([]byte, size-1)
	for i := range dst {
		dst[i] = 0x5a
	}
	if _, err := BuildEmbeddedLinkedFirmwareImage(dst, plan, opts); !errors.Is(err, embedded32.ErrArenaCapacity) {
		t.Fatalf("capacity error=%v", err)
	}
	for i, value := range dst {
		if value != 0x5a {
			t.Fatalf("destination mutated at %d", i)
		}
	}
}

func TestEmbeddedLinkedFirmwareImageRejectsSharedMemoryAndTableImports(t *testing.T) {
	provider := embeddedLinkProvider(t)
	consumer := embeddedLinkConsumer(t)
	plan, err := ResolveEmbeddedLinks([]EmbeddedNamedModule{{Name: "provider", Module: provider}, {Name: "consumer", Module: consumer}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := EmbeddedLinkedFirmwareImageSize(plan, linkedFirmwareTestOptions(2)); err == nil {
		t.Fatal("shared memory/table link accepted")
	}
}
