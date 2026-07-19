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

func embeddedFirmwareMemoryProvider(t *testing.T) *EmbeddedModule {
	t.Helper()
	return compileEmbeddedLinkTestModule(t, wasmtest.Module(
		wasmtest.Section(5, wasmtest.Vec([]byte{1, 1, 2})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("memory", byte(wasm.ExternMem), 0))),
	))
}

func embeddedFirmwareMemoryConsumer(t *testing.T) *EmbeddedModule {
	t.Helper()
	memoryImport := append(wasmtest.Name("provider"), wasmtest.Name("memory")...)
	memoryImport = append(memoryImport, 2, 1, 1, 2)
	m := compileEmbeddedLinkTestModule(t, wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(memoryImport)),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("size", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x3f, 0, 0x0b}))),
	))
	m.Functions[0].CallOffset = m.Functions[0].Offset
	m.Functions[0].HasCallEntry = true
	return m
}

func TestBuildEmbeddedLinkedFirmwareImagePublishesSharedMemoryContext(t *testing.T) {
	provider := embeddedFirmwareMemoryProvider(t)
	consumer := embeddedFirmwareMemoryConsumer(t)
	plan, err := ResolveEmbeddedLinks([]EmbeddedNamedModule{{Name: "provider", Module: provider}, {Name: "consumer", Module: consumer}})
	if err != nil {
		t.Fatal(err)
	}
	opts := linkedFirmwareTestOptions(2)
	opts.Modules[0].MemoryCapacity = 2 * embedded32.WasmPageSize
	size, err := EmbeddedLinkedFirmwareImageSize(plan, opts)
	if err != nil {
		t.Fatal(err)
	}
	image, err := BuildEmbeddedLinkedFirmwareImage(make([]byte, size), plan, opts)
	if err != nil {
		t.Fatal(err)
	}
	providerImage := image.Modules[0].Image
	consumerImage := image.Modules[1].Image
	word := func(address uint32) uint32 {
		offset := address - image.BaseAddress
		return binary.LittleEndian.Uint32(image.Bytes[offset : offset+4])
	}
	if got := word(consumerImage.ContextAddress + embedded32.ContextLinearMemoryContextOffset); got != providerImage.ContextAddress {
		t.Fatalf("memory context=%#x want %#x", got, providerImage.ContextAddress)
	}
	if got := word(providerImage.ContextAddress + embedded32.ContextLinearMemoryLengthOffset); got != embedded32.WasmPageSize {
		t.Fatalf("provider memory length=%d", got)
	}
	if consumerImage.MemoryAddress != 0 || consumerImage.MemoryCapacity != 0 {
		t.Fatalf("consumer allocated imported memory: %+v", consumerImage)
	}
}

func TestEmbeddedLinkedFirmwareImagePublishesImportedFunctionReexports(t *testing.T) {
	provider := embeddedFirmwareLinkProvider(t)
	functionImport := append(wasmtest.Name("provider"), wasmtest.Name("f")...)
	functionImport = append(functionImport, byte(wasm.ExternFunc), 0)
	reexport := compileEmbeddedLinkTestModule(t, wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(functionImport)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("forward", byte(wasm.ExternFunc), 0))),
	))
	plan, err := ResolveEmbeddedLinks([]EmbeddedNamedModule{{Name: "provider", Module: provider}, {Name: "reexport", Module: reexport}})
	if err != nil {
		t.Fatal(err)
	}
	opts := linkedFirmwareTestOptions(2)
	size, err := EmbeddedLinkedFirmwareImageSize(plan, opts)
	if err != nil {
		t.Fatal(err)
	}
	image, err := BuildEmbeddedLinkedFirmwareImage(make([]byte, size), plan, opts)
	if err != nil {
		t.Fatal(err)
	}
	providerImage := image.Modules[0].Image
	reexportImage := image.Modules[1].Image
	if len(reexportImage.Exports) != 1 || len(reexportImage.TransportFunctions) != 1 {
		t.Fatalf("reexport metadata=%+v transport=%+v", reexportImage.Exports, reexportImage.TransportFunctions)
	}
	got := reexportImage.TransportFunctions[0]
	want := providerImage.TransportFunctions[0]
	if got != want || got.Context != providerImage.ContextAddress {
		t.Fatalf("reexport=%+v provider=%+v", got, want)
	}
}

func TestEmbeddedLinkedFirmwareImagePublishesCrossModuleFuncrefs(t *testing.T) {
	provider := compileEmbeddedLinkTestModule(t, wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 1, 1, 2})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("f", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("table", byte(wasm.ExternTable), 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}))),
	))
	provider.Functions[0].CallOffset = provider.Functions[0].Offset
	provider.Functions[0].HasCallEntry = true
	functionImport := append(wasmtest.Name("provider"), wasmtest.Name("f")...)
	functionImport = append(functionImport, byte(wasm.ExternFunc), 0)
	tableImport := append(wasmtest.Name("provider"), wasmtest.Name("table")...)
	tableImport = append(tableImport, byte(wasm.ExternTable), 0x70, 1, 1, 2)
	consumer := compileEmbeddedLinkTestModule(t, wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(functionImport, tableImport)),
		wasmtest.Section(9, wasmtest.Vec([]byte{0, 0x41, 0, 0x0b, 1, 0})),
	))
	plan, err := ResolveEmbeddedLinks([]EmbeddedNamedModule{{Name: "provider", Module: provider}, {Name: "consumer", Module: consumer}})
	if err != nil {
		t.Fatal(err)
	}
	opts := linkedFirmwareTestOptions(2)
	opts.Modules[0].TableCapacity = 2
	opts.Modules[1].TableCapacity = 2
	size, err := EmbeddedLinkedFirmwareImageSize(plan, opts)
	if err != nil {
		t.Fatal(err)
	}
	image, err := BuildEmbeddedLinkedFirmwareImage(make([]byte, size), plan, opts)
	if err != nil {
		t.Fatal(err)
	}
	word := func(address uint32) uint32 {
		offset := address - image.BaseAddress
		return binary.LittleEndian.Uint32(image.Bytes[offset : offset+4])
	}
	providerImage := image.Modules[0].Image
	consumerImage := image.Modules[1].Image
	table := providerImage.TableAddresses[0]
	entries := word(table + embedded32.TableABIEntriesBaseOffset)
	if got := word(entries); got != 1 {
		t.Fatalf("linked table funcref=%d", got)
	}
	contexts := word(table + embedded32.TableABIFunctionContextsBaseOffset)
	if got := word(contexts); got != providerImage.ContextAddress {
		t.Fatalf("linked function context=%#x provider=%#x", got, providerImage.ContextAddress)
	}
	refs := word(consumerImage.ContextAddress + embedded32.ContextFunctionRefsBaseOffset)
	if got := word(refs); got != 1 {
		t.Fatalf("consumer imported ref.func identity=%d", got)
	}
	elements := word(consumerImage.ContextAddress + embedded32.ContextElementSegmentsBaseOffset)
	if got := word(elements + embedded32.DataSegmentDroppedOffset); got != 1 {
		t.Fatalf("consumer active element dropped=%d", got)
	}
}

func TestBuildEmbeddedLinkedFirmwareImageAppliesActiveImportedData(t *testing.T) {
	provider := embeddedFirmwareMemoryProvider(t)
	memoryImport := append(wasmtest.Name("provider"), wasmtest.Name("memory")...)
	memoryImport = append(memoryImport, 2, 1, 1, 2)
	consumer := compileEmbeddedLinkTestModule(t, wasmtest.Module(
		wasmtest.Section(2, wasmtest.Vec(memoryImport)),
		wasmtest.Section(11, wasmtest.Vec([]byte{0, 0x41, 1, 0x0b, 2, 0xaa, 0xbb})),
	))
	plan, err := ResolveEmbeddedLinks([]EmbeddedNamedModule{{Name: "provider", Module: provider}, {Name: "consumer", Module: consumer}})
	if err != nil {
		t.Fatal(err)
	}
	opts := linkedFirmwareTestOptions(2)
	opts.Modules[0].MemoryCapacity = 2 * embedded32.WasmPageSize
	size, err := EmbeddedLinkedFirmwareImageSize(plan, opts)
	if err != nil {
		t.Fatal(err)
	}
	image, err := BuildEmbeddedLinkedFirmwareImage(make([]byte, size), plan, opts)
	if err != nil {
		t.Fatal(err)
	}
	providerImage := image.Modules[0].Image
	consumerImage := image.Modules[1].Image
	memoryOffset := providerImage.MemoryAddress - image.BaseAddress
	if got := image.Bytes[memoryOffset+1 : memoryOffset+3]; got[0] != 0xaa || got[1] != 0xbb {
		t.Fatalf("active data=%x", got)
	}
	descriptorAddress := binary.LittleEndian.Uint32(image.Bytes[consumerImage.ContextAddress-image.BaseAddress+embedded32.ContextDataSegmentsBaseOffset:])
	dropped := binary.LittleEndian.Uint32(image.Bytes[descriptorAddress-image.BaseAddress+embedded32.DataSegmentDroppedOffset:])
	if dropped != 1 {
		t.Fatalf("active descriptor dropped=%d", dropped)
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

func TestEmbeddedLinkedFirmwareImagePublishesImportedTableAliases(t *testing.T) {
	provider := compileEmbeddedLinkTestModule(t, wasmtest.Module(
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 1, 2, 4})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("table", byte(wasm.ExternTable), 0))),
	))
	tableImport := append(wasmtest.Name("provider"), wasmtest.Name("table")...)
	tableImport = append(tableImport, byte(wasm.ExternTable), 0x70, 1, 1, 4)
	consumer := compileEmbeddedLinkTestModule(t, wasmtest.Module(
		wasmtest.Section(2, wasmtest.Vec(tableImport)),
	))
	plan, err := ResolveEmbeddedLinks([]EmbeddedNamedModule{{Name: "provider", Module: provider}, {Name: "consumer", Module: consumer}})
	if err != nil {
		t.Fatal(err)
	}
	opts := linkedFirmwareTestOptions(2)
	opts.Modules[0].TableCapacity = 4
	opts.Modules[1].TableCapacity = 4
	size, err := EmbeddedLinkedFirmwareImageSize(plan, opts)
	if err != nil {
		t.Fatal(err)
	}
	image, err := BuildEmbeddedLinkedFirmwareImage(make([]byte, size), plan, opts)
	if err != nil {
		t.Fatal(err)
	}
	word := func(address uint32) uint32 {
		offset := address - image.BaseAddress
		return binary.LittleEndian.Uint32(image.Bytes[offset : offset+4])
	}
	providerTable := image.Modules[0].Image.TableAddresses[0]
	consumerImage := image.Modules[1].Image
	consumerContext := consumerImage.ContextAddress
	if got := word(consumerImage.TableAddresses[0] + embedded32.TableABIMaximumOffset); got != 0 {
		t.Fatalf("duplicate imported-table capacity=%d", got)
	}
	directory := word(consumerContext + embedded32.ContextTablesBaseOffset)
	if got := word(directory); got != providerTable {
		t.Fatalf("consumer table=%#x provider=%#x", got, providerTable)
	}
	if got := word(consumerContext + embedded32.ContextTableStorageOffset); got != providerTable {
		t.Fatalf("consumer table-0 storage=%#x provider=%#x", got, providerTable)
	}
}
