package shared

import (
	"encoding/binary"
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/embedded32"
)

const wasmPageBytes = uint64(65536)

type EmbeddedFirmwareOptions struct {
	BaseAddress         uint32
	MemoryCapacity      uint32
	TableCapacity       uint32
	NativeStackLimit    uint32
	FunctionAddressMask uint32
	HelperEntries       [4]uint32
}

type EmbeddedFirmwareExport struct {
	Name        string
	Kind        wasm.ExternKind
	Index       uint32
	CallAddress uint32
}

type EmbeddedFirmwareImage struct {
	Bytes               []byte
	BaseAddress         uint32
	CodeAddress         uint32
	ContextAddress      uint32
	TrapAddress         uint32
	CancellationAddress uint32
	MemoryAddress       uint32
	MemoryLength        uint32
	MemoryCapacity      uint32
	GlobalsAddress      uint32
	TableAddress        uint32
	StartAddress        uint32
	Exports             []EmbeddedFirmwareExport
}

type embeddedFirmwareLayout struct {
	required           uint32
	code               uint32
	context            uint32
	trap               uint32
	cancel             uint32
	helpers            uint32
	globals            uint32
	globalSlots        uint32
	dataDescriptors    uint32
	dataBytes          []uint32
	table              uint32
	tableEntries       uint32
	functionEntries    uint32
	functionTypes      uint32
	elementDescriptors uint32
	elementValues      []uint32
	memory             uint32
	memoryLength       uint32
	memoryCapacity     uint32
	tableCapacity      uint32
}

type embeddedFirmwareAllocator struct {
	offset uint64
}

func (a *embeddedFirmwareAllocator) reserve(size, align uint64) (uint32, error) {
	if align == 0 || align&(align-1) != 0 {
		return 0, embedded32.ErrInvalidArena
	}
	start := (a.offset + align - 1) &^ (align - 1)
	end := start + size
	if end < start || end > uint64(^uint32(0)) {
		return 0, embedded32.ErrArenaCapacity
	}
	a.offset = end
	return uint32(start), nil
}

func embeddedFirmwarePlan(module *EmbeddedModule, opts EmbeddedFirmwareOptions) (*embeddedFirmwareLayout, error) {
	if module == nil {
		return nil, embedded32.ErrInvalidArena
	}
	if module.ImportedFunctions != 0 || module.MemoryImported || len(module.ImportedGlobals) != 0 || module.Table != nil && module.Table.Imported {
		return nil, fmt.Errorf("embedded32: firmware image requires a closed module")
	}
	if opts.FunctionAddressMask > 1 {
		return nil, fmt.Errorf("embedded32: invalid function address mask %#x", opts.FunctionAddressMask)
	}
	if opts.NativeStackLimit == 0 {
		return nil, fmt.Errorf("embedded32: firmware image requires a native stack limit")
	}
	for i, entry := range opts.HelperEntries {
		if entry == 0 {
			return nil, fmt.Errorf("embedded32: helper entry %d is unavailable", i)
		}
	}
	layout := &embeddedFirmwareLayout{}
	var a embeddedFirmwareAllocator
	var err error
	if layout.code, err = a.reserve(uint64(len(module.Code)), 16); err != nil {
		return nil, err
	}
	if layout.context, err = a.reserve(embedded32.ContextABISize, 4); err != nil {
		return nil, err
	}
	if layout.trap, err = a.reserve(4, 4); err != nil {
		return nil, err
	}
	if layout.cancel, err = a.reserve(4, 4); err != nil {
		return nil, err
	}
	if layout.helpers, err = a.reserve(embedded32.HelperTableBytes, 4); err != nil {
		return nil, err
	}
	for i := range module.Globals {
		global := &module.Globals[i]
		width, ok := MixedValueSlots(global.Type)
		if !ok || global.Slot > ^uint32(0)-uint32(width) {
			return nil, fmt.Errorf("embedded32: global %d has invalid firmware layout", i)
		}
		end := global.Slot + uint32(width)
		if end > layout.globalSlots {
			layout.globalSlots = end
		}
	}
	if layout.globalSlots != 0 {
		if layout.globals, err = a.reserve(uint64(layout.globalSlots)*4, 4); err != nil {
			return nil, err
		}
	}
	if len(module.Data) != 0 {
		if layout.dataDescriptors, err = a.reserve(uint64(len(module.Data))*embedded32.DataSegmentABIBytes, 4); err != nil {
			return nil, err
		}
		layout.dataBytes = make([]uint32, len(module.Data))
		for i := range module.Data {
			if layout.dataBytes[i], err = a.reserve(uint64(len(module.Data[i].Bytes)), 4); err != nil {
				return nil, err
			}
		}
	}
	if module.Table != nil {
		capacity := opts.TableCapacity
		if capacity == 0 {
			capacity = module.Table.Minimum
		}
		if capacity < module.Table.Minimum || module.Table.HasMaximum && capacity > module.Table.Maximum {
			return nil, fmt.Errorf("embedded32: table capacity %d is outside module limits", capacity)
		}
		layout.tableCapacity = capacity
		if layout.table, err = a.reserve(embedded32.TableABIBytes, 4); err != nil {
			return nil, err
		}
		if capacity != 0 {
			if layout.tableEntries, err = a.reserve(uint64(capacity)*4, 4); err != nil {
				return nil, err
			}
		}
		functionCount := len(module.FunctionTypeIDs)
		if functionCount != 0 {
			if layout.functionEntries, err = a.reserve(uint64(functionCount)*4, 4); err != nil {
				return nil, err
			}
			if layout.functionTypes, err = a.reserve(uint64(functionCount)*4, 4); err != nil {
				return nil, err
			}
		}
		if len(module.Table.Elements) != 0 {
			if layout.elementDescriptors, err = a.reserve(uint64(len(module.Table.Elements))*embedded32.DataSegmentABIBytes, 4); err != nil {
				return nil, err
			}
			layout.elementValues = make([]uint32, len(module.Table.Elements))
			for i := range module.Table.Elements {
				if layout.elementValues[i], err = a.reserve(uint64(len(module.Table.Elements[i].Values))*4, 4); err != nil {
					return nil, err
				}
			}
		}
	}
	if module.Memory != nil {
		initial := uint64(module.Memory.Minimum) * wasmPageBytes
		if initial > uint64(^uint32(0)) {
			return nil, fmt.Errorf("embedded32: initial memory exceeds the 32-bit firmware image")
		}
		capacity := uint64(opts.MemoryCapacity)
		if capacity == 0 {
			capacity = initial
		}
		if capacity%wasmPageBytes != 0 {
			return nil, fmt.Errorf("embedded32: memory capacity %d is not page aligned", capacity)
		}
		if capacity < initial {
			return nil, fmt.Errorf("embedded32: memory capacity %d is below initial length %d", capacity, initial)
		}
		if module.Memory.HasMaximum && capacity > uint64(module.Memory.Maximum)*wasmPageBytes {
			return nil, fmt.Errorf("embedded32: memory capacity %d exceeds module maximum", capacity)
		}
		layout.memoryLength, layout.memoryCapacity = uint32(initial), uint32(capacity)
		if capacity != 0 {
			if layout.memory, err = a.reserve(capacity, 16); err != nil {
				return nil, err
			}
		}
	}
	for i := range module.Globals {
		if module.Globals[i].HasInitGlobal {
			return nil, fmt.Errorf("embedded32: closed firmware global %d has an imported initializer", i)
		}
	}
	for i := range module.Data {
		segment := &module.Data[i]
		if !segment.Passive && (module.Memory == nil || uint64(segment.Offset)+uint64(len(segment.Bytes)) > uint64(layout.memoryLength)) {
			return nil, fmt.Errorf("embedded32: active data segment %d exceeds initial memory", i)
		}
	}
	if module.Table != nil {
		for i := range module.Table.Elements {
			segment := &module.Table.Elements[i]
			if segment.Mode == EmbeddedElementActive && uint64(segment.Offset)+uint64(len(segment.Values)) > uint64(module.Table.Minimum) {
				return nil, fmt.Errorf("embedded32: active element segment %d exceeds initial table", i)
			}
		}
		for i := range module.Functions {
			function := &module.Functions[i]
			if uint64(function.FuncIndex) >= uint64(len(module.FunctionTypeIDs)) {
				return nil, fmt.Errorf("embedded32: function metadata index %d is unavailable", function.FuncIndex)
			}
		}
	}
	for i := range module.Exports {
		export := &module.Exports[i]
		if export.Kind != wasm.ExternFunc {
			continue
		}
		found := false
		for j := range module.Functions {
			function := &module.Functions[j]
			if function.FuncIndex == export.Index && function.HasCallEntry {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("embedded32: exported function %d has no call entry", export.Index)
		}
	}
	if opts.BaseAddress > ^uint32(0)-uint32(a.offset) {
		return nil, fmt.Errorf("embedded32: firmware image exceeds the target address space")
	}
	layout.required = uint32(a.offset)
	return layout, nil
}

func EmbeddedFirmwareImageSize(module *EmbeddedModule, opts EmbeddedFirmwareOptions) (uint32, error) {
	layout, err := embeddedFirmwarePlan(module, opts)
	if err != nil {
		return 0, err
	}
	return layout.required, nil
}

func BuildEmbeddedFirmwareImage(dst []byte, module *EmbeddedModule, opts EmbeddedFirmwareOptions) (*EmbeddedFirmwareImage, error) {
	layout, err := embeddedFirmwarePlan(module, opts)
	if err != nil {
		return nil, err
	}
	if uint64(layout.required) > uint64(len(dst)) {
		return nil, embedded32.ErrArenaCapacity
	}
	var globalCells []uint32
	if layout.globalSlots != 0 {
		globalCells = make([]uint32, layout.globalSlots)
		if err := module.InstantiateGlobals(globalCells); err != nil {
			return nil, err
		}
	}
	var tableEntries []uint32
	if module.Table != nil {
		tableEntries = make([]uint32, layout.tableCapacity)
		if err := module.InstantiateTable(tableEntries); err != nil {
			return nil, err
		}
	}
	imageBytes := dst[:layout.required]
	clear(imageBytes)
	addr := func(offset uint32) uint32 { return opts.BaseAddress + offset }
	put := func(offset, value uint32) { binary.LittleEndian.PutUint32(imageBytes[offset:offset+4], value) }
	copy(imageBytes[layout.code:], module.Code)
	for i, entry := range opts.HelperEntries {
		put(layout.helpers+uint32(i*4), entry)
	}
	if layout.globalSlots != 0 {
		for i, value := range globalCells {
			put(layout.globals+uint32(i*4), value)
		}
	}
	for i := range module.Data {
		segment := &module.Data[i]
		base := layout.dataBytes[i]
		copy(imageBytes[base:], segment.Bytes)
		descriptor := layout.dataDescriptors + uint32(i)*embedded32.DataSegmentABIBytes
		put(descriptor+embedded32.DataSegmentBaseOffset, addr(base))
		put(descriptor+embedded32.DataSegmentLengthOffset, uint32(len(segment.Bytes)))
		if !segment.Passive {
			put(descriptor+embedded32.DataSegmentDroppedOffset, 1)
			copy(imageBytes[layout.memory+segment.Offset:], segment.Bytes)
		}
	}
	if module.Table != nil {
		for i, value := range tableEntries {
			put(layout.tableEntries+uint32(i*4), value)
		}
		for i, value := range module.FunctionTypeIDs {
			put(layout.functionTypes+uint32(i*4), value)
		}
		for i := range module.Functions {
			function := &module.Functions[i]
			put(layout.functionEntries+function.FuncIndex*4, addr(layout.code+function.Offset)|opts.FunctionAddressMask)
		}
		for i := range module.Table.Elements {
			segment := &module.Table.Elements[i]
			values := layout.elementValues[i]
			for j, value := range segment.Values {
				put(values+uint32(j*4), value)
			}
			descriptor := layout.elementDescriptors + uint32(i)*embedded32.DataSegmentABIBytes
			put(descriptor+embedded32.DataSegmentBaseOffset, addr(values))
			put(descriptor+embedded32.DataSegmentLengthOffset, uint32(len(segment.Values)))
			if segment.Mode != EmbeddedElementPassive {
				put(descriptor+embedded32.DataSegmentDroppedOffset, 1)
			}
		}
		var entriesAddress, functionEntriesAddress, functionTypesAddress, elementsAddress uint32
		if layout.tableCapacity != 0 {
			entriesAddress = addr(layout.tableEntries)
		}
		if len(module.FunctionTypeIDs) != 0 {
			functionEntriesAddress = addr(layout.functionEntries)
			functionTypesAddress = addr(layout.functionTypes)
		}
		if len(module.Table.Elements) != 0 {
			elementsAddress = addr(layout.elementDescriptors)
		}
		put(layout.table+embedded32.TableABIEntriesBaseOffset, entriesAddress)
		put(layout.table+embedded32.TableABILengthOffset, module.Table.Minimum)
		put(layout.table+embedded32.TableABIMaximumOffset, layout.tableCapacity)
		put(layout.table+embedded32.TableABIFunctionEntriesBaseOffset, functionEntriesAddress)
		put(layout.table+embedded32.TableABIFunctionTypesBaseOffset, functionTypesAddress)
		put(layout.table+embedded32.TableABIElementSegmentsBaseOffset, elementsAddress)
		put(layout.table+embedded32.TableABIElementSegmentCountOffset, uint32(len(module.Table.Elements)))
	}
	var memoryAddress, globalsAddress, dataAddress, tableAddress uint32
	if module.Memory != nil && layout.memoryCapacity != 0 {
		memoryAddress = addr(layout.memory)
	}
	if layout.globalSlots != 0 {
		globalsAddress = addr(layout.globals)
	}
	if len(module.Data) != 0 {
		dataAddress = addr(layout.dataDescriptors)
	}
	if module.Table != nil {
		tableAddress = addr(layout.table)
	}
	put(layout.context+embedded32.ContextLinearMemoryBaseOffset, memoryAddress)
	put(layout.context+embedded32.ContextLinearMemoryLengthOffset, layout.memoryLength)
	put(layout.context+embedded32.ContextLinearMemoryMaximumOffset, layout.memoryCapacity)
	put(layout.context+embedded32.ContextTrapCellOffset, addr(layout.trap))
	put(layout.context+embedded32.ContextCancelCellOffset, addr(layout.cancel))
	put(layout.context+embedded32.ContextHelperTableOffset, addr(layout.helpers))
	put(layout.context+embedded32.ContextStackLimitOffset, opts.NativeStackLimit)
	put(layout.context+embedded32.ContextGlobalsBaseOffset, globalsAddress)
	put(layout.context+embedded32.ContextDataSegmentsBaseOffset, dataAddress)
	put(layout.context+embedded32.ContextDataSegmentCountOffset, uint32(len(module.Data)))
	put(layout.context+embedded32.ContextTableOffset, tableAddress)

	image := &EmbeddedFirmwareImage{
		Bytes:               imageBytes,
		BaseAddress:         opts.BaseAddress,
		CodeAddress:         addr(layout.code),
		ContextAddress:      addr(layout.context),
		TrapAddress:         addr(layout.trap),
		CancellationAddress: addr(layout.cancel),
		MemoryAddress:       memoryAddress,
		MemoryLength:        layout.memoryLength,
		MemoryCapacity:      layout.memoryCapacity,
		GlobalsAddress:      globalsAddress,
		TableAddress:        tableAddress,
		Exports:             make([]EmbeddedFirmwareExport, len(module.Exports)),
	}
	if module.StartEntry != nil {
		image.StartAddress = addr(layout.code+uint32(*module.StartEntry)) | opts.FunctionAddressMask
	}
	for i := range module.Exports {
		export := module.Exports[i]
		out := EmbeddedFirmwareExport{Name: export.Name, Kind: export.Kind, Index: export.Index}
		if export.Kind == wasm.ExternFunc {
			found := false
			for j := range module.Functions {
				function := &module.Functions[j]
				if function.FuncIndex == export.Index && function.HasCallEntry {
					out.CallAddress = addr(layout.code+function.CallOffset) | opts.FunctionAddressMask
					found = true
					break
				}
			}
			if !found {
				return nil, fmt.Errorf("embedded32: exported function %d has no call entry", export.Index)
			}
		}
		image.Exports[i] = out
	}
	return image, nil
}
