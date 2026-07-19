package shared

import (
	"encoding/binary"
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/embedded32"
)

type EmbeddedLinkedFirmwareOptions struct {
	BaseAddress uint32
	Modules     []EmbeddedFirmwareOptions
}

type EmbeddedLinkedFirmwareModule struct {
	Name  string
	Image *EmbeddedFirmwareImage
}

type EmbeddedLinkedFirmwareImage struct {
	Bytes       []byte
	BaseAddress uint32
	Modules     []EmbeddedLinkedFirmwareModule
}

type embeddedLinkedModuleLayout struct {
	clone         *EmbeddedModule
	options       EmbeddedFirmwareOptions
	offset        uint32
	imageBytes    uint32
	importsOffset uint32
	globalsOffset uint32
	regionBytes   uint32
}

type embeddedLinkedFirmwareLayout struct {
	required               uint32
	modules                []embeddedLinkedModuleLayout
	binding                map[[2]int]EmbeddedLinkBinding
	functionBase           []uint32
	functionCount          uint32
	functionEntriesOffset  uint32
	functionTypesOffset    uint32
	functionContextsOffset uint32
}

// EmbeddedLinkedFirmwareImageSize preflights a resolved function/global link
// graph. Shared imported memories/tables remain rejected until their mutable
// length/element descriptors are separated from per-module context state.
func EmbeddedLinkedFirmwareImageSize(plan *EmbeddedLinkPlan, opts EmbeddedLinkedFirmwareOptions) (uint32, error) {
	layout, err := embeddedLinkedFirmwarePlan(plan, opts)
	if err != nil {
		return 0, err
	}
	return layout.required, nil
}

func BuildEmbeddedLinkedFirmwareImage(dst []byte, plan *EmbeddedLinkPlan, opts EmbeddedLinkedFirmwareOptions) (*EmbeddedLinkedFirmwareImage, error) {
	layout, err := embeddedLinkedFirmwarePlan(plan, opts)
	if err != nil {
		return nil, err
	}
	if uint64(layout.required) > uint64(len(dst)) {
		return nil, embedded32.ErrArenaCapacity
	}
	out := &EmbeddedLinkedFirmwareImage{
		Bytes:       dst[:layout.required],
		BaseAddress: opts.BaseAddress,
		Modules:     make([]EmbeddedLinkedFirmwareModule, len(layout.modules)),
	}
	clear(out.Bytes)
	for i := range layout.modules {
		moduleLayout := &layout.modules[i]
		region := out.Bytes[moduleLayout.offset : moduleLayout.offset+moduleLayout.imageBytes]
		image, err := BuildEmbeddedFirmwareImage(region, moduleLayout.clone, moduleLayout.options)
		if err != nil {
			return nil, err
		}
		out.Modules[i] = EmbeddedLinkedFirmwareModule{Name: plan.Modules[i].Name, Image: image}
	}
	for moduleIndex := range plan.Modules {
		module := plan.Modules[moduleIndex].Module
		image := out.Modules[moduleIndex].Image
		functionExports := 0
		for i := range module.Exports {
			if module.Exports[i].Kind == wasm.ExternFunc {
				functionExports++
			}
		}
		image.Exports = make([]EmbeddedFirmwareExport, len(module.Exports))
		image.TransportFunctions = make([]embedded32.FirmwareTransportFunction, 0, functionExports)
		for exportIndex := range module.Exports {
			export := module.Exports[exportIndex]
			published := EmbeddedFirmwareExport{Name: export.Name, Kind: export.Kind, Index: export.Index}
			if export.Kind == wasm.ExternFunc {
				function, err := resolveLinkedFunctionCall(plan, layout, out, moduleIndex, export.Index, make(map[[2]uint32]bool))
				if err != nil {
					return nil, err
				}
				published.CallAddress = function.Address
				published.ContextAddress = function.Context
				published.ParamSlots = function.ParamSlots
				published.ResultSlots = function.ResultSlots
				image.TransportFunctions = append(image.TransportFunctions, function)
			}
			image.Exports[exportIndex] = published
		}
	}
	readAddress := func(address uint32) (uint32, error) {
		if address < opts.BaseAddress || uint64(address-opts.BaseAddress)+4 > uint64(len(out.Bytes)) {
			return 0, fmt.Errorf("embedded32: linked target address %#x is outside the image", address)
		}
		offset := address - opts.BaseAddress
		return binary.LittleEndian.Uint32(out.Bytes[offset : offset+4]), nil
	}
	putAddress := func(address, value uint32) error {
		if address < opts.BaseAddress || uint64(address-opts.BaseAddress)+4 > uint64(len(out.Bytes)) {
			return fmt.Errorf("embedded32: linked target address %#x is outside the image", address)
		}
		offset := address - opts.BaseAddress
		binary.LittleEndian.PutUint32(out.Bytes[offset:offset+4], value)
		return nil
	}
	functionEntriesAddress := opts.BaseAddress + layout.functionEntriesOffset
	functionTypesAddress := opts.BaseAddress + layout.functionTypesOffset
	functionContextsAddress := opts.BaseAddress + layout.functionContextsOffset
	for moduleIndex := range plan.Modules {
		module := plan.Modules[moduleIndex].Module
		image := out.Modules[moduleIndex].Image
		for localIndex := range module.Functions {
			function := &module.Functions[localIndex]
			id := layout.functionBase[moduleIndex] + uint32(localIndex)
			entry := image.CodeAddress + function.Offset
			entry |= layout.modules[moduleIndex].options.FunctionAddressMask
			if err := putAddress(functionEntriesAddress+id*4, entry); err != nil {
				return nil, err
			}
			if err := putAddress(functionTypesAddress+id*4, module.FunctionTypeIDs[function.FuncIndex]); err != nil {
				return nil, err
			}
			if err := putAddress(functionContextsAddress+id*4, image.ContextAddress); err != nil {
				return nil, err
			}
		}
		refsAddress, err := readAddress(image.ContextAddress + embedded32.ContextFunctionRefsBaseOffset)
		if err != nil {
			return nil, err
		}
		for functionIndex := range module.FunctionTypeIDs {
			id, err := resolveLinkedFunctionID(plan, layout, moduleIndex, uint32(functionIndex), make(map[[2]uint32]bool))
			if err != nil {
				return nil, err
			}
			if err := putAddress(refsAddress+uint32(functionIndex)*4, id+1); err != nil {
				return nil, err
			}
		}
		for _, tableAddress := range image.TableAddresses {
			if err := putAddress(tableAddress+embedded32.TableABIFunctionEntriesBaseOffset, functionEntriesAddress); err != nil {
				return nil, err
			}
			if err := putAddress(tableAddress+embedded32.TableABIFunctionTypesBaseOffset, functionTypesAddress); err != nil {
				return nil, err
			}
			if err := putAddress(tableAddress+embedded32.TableABIFunctionContextsBaseOffset, functionContextsAddress); err != nil {
				return nil, err
			}
		}
		elements := embeddedModuleElements(module)
		descriptors, err := readAddress(image.ContextAddress + embedded32.ContextElementSegmentsBaseOffset)
		if err != nil {
			return nil, err
		}
		for elementIndex := range elements {
			valueType, ok := embeddedReferenceValueType(elements[elementIndex].Reference)
			if !ok || valueType != wasm.FuncRef {
				continue
			}
			base, err := readAddress(descriptors + uint32(elementIndex)*embedded32.DataSegmentABIBytes + embedded32.DataSegmentBaseOffset)
			if err != nil {
				return nil, err
			}
			for valueIndex, value := range elements[elementIndex].Values {
				if value == 0 {
					continue
				}
				id, err := resolveLinkedFunctionID(plan, layout, moduleIndex, value-1, make(map[[2]uint32]bool))
				if err != nil {
					return nil, err
				}
				if err := putAddress(base+uint32(valueIndex)*4, id+1); err != nil {
					return nil, err
				}
			}
		}
		tables := embeddedModuleTables(module)
		for tableIndex := range tables {
			if tables[tableIndex].Imported {
				continue
			}
			valueType, ok := embeddedReferenceValueType(tables[tableIndex].Reference)
			if !ok || valueType != wasm.FuncRef {
				continue
			}
			tableAddress := image.TableAddresses[tableIndex]
			entries, err := readAddress(tableAddress + embedded32.TableABIEntriesBaseOffset)
			if err != nil {
				return nil, err
			}
			length, err := readAddress(tableAddress + embedded32.TableABILengthOffset)
			if err != nil {
				return nil, err
			}
			for entryIndex := uint32(0); entryIndex < length; entryIndex++ {
				value, err := readAddress(entries + entryIndex*4)
				if err != nil {
					return nil, err
				}
				if value == 0 {
					continue
				}
				id, err := resolveLinkedFunctionID(plan, layout, moduleIndex, value-1, make(map[[2]uint32]bool))
				if err != nil {
					return nil, err
				}
				if err := putAddress(entries+entryIndex*4, id+1); err != nil {
					return nil, err
				}
			}
		}
		for globalIndex := range module.Globals {
			global := &module.Globals[globalIndex]
			if global.Type.Kind != wasm.ValRef {
				continue
			}
			valueType, ok := embeddedReferenceValueType(global.Type.Ref)
			if !ok || valueType != wasm.FuncRef {
				continue
			}
			value, err := resolveLinkedFuncRefGlobal(plan, layout, moduleIndex, uint32(len(module.ImportedGlobals)+globalIndex), make(map[[2]uint32]bool))
			if err != nil {
				return nil, err
			}
			if err := putAddress(image.GlobalsAddress+global.Slot*4, value); err != nil {
				return nil, err
			}
		}
	}
	for consumerIndex := range plan.Modules {
		module := plan.Modules[consumerIndex].Module
		moduleLayout := &layout.modules[consumerIndex]
		image := out.Modules[consumerIndex].Image
		if module.ImportedFunctions != 0 {
			importsAddress := opts.BaseAddress + moduleLayout.importsOffset
			if err := putAddress(image.ContextAddress+embedded32.ContextImportsBaseOffset, importsAddress); err != nil {
				return nil, err
			}
			for ordinal := uint32(0); ordinal < module.ImportedFunctions; ordinal++ {
				importIndex, ok := embeddedImportIndex(module, wasm.ExternFunc, ordinal)
				if !ok {
					return nil, fmt.Errorf("embedded32: module %q function import %d is unavailable", plan.Modules[consumerIndex].Name, ordinal)
				}
				entry, context, err := resolveLinkedFunction(plan, layout, out, consumerIndex, importIndex, make(map[[2]int]bool))
				if err != nil {
					return nil, err
				}
				descriptor := importsAddress + ordinal*embedded32.ImportFunctionABIBytes
				if err := putAddress(descriptor+embedded32.ImportFunctionEntryOffset, entry); err != nil {
					return nil, err
				}
				if err := putAddress(descriptor+embedded32.ImportFunctionContextOffset, context); err != nil {
					return nil, err
				}
			}
		}
		if module.MemoryImported {
			importIndex, ok := embeddedImportIndex(module, wasm.ExternMem, 0)
			if !ok {
				return nil, fmt.Errorf("embedded32: module %q memory import is unavailable", plan.Modules[consumerIndex].Name)
			}
			memoryContext, err := resolveLinkedMemoryContext(plan, layout, out, consumerIndex, importIndex, make(map[[2]int]bool))
			if err != nil {
				return nil, err
			}
			if err := putAddress(image.ContextAddress+embedded32.ContextLinearMemoryContextOffset, memoryContext); err != nil {
				return nil, err
			}
			memoryBase, err := readAddress(memoryContext + embedded32.ContextLinearMemoryBaseOffset)
			if err != nil {
				return nil, err
			}
			memoryLength, err := readAddress(memoryContext + embedded32.ContextLinearMemoryLengthOffset)
			if err != nil {
				return nil, err
			}
			descriptors, err := readAddress(image.ContextAddress + embedded32.ContextDataSegmentsBaseOffset)
			if err != nil {
				return nil, err
			}
			for segmentIndex := range module.Data {
				segment := &module.Data[segmentIndex]
				resolved := &layout.modules[consumerIndex].clone.Data[segmentIndex]
				if segment.Passive {
					continue
				}
				if uint64(resolved.Offset)+uint64(len(segment.Bytes)) > uint64(memoryLength) {
					return nil, fmt.Errorf("embedded32: module %q active data segment %d exceeds linked memory", plan.Modules[consumerIndex].Name, segmentIndex)
				}
				if memoryBase < opts.BaseAddress {
					return nil, fmt.Errorf("embedded32: module %q linked memory is outside the image", plan.Modules[consumerIndex].Name)
				}
				start := uint64(memoryBase-opts.BaseAddress) + uint64(resolved.Offset)
				end := start + uint64(len(segment.Bytes))
				if end > uint64(len(out.Bytes)) {
					return nil, fmt.Errorf("embedded32: module %q active data segment %d targets memory outside the image", plan.Modules[consumerIndex].Name, segmentIndex)
				}
				copy(out.Bytes[start:end], segment.Bytes)
				if err := putAddress(descriptors+uint32(segmentIndex)*embedded32.DataSegmentABIBytes+embedded32.DataSegmentDroppedOffset, 1); err != nil {
					return nil, err
				}
			}
		}
		tables := embeddedModuleTables(module)
		if len(tables) != 0 {
			directory, err := readAddress(image.ContextAddress + embedded32.ContextTablesBaseOffset)
			if err != nil {
				return nil, err
			}
			for tableIndex := range tables {
				if !tables[tableIndex].Imported {
					continue
				}
				importIndex, ok := embeddedImportIndex(module, wasm.ExternTable, uint32(tableIndex))
				if !ok {
					return nil, fmt.Errorf("embedded32: module %q table import %d is unavailable", plan.Modules[consumerIndex].Name, tableIndex)
				}
				providerTable, err := resolveLinkedTableAddress(plan, layout, out, consumerIndex, importIndex, make(map[[2]int]bool))
				if err != nil {
					return nil, err
				}
				if err := putAddress(directory+uint32(tableIndex)*4, providerTable); err != nil {
					return nil, err
				}
				if tableIndex == 0 {
					if err := putAddress(image.ContextAddress+embedded32.ContextTableOffset, providerTable); err != nil {
						return nil, err
					}
					if err := putAddress(image.ContextAddress+embedded32.ContextTableStorageOffset, providerTable); err != nil {
						return nil, err
					}
				}
			}
			elements := embeddedModuleElements(module)
			descriptors, err := readAddress(image.ContextAddress + embedded32.ContextElementSegmentsBaseOffset)
			if err != nil {
				return nil, err
			}
			for elementIndex := range elements {
				segment := &elements[elementIndex]
				if segment.Mode != EmbeddedElementActive || uint64(segment.Table) >= uint64(len(tables)) || !tables[segment.Table].Imported {
					continue
				}
				providerTable, err := readAddress(directory + segment.Table*4)
				if err != nil {
					return nil, err
				}
				entries, err := readAddress(providerTable + embedded32.TableABIEntriesBaseOffset)
				if err != nil {
					return nil, err
				}
				length, err := readAddress(providerTable + embedded32.TableABILengthOffset)
				if err != nil {
					return nil, err
				}
				if uint64(segment.Offset)+uint64(len(segment.Values)) > uint64(length) {
					return nil, fmt.Errorf("embedded32: module %q active element segment %d exceeds linked table", plan.Modules[consumerIndex].Name, elementIndex)
				}
				values, err := readAddress(descriptors + uint32(elementIndex)*embedded32.DataSegmentABIBytes + embedded32.DataSegmentBaseOffset)
				if err != nil {
					return nil, err
				}
				for valueIndex := range segment.Values {
					value, err := readAddress(values + uint32(valueIndex)*4)
					if err != nil {
						return nil, err
					}
					if err := putAddress(entries+(segment.Offset+uint32(valueIndex))*4, value); err != nil {
						return nil, err
					}
				}
				if err := putAddress(descriptors+uint32(elementIndex)*embedded32.DataSegmentABIBytes+embedded32.DataSegmentDroppedOffset, 1); err != nil {
					return nil, err
				}
			}
		}
		if len(module.ImportedGlobals) != 0 {
			directoryAddress := opts.BaseAddress + moduleLayout.globalsOffset
			if err := putAddress(image.ContextAddress+embedded32.ContextImportedGlobalsBaseOffset, directoryAddress); err != nil {
				return nil, err
			}
			for ordinal := uint32(0); ordinal < uint32(len(module.ImportedGlobals)); ordinal++ {
				importIndex, ok := embeddedImportIndex(module, wasm.ExternGlobal, ordinal)
				if !ok {
					return nil, fmt.Errorf("embedded32: module %q global import %d is unavailable", plan.Modules[consumerIndex].Name, ordinal)
				}
				address, err := resolveLinkedGlobalAddress(plan, layout, out, consumerIndex, importIndex, make(map[[2]int]bool))
				if err != nil {
					return nil, err
				}
				if err := putAddress(directoryAddress+ordinal*4, address); err != nil {
					return nil, err
				}
			}
		}
	}
	return out, nil
}

func embeddedLinkedFirmwarePlan(plan *EmbeddedLinkPlan, opts EmbeddedLinkedFirmwareOptions) (*embeddedLinkedFirmwareLayout, error) {
	if plan == nil || len(plan.Modules) == 0 || len(opts.Modules) != len(plan.Modules) {
		return nil, embedded32.ErrInvalidArena
	}
	validated, err := ResolveEmbeddedLinks(plan.Modules)
	if err != nil {
		return nil, err
	}
	plan = validated
	binding := make(map[[2]int]EmbeddedLinkBinding, len(plan.Bindings))
	for _, item := range plan.Bindings {
		key := [2]int{item.ConsumerModule, item.ImportIndex}
		if _, ok := binding[key]; ok {
			return nil, fmt.Errorf("embedded32: duplicate linked binding for module %d import %d", item.ConsumerModule, item.ImportIndex)
		}
		binding[key] = item
	}
	layout := &embeddedLinkedFirmwareLayout{modules: make([]embeddedLinkedModuleLayout, len(plan.Modules)), binding: binding, functionBase: make([]uint32, len(plan.Modules))}
	for i := range plan.Modules {
		layout.functionBase[i] = layout.functionCount
		count := uint64(len(plan.Modules[i].Module.Functions))
		if uint64(layout.functionCount)+count > uint64(^uint32(0)) {
			return nil, embedded32.ErrArenaCapacity
		}
		layout.functionCount += uint32(count)
	}
	globalWords := make(map[[2]uint32][4]uint32)
	visitingGlobals := make(map[[2]uint32]bool)
	var offset uint64
	for i := range plan.Modules {
		module := plan.Modules[i].Module
		if module == nil {
			return nil, fmt.Errorf("embedded32: linked module %d is nil", i)
		}
		clone := *module
		clone.Exports = make([]EmbeddedExport, 0, len(module.Exports))
		for j := range module.Exports {
			export := module.Exports[j]
			if export.Kind != wasm.ExternFunc || export.Index >= module.ImportedFunctions {
				clone.Exports = append(clone.Exports, export)
			}
		}
		clone.Imports = nil
		clone.ImportedFunctions = 0
		clone.ImportedGlobals = nil
		clone.MemoryImported = false
		resolveOffset := func(global uint32) (uint32, error) {
			importIndex, ok := embeddedImportIndex(module, wasm.ExternGlobal, global)
			if !ok {
				return 0, fmt.Errorf("embedded32: module %q offset global import %d is unavailable", plan.Modules[i].Name, global)
			}
			item, ok := binding[[2]int{i, importIndex}]
			if !ok {
				return 0, fmt.Errorf("embedded32: module %q offset global import %d is unbound", plan.Modules[i].Name, global)
			}
			providerExport := plan.Modules[item.ProviderModule].Module.Exports[item.ExportIndex]
			words, err := resolveLinkedGlobalWords(plan, binding, item.ProviderModule, providerExport.Index, globalWords, visitingGlobals)
			return words[0], err
		}
		clone.Data = append([]EmbeddedDataSegment(nil), module.Data...)
		for j := range clone.Data {
			if clone.Data[j].HasOffsetGlobal {
				value, err := resolveOffset(clone.Data[j].OffsetGlobal)
				if err != nil {
					return nil, err
				}
				clone.Data[j].Offset, clone.Data[j].HasOffsetGlobal = value, false
			}
		}
		clone.Tables = append([]EmbeddedTable(nil), module.Tables...)
		for j := range clone.Tables {
			imported := clone.Tables[j].Imported
			clone.Tables[j].Imported = false
			clone.Tables[j].Elements = nil
			if imported {
				clone.Tables[j].Minimum = 0
				clone.Tables[j].Maximum = 0
				clone.Tables[j].HasMaximum = true
			}
		}
		clone.Elements = append([]EmbeddedElementSegment(nil), module.Elements...)
		if module.Elements == nil && module.Table != nil {
			clone.Elements = append([]EmbeddedElementSegment(nil), module.Table.Elements...)
		}
		for j := range clone.Elements {
			if clone.Elements[j].Mode == EmbeddedElementActive && uint64(clone.Elements[j].Table) < uint64(len(module.Tables)) && module.Tables[clone.Elements[j].Table].Imported {
				clone.Elements[j].Mode = EmbeddedElementPassive
			}
			if clone.Elements[j].HasOffsetGlobal {
				value, err := resolveOffset(clone.Elements[j].OffsetGlobal)
				if err != nil {
					return nil, err
				}
				clone.Elements[j].Offset, clone.Elements[j].HasOffsetGlobal = value, false
			}
		}
		if len(clone.Tables) == 1 {
			clone.Tables[0].Elements = clone.Elements
			clone.Table = &clone.Tables[0]
		} else if len(clone.Tables) > 1 {
			compat := clone.Tables[0]
			compat.Elements = clone.Elements
			clone.Table = &compat
		} else if len(clone.Elements) != 0 {
			clone.Table = &EmbeddedTable{Reference: wasm.FuncRef.Ref, Elements: clone.Elements}
		} else {
			clone.Table = nil
		}
		if module.MemoryImported {
			importIndex, ok := embeddedImportIndex(module, wasm.ExternMem, 0)
			if !ok {
				return nil, fmt.Errorf("embedded32: module %q memory import is unavailable", plan.Modules[i].Name)
			}
			providerIndex, err := resolveLinkedMemoryModule(plan, binding, i, importIndex, make(map[[2]int]bool))
			if err != nil {
				return nil, err
			}
			providerMemory := plan.Modules[providerIndex].Module.Memory
			initialBytes := uint64(providerMemory.Minimum) * uint64(embedded32.WasmPageSize)
			for j := range clone.Data {
				segment := &clone.Data[j]
				if !segment.Passive && uint64(segment.Offset)+uint64(len(segment.Bytes)) > initialBytes {
					return nil, fmt.Errorf("embedded32: module %q active data segment %d exceeds linked initial memory", plan.Modules[i].Name, j)
				}
				if !segment.Passive {
					segment.Passive = true
				}
			}
			clone.Memory = nil
		}
		clone.Globals = append([]EmbeddedGlobal(nil), module.Globals...)
		for j := range clone.Globals {
			if !clone.Globals[j].HasInitGlobal {
				continue
			}
			importIndex, ok := embeddedImportIndex(module, wasm.ExternGlobal, clone.Globals[j].InitGlobal)
			if !ok {
				return nil, fmt.Errorf("embedded32: module %q global initializer import %d is unavailable", plan.Modules[i].Name, clone.Globals[j].InitGlobal)
			}
			item, ok := binding[[2]int{i, importIndex}]
			if !ok {
				return nil, fmt.Errorf("embedded32: module %q global initializer is unbound", plan.Modules[i].Name)
			}
			providerExport := plan.Modules[item.ProviderModule].Module.Exports[item.ExportIndex]
			words, err := resolveLinkedGlobalWords(plan, binding, item.ProviderModule, providerExport.Index, globalWords, visitingGlobals)
			if err != nil {
				return nil, err
			}
			clone.Globals[j].Words = words
			clone.Globals[j].HasInitGlobal = false
		}
		moduleOptions := opts.Modules[i]
		if len(clone.Tables) != 0 {
			capacities := make([]uint32, len(clone.Tables))
			for tableIndex := range capacities {
				if module.Tables[tableIndex].Imported {
					continue
				}
				if tableIndex < len(moduleOptions.TableCapacities) {
					capacities[tableIndex] = moduleOptions.TableCapacities[tableIndex]
				} else if tableIndex == 0 {
					capacities[tableIndex] = moduleOptions.TableCapacity
				}
			}
			moduleOptions.TableCapacity = 0
			moduleOptions.TableCapacities = capacities
		}
		if i != 0 && moduleOptions.FunctionAddressMask != opts.Modules[0].FunctionAddressMask {
			return nil, fmt.Errorf("embedded32: linked modules use different function address masks")
		}
		offset = (offset + 15) &^ 15
		if offset > uint64(^uint32(0)) || uint64(opts.BaseAddress)+offset > uint64(^uint32(0)) {
			return nil, embedded32.ErrArenaCapacity
		}
		moduleOptions.BaseAddress = opts.BaseAddress + uint32(offset)
		size, err := EmbeddedFirmwareImageSize(&clone, moduleOptions)
		if err != nil {
			return nil, fmt.Errorf("embedded32: module %q firmware layout: %w", plan.Modules[i].Name, err)
		}
		moduleLayout := &layout.modules[i]
		moduleLayout.clone = &clone
		moduleLayout.options = moduleOptions
		moduleLayout.offset = uint32(offset)
		moduleLayout.imageBytes = size
		offset += uint64(size)
		offset = (offset + 3) &^ 3
		if offset > uint64(^uint32(0)) {
			return nil, embedded32.ErrArenaCapacity
		}
		moduleLayout.importsOffset = uint32(offset)
		offset += uint64(module.ImportedFunctions) * embedded32.ImportFunctionABIBytes
		if offset > uint64(^uint32(0)) {
			return nil, embedded32.ErrArenaCapacity
		}
		moduleLayout.globalsOffset = uint32(offset)
		offset += uint64(len(module.ImportedGlobals)) * 4
		if offset > uint64(^uint32(0)) {
			return nil, embedded32.ErrArenaCapacity
		}
		moduleLayout.regionBytes = uint32(offset) - moduleLayout.offset
	}
	offset = (offset + 3) &^ 3
	if layout.functionCount != 0 {
		layout.functionEntriesOffset = uint32(offset)
		offset += uint64(layout.functionCount) * 4
		layout.functionTypesOffset = uint32(offset)
		offset += uint64(layout.functionCount) * 4
		layout.functionContextsOffset = uint32(offset)
		offset += uint64(layout.functionCount) * 4
	}
	if offset > uint64(^uint32(0)) || uint64(opts.BaseAddress)+offset > uint64(^uint32(0)) {
		return nil, embedded32.ErrArenaCapacity
	}
	layout.required = uint32(offset)
	for consumer := range plan.Modules {
		for importIndex := range plan.Modules[consumer].Module.Imports {
			kind := plan.Modules[consumer].Module.Imports[importIndex].Kind
			if kind != wasm.ExternFunc && kind != wasm.ExternGlobal && kind != wasm.ExternMem && kind != wasm.ExternTable {
				return nil, fmt.Errorf("embedded32: module %q import %d kind %d is not linkable in a firmware bundle", plan.Modules[consumer].Name, importIndex, kind)
			}
			if _, ok := binding[[2]int{consumer, importIndex}]; !ok {
				return nil, fmt.Errorf("embedded32: module %q import %d is unbound", plan.Modules[consumer].Name, importIndex)
			}
			if kind == wasm.ExternFunc {
				if err := validateLinkedFunctionTarget(plan, binding, consumer, importIndex, make(map[[2]int]bool)); err != nil {
					return nil, err
				}
			} else if kind == wasm.ExternGlobal {
				if err := validateLinkedGlobalTarget(plan, binding, consumer, importIndex, make(map[[2]int]bool)); err != nil {
					return nil, err
				}
			} else if kind == wasm.ExternTable {
				if err := validateLinkedTableTarget(plan, binding, consumer, importIndex, make(map[[2]int]bool)); err != nil {
					return nil, err
				}
			} else if err := validateLinkedMemoryTarget(plan, binding, consumer, importIndex, make(map[[2]int]bool)); err != nil {
				return nil, err
			}
		}
	}
	return layout, nil
}

func embeddedImportIndex(module *EmbeddedModule, kind wasm.ExternKind, ordinal uint32) (int, bool) {
	if module == nil {
		return 0, false
	}
	for i := range module.Imports {
		if module.Imports[i].Kind == kind && module.Imports[i].Index == ordinal {
			return i, true
		}
	}
	return 0, false
}

func validateLinkedFunctionTarget(plan *EmbeddedLinkPlan, binding map[[2]int]EmbeddedLinkBinding, consumer, importIndex int, visiting map[[2]int]bool) error {
	key := [2]int{consumer, importIndex}
	if visiting[key] {
		return fmt.Errorf("embedded32: cyclic function re-export at module %q import %d", plan.Modules[consumer].Name, importIndex)
	}
	visiting[key] = true
	defer delete(visiting, key)
	item, ok := binding[key]
	if !ok {
		return fmt.Errorf("embedded32: unbound function import at module %q import %d", plan.Modules[consumer].Name, importIndex)
	}
	provider := plan.Modules[item.ProviderModule].Module
	export := provider.Exports[item.ExportIndex]
	if export.Index < provider.ImportedFunctions {
		providerImport, ok := embeddedImportIndex(provider, wasm.ExternFunc, export.Index)
		if !ok {
			return fmt.Errorf("embedded32: provider function import %d is unavailable", export.Index)
		}
		return validateLinkedFunctionTarget(plan, binding, item.ProviderModule, providerImport, visiting)
	}
	for i := range provider.Functions {
		if provider.Functions[i].FuncIndex == export.Index {
			return nil
		}
	}
	return fmt.Errorf("embedded32: provider function %d has no local entry", export.Index)
}

func validateLinkedMemoryTarget(plan *EmbeddedLinkPlan, binding map[[2]int]EmbeddedLinkBinding, consumer, importIndex int, visiting map[[2]int]bool) error {
	key := [2]int{consumer, importIndex}
	if visiting[key] {
		return fmt.Errorf("embedded32: cyclic memory re-export at module %q import %d", plan.Modules[consumer].Name, importIndex)
	}
	visiting[key] = true
	defer delete(visiting, key)
	item, ok := binding[key]
	if !ok {
		return fmt.Errorf("embedded32: unbound memory import at module %q import %d", plan.Modules[consumer].Name, importIndex)
	}
	provider := plan.Modules[item.ProviderModule].Module
	export := provider.Exports[item.ExportIndex]
	if export.Index != 0 || provider.Memory == nil {
		return fmt.Errorf("embedded32: provider memory %d is unavailable", export.Index)
	}
	if provider.Memory.Imported {
		providerImport, ok := embeddedImportIndex(provider, wasm.ExternMem, 0)
		if !ok {
			return fmt.Errorf("embedded32: provider memory import is unavailable")
		}
		return validateLinkedMemoryTarget(plan, binding, item.ProviderModule, providerImport, visiting)
	}
	return nil
}

func validateLinkedTableTarget(plan *EmbeddedLinkPlan, binding map[[2]int]EmbeddedLinkBinding, consumer, importIndex int, visiting map[[2]int]bool) error {
	key := [2]int{consumer, importIndex}
	if visiting[key] {
		return fmt.Errorf("embedded32: cyclic table re-export at module %q import %d", plan.Modules[consumer].Name, importIndex)
	}
	visiting[key] = true
	defer delete(visiting, key)
	item, ok := binding[key]
	if !ok {
		return fmt.Errorf("embedded32: unbound table import at module %q import %d", plan.Modules[consumer].Name, importIndex)
	}
	provider := plan.Modules[item.ProviderModule].Module
	export := provider.Exports[item.ExportIndex]
	tables := embeddedModuleTables(provider)
	if uint64(export.Index) >= uint64(len(tables)) {
		return fmt.Errorf("embedded32: provider table %d is unavailable", export.Index)
	}
	if tables[export.Index].Imported {
		providerImport, ok := embeddedImportIndex(provider, wasm.ExternTable, export.Index)
		if !ok {
			return fmt.Errorf("embedded32: provider table import %d is unavailable", export.Index)
		}
		return validateLinkedTableTarget(plan, binding, item.ProviderModule, providerImport, visiting)
	}
	return nil
}

func validateLinkedGlobalTarget(plan *EmbeddedLinkPlan, binding map[[2]int]EmbeddedLinkBinding, consumer, importIndex int, visiting map[[2]int]bool) error {
	key := [2]int{consumer, importIndex}
	if visiting[key] {
		return fmt.Errorf("embedded32: cyclic global re-export at module %q import %d", plan.Modules[consumer].Name, importIndex)
	}
	visiting[key] = true
	defer delete(visiting, key)
	item, ok := binding[key]
	if !ok {
		return fmt.Errorf("embedded32: unbound global import at module %q import %d", plan.Modules[consumer].Name, importIndex)
	}
	provider := plan.Modules[item.ProviderModule].Module
	export := provider.Exports[item.ExportIndex]
	if uint64(export.Index) < uint64(len(provider.ImportedGlobals)) {
		providerImport, ok := embeddedImportIndex(provider, wasm.ExternGlobal, export.Index)
		if !ok {
			return fmt.Errorf("embedded32: provider global import %d is unavailable", export.Index)
		}
		return validateLinkedGlobalTarget(plan, binding, item.ProviderModule, providerImport, visiting)
	}
	local := uint64(export.Index) - uint64(len(provider.ImportedGlobals))
	if local >= uint64(len(provider.Globals)) {
		return fmt.Errorf("embedded32: provider global %d is unavailable", export.Index)
	}
	return nil
}

func resolveLinkedFunction(plan *EmbeddedLinkPlan, layout *embeddedLinkedFirmwareLayout, image *EmbeddedLinkedFirmwareImage, consumer, importIndex int, visiting map[[2]int]bool) (uint32, uint32, error) {
	key := [2]int{consumer, importIndex}
	if visiting[key] {
		return 0, 0, fmt.Errorf("embedded32: cyclic function re-export at module %q import %d", plan.Modules[consumer].Name, importIndex)
	}
	visiting[key] = true
	defer delete(visiting, key)
	binding, ok := layout.binding[key]
	if !ok {
		return 0, 0, fmt.Errorf("embedded32: unbound function import at module %q import %d", plan.Modules[consumer].Name, importIndex)
	}
	provider := plan.Modules[binding.ProviderModule].Module
	export := provider.Exports[binding.ExportIndex]
	if export.Index < provider.ImportedFunctions {
		providerImport, ok := embeddedImportIndex(provider, wasm.ExternFunc, export.Index)
		if !ok {
			return 0, 0, fmt.Errorf("embedded32: provider function import %d is unavailable", export.Index)
		}
		return resolveLinkedFunction(plan, layout, image, binding.ProviderModule, providerImport, visiting)
	}
	for i := range provider.Functions {
		function := &provider.Functions[i]
		if function.FuncIndex == export.Index {
			providerImage := image.Modules[binding.ProviderModule].Image
			entry := providerImage.CodeAddress + function.Offset
			entry |= layout.modules[binding.ProviderModule].options.FunctionAddressMask
			return entry, providerImage.ContextAddress, nil
		}
	}
	return 0, 0, fmt.Errorf("embedded32: provider function %d has no local entry", export.Index)
}

func resolveLinkedFunctionCall(plan *EmbeddedLinkPlan, layout *embeddedLinkedFirmwareLayout, image *EmbeddedLinkedFirmwareImage, moduleIndex int, functionIndex uint32, visiting map[[2]uint32]bool) (embedded32.FirmwareTransportFunction, error) {
	key := [2]uint32{uint32(moduleIndex), functionIndex}
	if visiting[key] {
		return embedded32.FirmwareTransportFunction{}, fmt.Errorf("embedded32: cyclic function re-export at module %q function %d", plan.Modules[moduleIndex].Name, functionIndex)
	}
	visiting[key] = true
	defer delete(visiting, key)
	module := plan.Modules[moduleIndex].Module
	if functionIndex < module.ImportedFunctions {
		importIndex, ok := embeddedImportIndex(module, wasm.ExternFunc, functionIndex)
		if !ok {
			return embedded32.FirmwareTransportFunction{}, fmt.Errorf("embedded32: module %q function import %d is unavailable", plan.Modules[moduleIndex].Name, functionIndex)
		}
		item, ok := layout.binding[[2]int{moduleIndex, importIndex}]
		if !ok {
			return embedded32.FirmwareTransportFunction{}, fmt.Errorf("embedded32: module %q function import %d is unbound", plan.Modules[moduleIndex].Name, functionIndex)
		}
		export := plan.Modules[item.ProviderModule].Module.Exports[item.ExportIndex]
		return resolveLinkedFunctionCall(plan, layout, image, item.ProviderModule, export.Index, visiting)
	}
	for i := range module.Functions {
		function := &module.Functions[i]
		if function.FuncIndex != functionIndex || !function.HasCallEntry {
			continue
		}
		providerImage := image.Modules[moduleIndex].Image
		return embedded32.FirmwareTransportFunction{
			Address:     providerImage.CodeAddress + function.CallOffset | layout.modules[moduleIndex].options.FunctionAddressMask,
			Context:     providerImage.ContextAddress,
			ParamSlots:  function.ParamSlots,
			ResultSlots: function.ResultSlots,
		}, nil
	}
	return embedded32.FirmwareTransportFunction{}, fmt.Errorf("embedded32: module %q function %d has no call entry", plan.Modules[moduleIndex].Name, functionIndex)
}

func resolveLinkedFunctionID(plan *EmbeddedLinkPlan, layout *embeddedLinkedFirmwareLayout, moduleIndex int, functionIndex uint32, visiting map[[2]uint32]bool) (uint32, error) {
	key := [2]uint32{uint32(moduleIndex), functionIndex}
	if visiting[key] {
		return 0, fmt.Errorf("embedded32: cyclic function identity at module %q function %d", plan.Modules[moduleIndex].Name, functionIndex)
	}
	visiting[key] = true
	defer delete(visiting, key)
	module := plan.Modules[moduleIndex].Module
	if functionIndex < module.ImportedFunctions {
		importIndex, ok := embeddedImportIndex(module, wasm.ExternFunc, functionIndex)
		if !ok {
			return 0, fmt.Errorf("embedded32: module %q function import %d is unavailable", plan.Modules[moduleIndex].Name, functionIndex)
		}
		item, ok := layout.binding[[2]int{moduleIndex, importIndex}]
		if !ok {
			return 0, fmt.Errorf("embedded32: module %q function import %d is unbound", plan.Modules[moduleIndex].Name, functionIndex)
		}
		export := plan.Modules[item.ProviderModule].Module.Exports[item.ExportIndex]
		return resolveLinkedFunctionID(plan, layout, item.ProviderModule, export.Index, visiting)
	}
	local := functionIndex - module.ImportedFunctions
	if uint64(local) >= uint64(len(module.Functions)) {
		return 0, fmt.Errorf("embedded32: module %q local function %d is unavailable", plan.Modules[moduleIndex].Name, functionIndex)
	}
	return layout.functionBase[moduleIndex] + local, nil
}

func resolveLinkedTableAddress(plan *EmbeddedLinkPlan, layout *embeddedLinkedFirmwareLayout, image *EmbeddedLinkedFirmwareImage, consumer, importIndex int, visiting map[[2]int]bool) (uint32, error) {
	key := [2]int{consumer, importIndex}
	if visiting[key] {
		return 0, fmt.Errorf("embedded32: cyclic table re-export at module %q import %d", plan.Modules[consumer].Name, importIndex)
	}
	visiting[key] = true
	defer delete(visiting, key)
	item, ok := layout.binding[key]
	if !ok {
		return 0, fmt.Errorf("embedded32: unbound table import at module %q import %d", plan.Modules[consumer].Name, importIndex)
	}
	provider := plan.Modules[item.ProviderModule].Module
	export := provider.Exports[item.ExportIndex]
	tables := embeddedModuleTables(provider)
	if uint64(export.Index) >= uint64(len(tables)) {
		return 0, fmt.Errorf("embedded32: provider table %d is unavailable", export.Index)
	}
	if tables[export.Index].Imported {
		providerImport, ok := embeddedImportIndex(provider, wasm.ExternTable, export.Index)
		if !ok {
			return 0, fmt.Errorf("embedded32: provider table import %d is unavailable", export.Index)
		}
		return resolveLinkedTableAddress(plan, layout, image, item.ProviderModule, providerImport, visiting)
	}
	providerImage := image.Modules[item.ProviderModule].Image
	if uint64(export.Index) >= uint64(len(providerImage.TableAddresses)) {
		return 0, fmt.Errorf("embedded32: provider table image %d is unavailable", export.Index)
	}
	return providerImage.TableAddresses[export.Index], nil
}

func resolveLinkedMemoryModule(plan *EmbeddedLinkPlan, binding map[[2]int]EmbeddedLinkBinding, consumer, importIndex int, visiting map[[2]int]bool) (int, error) {
	key := [2]int{consumer, importIndex}
	if visiting[key] {
		return 0, fmt.Errorf("embedded32: cyclic memory re-export at module %q import %d", plan.Modules[consumer].Name, importIndex)
	}
	visiting[key] = true
	defer delete(visiting, key)
	item, ok := binding[key]
	if !ok {
		return 0, fmt.Errorf("embedded32: unbound memory import at module %q import %d", plan.Modules[consumer].Name, importIndex)
	}
	provider := plan.Modules[item.ProviderModule].Module
	export := provider.Exports[item.ExportIndex]
	if export.Index != 0 || provider.Memory == nil {
		return 0, fmt.Errorf("embedded32: provider memory %d is unavailable", export.Index)
	}
	if provider.Memory.Imported {
		providerImport, ok := embeddedImportIndex(provider, wasm.ExternMem, 0)
		if !ok {
			return 0, fmt.Errorf("embedded32: provider memory import is unavailable")
		}
		return resolveLinkedMemoryModule(plan, binding, item.ProviderModule, providerImport, visiting)
	}
	return item.ProviderModule, nil
}

func resolveLinkedMemoryContext(plan *EmbeddedLinkPlan, layout *embeddedLinkedFirmwareLayout, image *EmbeddedLinkedFirmwareImage, consumer, importIndex int, visiting map[[2]int]bool) (uint32, error) {
	key := [2]int{consumer, importIndex}
	if visiting[key] {
		return 0, fmt.Errorf("embedded32: cyclic memory re-export at module %q import %d", plan.Modules[consumer].Name, importIndex)
	}
	visiting[key] = true
	defer delete(visiting, key)
	item, ok := layout.binding[key]
	if !ok {
		return 0, fmt.Errorf("embedded32: unbound memory import at module %q import %d", plan.Modules[consumer].Name, importIndex)
	}
	provider := plan.Modules[item.ProviderModule].Module
	export := provider.Exports[item.ExportIndex]
	if export.Index != 0 || provider.Memory == nil {
		return 0, fmt.Errorf("embedded32: provider memory %d is unavailable", export.Index)
	}
	if provider.Memory.Imported {
		providerImport, ok := embeddedImportIndex(provider, wasm.ExternMem, 0)
		if !ok {
			return 0, fmt.Errorf("embedded32: provider memory import is unavailable")
		}
		return resolveLinkedMemoryContext(plan, layout, image, item.ProviderModule, providerImport, visiting)
	}
	return image.Modules[item.ProviderModule].Image.ContextAddress, nil
}

func resolveLinkedGlobalAddress(plan *EmbeddedLinkPlan, layout *embeddedLinkedFirmwareLayout, image *EmbeddedLinkedFirmwareImage, consumer, importIndex int, visiting map[[2]int]bool) (uint32, error) {
	key := [2]int{consumer, importIndex}
	if visiting[key] {
		return 0, fmt.Errorf("embedded32: cyclic global re-export at module %q import %d", plan.Modules[consumer].Name, importIndex)
	}
	visiting[key] = true
	defer delete(visiting, key)
	binding, ok := layout.binding[key]
	if !ok {
		return 0, fmt.Errorf("embedded32: unbound global import at module %q import %d", plan.Modules[consumer].Name, importIndex)
	}
	provider := plan.Modules[binding.ProviderModule].Module
	export := provider.Exports[binding.ExportIndex]
	if uint64(export.Index) < uint64(len(provider.ImportedGlobals)) {
		providerImport, ok := embeddedImportIndex(provider, wasm.ExternGlobal, export.Index)
		if !ok {
			return 0, fmt.Errorf("embedded32: provider global import %d is unavailable", export.Index)
		}
		return resolveLinkedGlobalAddress(plan, layout, image, binding.ProviderModule, providerImport, visiting)
	}
	local := uint64(export.Index) - uint64(len(provider.ImportedGlobals))
	if local >= uint64(len(provider.Globals)) {
		return 0, fmt.Errorf("embedded32: provider global %d is unavailable", export.Index)
	}
	global := provider.Globals[local]
	providerImage := image.Modules[binding.ProviderModule].Image
	return providerImage.GlobalsAddress + global.Slot*4, nil
}

func resolveLinkedFuncRefGlobal(plan *EmbeddedLinkPlan, layout *embeddedLinkedFirmwareLayout, moduleIndex int, globalIndex uint32, visiting map[[2]uint32]bool) (uint32, error) {
	key := [2]uint32{uint32(moduleIndex), globalIndex}
	if visiting[key] {
		return 0, fmt.Errorf("embedded32: cyclic funcref global at module %q global %d", plan.Modules[moduleIndex].Name, globalIndex)
	}
	visiting[key] = true
	defer delete(visiting, key)
	module := plan.Modules[moduleIndex].Module
	if uint64(globalIndex) < uint64(len(module.ImportedGlobals)) {
		importIndex, ok := embeddedImportIndex(module, wasm.ExternGlobal, globalIndex)
		if !ok {
			return 0, fmt.Errorf("embedded32: module %q funcref global import %d is unavailable", plan.Modules[moduleIndex].Name, globalIndex)
		}
		item, ok := layout.binding[[2]int{moduleIndex, importIndex}]
		if !ok {
			return 0, fmt.Errorf("embedded32: module %q funcref global import %d is unbound", plan.Modules[moduleIndex].Name, globalIndex)
		}
		export := plan.Modules[item.ProviderModule].Module.Exports[item.ExportIndex]
		return resolveLinkedFuncRefGlobal(plan, layout, item.ProviderModule, export.Index, visiting)
	}
	local := uint64(globalIndex) - uint64(len(module.ImportedGlobals))
	if local >= uint64(len(module.Globals)) {
		return 0, fmt.Errorf("embedded32: module %q funcref global %d is unavailable", plan.Modules[moduleIndex].Name, globalIndex)
	}
	global := module.Globals[local]
	valueType, ok := embeddedReferenceValueType(global.Type.Ref)
	if !ok || valueType != wasm.FuncRef {
		return 0, fmt.Errorf("embedded32: module %q global %d is not funcref", plan.Modules[moduleIndex].Name, globalIndex)
	}
	if global.HasInitGlobal {
		importIndex, ok := embeddedImportIndex(module, wasm.ExternGlobal, global.InitGlobal)
		if !ok {
			return 0, fmt.Errorf("embedded32: module %q funcref initializer import %d is unavailable", plan.Modules[moduleIndex].Name, global.InitGlobal)
		}
		item, ok := layout.binding[[2]int{moduleIndex, importIndex}]
		if !ok {
			return 0, fmt.Errorf("embedded32: module %q funcref initializer import %d is unbound", plan.Modules[moduleIndex].Name, global.InitGlobal)
		}
		export := plan.Modules[item.ProviderModule].Module.Exports[item.ExportIndex]
		return resolveLinkedFuncRefGlobal(plan, layout, item.ProviderModule, export.Index, visiting)
	}
	if global.Words[0] == 0 {
		return 0, nil
	}
	id, err := resolveLinkedFunctionID(plan, layout, moduleIndex, global.Words[0]-1, make(map[[2]uint32]bool))
	if err != nil {
		return 0, err
	}
	return id + 1, nil
}

func resolveLinkedGlobalWords(plan *EmbeddedLinkPlan, binding map[[2]int]EmbeddedLinkBinding, moduleIndex int, globalIndex uint32, memo map[[2]uint32][4]uint32, visiting map[[2]uint32]bool) ([4]uint32, error) {
	key := [2]uint32{uint32(moduleIndex), globalIndex}
	if words, ok := memo[key]; ok {
		return words, nil
	}
	if visiting[key] {
		return [4]uint32{}, fmt.Errorf("embedded32: cyclic global initializer at module %q global %d", plan.Modules[moduleIndex].Name, globalIndex)
	}
	visiting[key] = true
	defer delete(visiting, key)
	module := plan.Modules[moduleIndex].Module
	if uint64(globalIndex) < uint64(len(module.ImportedGlobals)) {
		importIndex, ok := embeddedImportIndex(module, wasm.ExternGlobal, globalIndex)
		if !ok {
			return [4]uint32{}, fmt.Errorf("embedded32: module %q global import %d is unavailable", plan.Modules[moduleIndex].Name, globalIndex)
		}
		item, ok := binding[[2]int{moduleIndex, importIndex}]
		if !ok {
			return [4]uint32{}, fmt.Errorf("embedded32: module %q global import %d is unbound", plan.Modules[moduleIndex].Name, globalIndex)
		}
		export := plan.Modules[item.ProviderModule].Module.Exports[item.ExportIndex]
		return resolveLinkedGlobalWords(plan, binding, item.ProviderModule, export.Index, memo, visiting)
	}
	local := uint64(globalIndex) - uint64(len(module.ImportedGlobals))
	if local >= uint64(len(module.Globals)) {
		return [4]uint32{}, fmt.Errorf("embedded32: module %q global %d is unavailable", plan.Modules[moduleIndex].Name, globalIndex)
	}
	global := module.Globals[local]
	words := global.Words
	if global.HasInitGlobal {
		importIndex, ok := embeddedImportIndex(module, wasm.ExternGlobal, global.InitGlobal)
		if !ok {
			return [4]uint32{}, fmt.Errorf("embedded32: module %q initializer import %d is unavailable", plan.Modules[moduleIndex].Name, global.InitGlobal)
		}
		item, ok := binding[[2]int{moduleIndex, importIndex}]
		if !ok {
			return [4]uint32{}, fmt.Errorf("embedded32: module %q initializer import %d is unbound", plan.Modules[moduleIndex].Name, global.InitGlobal)
		}
		export := plan.Modules[item.ProviderModule].Module.Exports[item.ExportIndex]
		var err error
		words, err = resolveLinkedGlobalWords(plan, binding, item.ProviderModule, export.Index, memo, visiting)
		if err != nil {
			return [4]uint32{}, err
		}
	}
	memo[key] = words
	return words, nil
}
