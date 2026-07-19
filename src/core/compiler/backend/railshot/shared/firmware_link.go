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
	required uint32
	modules  []embeddedLinkedModuleLayout
	binding  map[[2]int]EmbeddedLinkBinding
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
	layout := &embeddedLinkedFirmwareLayout{modules: make([]embeddedLinkedModuleLayout, len(plan.Modules)), binding: binding}
	globalWords := make(map[[2]uint32][4]uint32)
	visitingGlobals := make(map[[2]uint32]bool)
	var offset uint64
	for i := range plan.Modules {
		module := plan.Modules[i].Module
		if module == nil {
			return nil, fmt.Errorf("embedded32: linked module %d is nil", i)
		}
		if module.Table != nil && module.Table.Imported {
			return nil, fmt.Errorf("embedded32: module %q imports a table; shared table descriptors are not linked yet", plan.Modules[i].Name)
		}
		if module.Table != nil && module.ImportedFunctions != 0 {
			return nil, fmt.Errorf("embedded32: module %q combines imported functions with a table; context-aware table entries are not linked yet", plan.Modules[i].Name)
		}
		for j := range module.ImportedGlobals {
			if wasm.EqualValType(module.ImportedGlobals[j].Type, wasm.FuncRef) {
				return nil, fmt.Errorf("embedded32: module %q imports a funcref global; cross-module function references are not linked yet", plan.Modules[i].Name)
			}
		}
		for j := range module.Exports {
			export := &module.Exports[j]
			if export.Kind == wasm.ExternFunc && export.Index < module.ImportedFunctions {
				return nil, fmt.Errorf("embedded32: module %q re-exports imported function %q; transport entry forwarding is not linked yet", plan.Modules[i].Name, export.Name)
			}
		}
		clone := *module
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
		if module.Table != nil {
			table := *module.Table
			table.Elements = append([]EmbeddedElementSegment(nil), module.Table.Elements...)
			for j := range table.Elements {
				if table.Elements[j].HasOffsetGlobal {
					value, err := resolveOffset(table.Elements[j].OffsetGlobal)
					if err != nil {
						return nil, err
					}
					table.Elements[j].Offset, table.Elements[j].HasOffsetGlobal = value, false
				}
			}
			clone.Table = &table
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
	if offset > uint64(^uint32(0)) || uint64(opts.BaseAddress)+offset > uint64(^uint32(0)) {
		return nil, embedded32.ErrArenaCapacity
	}
	layout.required = uint32(offset)
	for consumer := range plan.Modules {
		for importIndex := range plan.Modules[consumer].Module.Imports {
			kind := plan.Modules[consumer].Module.Imports[importIndex].Kind
			if kind != wasm.ExternFunc && kind != wasm.ExternGlobal && kind != wasm.ExternMem {
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
