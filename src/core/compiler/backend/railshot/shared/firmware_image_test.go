package shared

import (
	"encoding/binary"
	"errors"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/embedded32"
)

func testEmbeddedFirmwareModule() *EmbeddedModule {
	start := 8
	return &EmbeddedModule{
		Code:            []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12},
		Entry:           []int{0},
		Functions:       []EmbeddedFunctionMetadata{{FuncIndex: 0, Offset: 0, Size: 4, CallOffset: 4, HasCallEntry: true}},
		FunctionTypeIDs: []uint32{17},
		Memory:          &EmbeddedMemory{Minimum: 1, Maximum: 2, HasMaximum: true},
		Data: []EmbeddedDataSegment{
			{Offset: 2, Bytes: []byte{0xaa, 0xbb}},
			{Passive: true, Bytes: []byte{0xcc, 0xdd, 0xee}},
		},
		Globals: []EmbeddedGlobal{{Type: wasm.I64, Slot: 0, Words: [4]uint32{0x55667788, 0x11223344}}},
		Table: &EmbeddedTable{
			Minimum: 2, Maximum: 4, HasMaximum: true,
			Elements: []EmbeddedElementSegment{
				{Mode: EmbeddedElementActive, Offset: 1, Values: []uint32{1}},
				{Mode: EmbeddedElementPassive, Values: []uint32{1, 0}},
			},
		},
		Exports:    []EmbeddedExport{{Name: "run", Kind: wasm.ExternFunc, Index: 0}},
		StartEntry: &start,
	}
}

func TestBuildEmbeddedFirmwareImageSerializesClosedModuleState(t *testing.T) {
	module := testEmbeddedFirmwareModule()
	opts := EmbeddedFirmwareOptions{
		BaseAddress:         0x20000000,
		MemoryCapacity:      2 * embedded32.WasmPageSize,
		TableCapacity:       4,
		NativeStackLimit:    0x20040000,
		FunctionAddressMask: 1,
		HelperEntries:       [4]uint32{0x1001, 0x2001, 0x3001, 0x4001},
	}
	layout, err := embeddedFirmwarePlan(module, opts)
	if err != nil {
		t.Fatal(err)
	}
	size, err := EmbeddedFirmwareImageSize(module, opts)
	if err != nil || size != layout.required {
		t.Fatalf("size=%d layout=%d err=%v", size, layout.required, err)
	}
	dst := make([]byte, size)
	image, err := BuildEmbeddedFirmwareImage(dst, module, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(image.Bytes[:len(module.Code)], module.Code) {
		t.Fatalf("code=%x", image.Bytes[:len(module.Code)])
	}
	word := func(offset uint32) uint32 { return binary.LittleEndian.Uint32(image.Bytes[offset : offset+4]) }
	address := func(offset uint32) uint32 { return opts.BaseAddress + offset }
	if image.ContextAddress != address(layout.context) || image.StartAddress != address(layout.code+8)|1 || len(image.Exports) != 1 || image.Exports[0].CallAddress != address(layout.code+4)|1 {
		t.Fatalf("image metadata=%+v", image)
	}
	if word(layout.context+embedded32.ContextLinearMemoryBaseOffset) != image.MemoryAddress ||
		word(layout.context+embedded32.ContextLinearMemoryLengthOffset) != embedded32.WasmPageSize ||
		word(layout.context+embedded32.ContextLinearMemoryMaximumOffset) != 2*embedded32.WasmPageSize ||
		word(layout.context+embedded32.ContextGlobalsBaseOffset) != image.GlobalsAddress ||
		word(layout.context+embedded32.ContextTableOffset) != image.TableAddress ||
		word(layout.context+embedded32.ContextStackLimitOffset) != opts.NativeStackLimit {
		t.Fatalf("context=%x", image.Bytes[layout.context:layout.context+embedded32.ContextABISize])
	}
	for i, helper := range opts.HelperEntries {
		if got := word(layout.helpers + uint32(i*4)); got != helper {
			t.Fatalf("helper %d=%#x want %#x", i, got, helper)
		}
	}
	if got := image.Bytes[layout.memory+2 : layout.memory+4]; !slices.Equal(got, []byte{0xaa, 0xbb}) {
		t.Fatalf("active data=%x", got)
	}
	if got := []uint32{word(layout.globals), word(layout.globals + 4)}; !slices.Equal(got, []uint32{0x55667788, 0x11223344}) {
		t.Fatalf("globals=%#v", got)
	}
	if got := []uint32{word(layout.tableEntries), word(layout.tableEntries + 4), word(layout.tableEntries + 8), word(layout.tableEntries + 12)}; !slices.Equal(got, []uint32{0, 1, 0, 0}) {
		t.Fatalf("table entries=%v", got)
	}
	if word(layout.functionEntries) != image.CodeAddress|1 || word(layout.functionTypes) != 17 {
		t.Fatalf("function arrays entry=%#x type=%d", word(layout.functionEntries), word(layout.functionTypes))
	}
	activeData := layout.dataDescriptors
	passiveData := activeData + embedded32.DataSegmentABIBytes
	if word(activeData+embedded32.DataSegmentDroppedOffset) != 1 || word(passiveData+embedded32.DataSegmentDroppedOffset) != 0 || word(passiveData+embedded32.DataSegmentLengthOffset) != 3 {
		t.Fatalf("data descriptors=%x", image.Bytes[activeData:passiveData+embedded32.DataSegmentABIBytes])
	}
	activeElem := layout.elementDescriptors
	passiveElem := activeElem + embedded32.DataSegmentABIBytes
	if word(activeElem+embedded32.DataSegmentDroppedOffset) != 1 || word(passiveElem+embedded32.DataSegmentDroppedOffset) != 0 || word(passiveElem+embedded32.DataSegmentLengthOffset) != 2 {
		t.Fatalf("element descriptors=%x", image.Bytes[activeElem:passiveElem+embedded32.DataSegmentABIBytes])
	}
}

func TestBuildEmbeddedFirmwareImagePreflightsBeforeMutation(t *testing.T) {
	module := testEmbeddedFirmwareModule()
	opts := EmbeddedFirmwareOptions{
		BaseAddress:      0x20000000,
		MemoryCapacity:   2 * embedded32.WasmPageSize,
		TableCapacity:    4,
		NativeStackLimit: 0x20040000,
		HelperEntries:    [4]uint32{1, 2, 3, 4},
	}
	size, err := EmbeddedFirmwareImageSize(module, opts)
	if err != nil {
		t.Fatal(err)
	}
	dst := make([]byte, size-1)
	for i := range dst {
		dst[i] = 0x5a
	}
	if _, err := BuildEmbeddedFirmwareImage(dst, module, opts); !errors.Is(err, embedded32.ErrArenaCapacity) {
		t.Fatalf("capacity error=%v", err)
	}
	for i, value := range dst {
		if value != 0x5a {
			t.Fatalf("destination mutated at %d", i)
		}
	}
	module.Data[0].Offset = embedded32.WasmPageSize
	if _, err := EmbeddedFirmwareImageSize(module, opts); err == nil {
		t.Fatal("out-of-range active data accepted")
	}
}

func TestEmbeddedFirmwareImageRejectsOpenModules(t *testing.T) {
	module := testEmbeddedFirmwareModule()
	module.ImportedGlobals = []EmbeddedGlobal{{Type: wasm.I32}}
	_, err := EmbeddedFirmwareImageSize(module, EmbeddedFirmwareOptions{
		BaseAddress:      0x20000000,
		MemoryCapacity:   2 * embedded32.WasmPageSize,
		TableCapacity:    4,
		NativeStackLimit: 1,
		HelperEntries:    [4]uint32{1, 2, 3, 4},
	})
	if err == nil {
		t.Fatal("open module accepted")
	}
}
