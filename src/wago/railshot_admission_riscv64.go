//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"slices"

	"github.com/wago-org/wago/src/core/compiler/wasm"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/gc/native"
	rv64runtime "github.com/wago-org/wago/src/core/runtime/riscv64"
)

func rv64FreshRootedGC(c *Compiled) bool {
	if c == nil {
		return false
	}
	memo := c.loadValidateMemo()
	if memo == nil || !memo.rv64FreshRootedGC || sha256.Sum256(c.code) != memo.rv64FreshRootedGCDigest {
		return false
	}
	if len(c.Types) == 0 || len(c.Types) > railshot.MaxFunctions || len(c.Funcs) == 0 || len(c.Funcs) > railshot.MaxFunctions ||
		len(c.FuncTypeID) > railshot.MaxFunctions || len(c.Exports) > railshot.MaxFunctions {
		return false
	}
	structProduct, arrayProduct := c.stagedGCStructProduct(), c.stagedGCArrayProduct()
	structHelpers, arrayHelpers := structProduct == stagedGCStructGeneric, arrayProduct == stagedGCArrayProductGeneric
	if structProduct != 0 && !structHelpers || arrayProduct != 0 && !arrayHelpers || !structHelpers && !arrayHelpers ||
		c.usesGCStructHelpers() != structHelpers || c.usesGCArrayHelpers() != arrayHelpers || c.syncHostSlots < 4 ||
		c.nativeGCABIRequirement() != gc.NativeABIVersion || c.NumImports != 0 || len(c.Imports) != 0 ||
		len(c.importFuncSigs) != 0 || c.HasTable || c.tableCount() != 0 || c.memoryCount() > 1 ||
		len(c.GlobalImports) != 0 || len(c.Globals) != 0 || c.HasStart || len(c.Elems) != 0 ||
		len(c.passiveElems) != 0 || len(c.Data) != 0 || len(c.PassiveData) != 0 {
		return false
	}
	directory := c.memoryDir
	if directory == nil || len(directory.defs) > 1 || len(directory.exports) != 0 || !directory.exactExports || directory.staged || directory.stagedMemory64 ||
		len(directory.gcStructGlobals) != 0 || len(directory.gcArrayGlobals) != 0 || directory.gcArrayElement != nil || directory.gcI31TableInit != nil ||
		len(directory.ehTags) != 0 || len(directory.ehTagExports) != 0 || uint8(len(directory.defs)) != memo.rv64FreshRootedGCMemories {
		return false
	}
	if len(directory.defs) == 1 {
		memory := directory.defs[0]
		if !c.HasMemory || memory.ImportKey != "" || memory.Shared || memory.Addr64 || !memory.HasMax || memory.Min > memory.Max || memory.Max > 256 ||
			uint64(c.MemMinPages) != memory.Min || uint64(c.MemMaxPages) < memory.Min || uint64(c.MemMaxPages) > memory.Max || c.MemHasMax != memory.HasMax || c.memoryImport != memory.ImportKey ||
			memory.Min != memo.rv64FreshRootedGCMemMin || memory.Max != memo.rv64FreshRootedGCMemMax || c.MemMaxPages != memo.rv64FreshRootedGCReserveMax {
			return false
		}
	} else if c.HasMemory || c.MemMinPages != 0 || c.MemMaxPages != 0 || c.MemHasMax || c.memoryImport != "" ||
		memo.rv64FreshRootedGCMemMin != 0 || memo.rv64FreshRootedGCMemMax != 0 || memo.rv64FreshRootedGCReserveMax != 0 {
		return false
	}
	roots := c.genericGCFrameRoots()
	if roots == nil || len(roots.safepoints) == 0 || len(c.Types) != len(c.GCTypeDescs) {
		return false
	}
	if validateCompiledGCFrameRoots(c, roots) != nil {
		return false
	}
	rootedAbstract := func(heap AbstractHeapType) (wasm.AbsHeapType, bool) {
		switch heap {
		case AbstractHeapNone:
			return wasm.HeapNone, true
		case AbstractHeapAny:
			return wasm.HeapAny, true
		case AbstractHeapEq:
			return wasm.HeapEq, true
		case AbstractHeapStruct:
			return wasm.HeapStruct, true
		case AbstractHeapArray:
			return wasm.HeapArray, true
		default:
			return 0, false
		}
	}
	rootedValue := func(value ValueTypeDescriptor, owner int) bool {
		if value.Kind <= ValueTypeF64 {
			return value.Ref == (ReferenceTypeDescriptor{})
		}
		if value.Kind != ValueTypeReference || value.Ref.Exact {
			return false
		}
		if !value.Ref.Heap.Defined {
			_, ok := rootedAbstract(value.Ref.Heap.Abstract)
			return ok && value.Ref.Heap.TypeIndex == 0
		}
		if int(value.Ref.Heap.TypeIndex) >= owner || int(value.Ref.Heap.TypeIndex) >= len(c.Types) {
			return false
		}
		target := c.Types[value.Ref.Heap.TypeIndex]
		return target.Kind == CompositeTypeStruct || target.Kind == CompositeTypeArray
	}
	storageKind := func(storage StorageTypeDescriptor, owner int) (gc.StorageKind, bool) {
		if storage.Packed {
			switch storage.PackedType {
			case PackedTypeI8:
				return gc.StorageI8, true
			case PackedTypeI16:
				return gc.StorageI16, true
			default:
				return 0, false
			}
		}
		if storage.Value.Kind == ValueTypeReference {
			ref := storage.Value.Ref
			if ref.Exact || !ref.Heap.Defined || int(ref.Heap.TypeIndex) >= owner || int(ref.Heap.TypeIndex) >= len(c.Types) {
				return 0, false
			}
			target := c.Types[ref.Heap.TypeIndex]
			if target.Kind != CompositeTypeStruct && target.Kind != CompositeTypeArray {
				return 0, false
			}
			if ref.Nullable {
				return gc.StorageRefNull, true
			}
			return gc.StorageRef, true
		}
		if storage.Value.Ref != (ReferenceTypeDescriptor{}) {
			return 0, false
		}
		switch storage.Value.Kind {
		case ValueTypeI32:
			return gc.StorageI32, true
		case ValueTypeI64:
			return gc.StorageI64, true
		case ValueTypeF32:
			return gc.StorageF32, true
		case ValueTypeF64:
			return gc.StorageF64, true
		default:
			return 0, false
		}
	}
	hasStruct, hasArray := false, false
	requiredSlots := 4
	if arrayHelpers {
		requiredSlots = rv64GenericArrayFixedSlots
	}
	for i, desc := range c.GCTypeDescs {
		typ := &c.Types[i]
		if typ.RecGroup != uint32(i) || !typ.Final || len(typ.Supers) != 0 || typ.HasDescribes || typ.HasDescriptor {
			return false
		}
		switch desc.Kind {
		case gc.KindFunc:
			if typ.Kind != CompositeTypeFunction || len(typ.Params) > 256 || len(typ.Results) > 256 || !desc.Final || desc.HasSuper || len(desc.Fields) != 0 || desc.Elem != 0 || desc.HasRefs {
				return false
			}
			for _, values := range [][]ValueTypeDescriptor{typ.Params, typ.Results} {
				for _, value := range values {
					if !rootedValue(value, i) {
						return false
					}
				}
			}
		case gc.KindStruct:
			if typ.Kind != CompositeTypeStruct || len(typ.Fields) == 0 || len(typ.Fields) > 255 || len(typ.Fields) != len(desc.Fields) ||
				!desc.Final || desc.HasSuper {
				return false
			}
			kinds := make([]gc.StorageKind, len(typ.Fields))
			for field := range typ.Fields {
				kind, ok := storageKind(typ.Fields[field].Storage, i)
				if !ok {
					return false
				}
				kinds[field] = kind
			}
			expected, err := gc.NewStructDesc(gc.TypeID(i), kinds)
			if err != nil || desc.ID != expected.ID || desc.Kind != expected.Kind || desc.Elem != expected.Elem || desc.Size != expected.Size ||
				desc.ElemSize != expected.ElemSize || desc.Align != expected.Align || desc.HasRefs != expected.HasRefs || desc.Final != expected.Final ||
				desc.Super != expected.Super || desc.HasSuper != expected.HasSuper || !slices.Equal(desc.Fields, expected.Fields) {
				return false
			}
			requiredSlots = max(requiredSlots, len(typ.Fields)+1)
			hasStruct = true
		case gc.KindArray:
			if typ.Kind != CompositeTypeArray || !desc.Final || desc.HasSuper || len(desc.Fields) != 0 {
				return false
			}
			kind, ok := storageKind(typ.Array.Storage, i)
			if !ok {
				return false
			}
			expected, err := gc.NewArrayDesc(gc.TypeID(i), kind)
			if err != nil || desc.ID != expected.ID || desc.Kind != expected.Kind || desc.Elem != expected.Elem || desc.Size != expected.Size ||
				desc.ElemSize != expected.ElemSize || desc.Align != expected.Align || desc.HasRefs != expected.HasRefs || desc.Final != expected.Final ||
				desc.Super != expected.Super || desc.HasSuper != expected.HasSuper {
				return false
			}
			hasArray = true
		default:
			return false
		}
	}
	if len(c.Funcs) != len(c.FuncTypeID) {
		return false
	}
	structural := &wasm.Module{Types: make([]wasm.RecType, len(c.Types))}
	toWasmValue := func(value ValueTypeDescriptor) (wasm.ValType, bool) {
		switch value.Kind {
		case ValueTypeI32:
			return wasm.I32, true
		case ValueTypeI64:
			return wasm.I64, true
		case ValueTypeF32:
			return wasm.F32, true
		case ValueTypeF64:
			return wasm.F64, true
		case ValueTypeReference:
			if value.Ref.Heap.Defined {
				return wasm.RefVal(wasm.Ref(value.Ref.Nullable, wasm.IndexedHeap(wasm.TypeIdx{Index: value.Ref.Heap.TypeIndex}), value.Ref.Exact)), true
			}
			heap, ok := rootedAbstract(value.Ref.Heap.Abstract)
			if !ok || value.Ref.Heap.TypeIndex != 0 || value.Ref.Exact {
				return wasm.ValType{}, false
			}
			return wasm.RefVal(wasm.Ref(value.Ref.Nullable, wasm.AbsHeap(heap), false)), true
		default:
			return wasm.ValType{}, false
		}
	}
	toWasmField := func(field FieldTypeDescriptor) (wasm.FieldType, bool) {
		var storage wasm.StorageType
		if field.Storage.Packed {
			switch field.Storage.PackedType {
			case PackedTypeI8:
				storage = wasm.StoragePacked(wasm.PackI8)
			case PackedTypeI16:
				storage = wasm.StoragePacked(wasm.PackI16)
			default:
				return wasm.FieldType{}, false
			}
		} else {
			value, ok := toWasmValue(field.Storage.Value)
			if !ok {
				return wasm.FieldType{}, false
			}
			storage = wasm.StorageVal(value)
		}
		mut := wasm.Const
		if field.Mutable {
			mut = wasm.Var
		}
		return wasm.NewFieldType(storage, mut), true
	}
	for i := range c.Types {
		typ := &c.Types[i]
		comp := wasm.CompType{}
		switch typ.Kind {
		case CompositeTypeFunction:
			comp.Kind = wasm.CompFunc
			comp.Params = make([]wasm.ValType, len(typ.Params))
			comp.Results = make([]wasm.ValType, len(typ.Results))
			for j := range typ.Params {
				var ok bool
				comp.Params[j], ok = toWasmValue(typ.Params[j])
				if !ok {
					return false
				}
			}
			for j := range typ.Results {
				var ok bool
				comp.Results[j], ok = toWasmValue(typ.Results[j])
				if !ok {
					return false
				}
			}
		case CompositeTypeStruct:
			comp.Kind = wasm.CompStruct
			comp.Fields = make([]wasm.FieldType, len(typ.Fields))
			for j := range typ.Fields {
				var ok bool
				comp.Fields[j], ok = toWasmField(typ.Fields[j])
				if !ok {
					return false
				}
			}
		case CompositeTypeArray:
			comp.Kind = wasm.CompArray
			var ok bool
			comp.Array, ok = toWasmField(typ.Array)
			if !ok {
				return false
			}
		default:
			return false
		}
		structural.Types[i] = wasm.RecType{SubTypes: []wasm.SubType{{Final: true, Comp: comp}}}
	}
	for function := range c.Funcs {
		sig := &c.Funcs[function]
		if !sig.HasTypeIndex || int(sig.TypeIndex) >= len(c.Types) {
			return false
		}
		typ := &c.Types[sig.TypeIndex]
		if typ.Kind != CompositeTypeFunction || len(sig.Params) != len(typ.Params) || len(sig.Results) != len(typ.Results) {
			return false
		}
		key, ok := structural.StructuralTypeKeyChecked(sig.TypeIndex)
		if !ok || c.FuncTypeID[function] != key {
			return false
		}
		for i, exact := range typ.Params {
			abi, ok := exact.ABIType(c.Types)
			if !ok || sig.Params[i] != abi {
				return false
			}
		}
		for i, exact := range typ.Results {
			abi, ok := exact.ABIType(c.Types)
			if !ok || sig.Results[i] != abi {
				return false
			}
		}
	}
	for _, function := range c.Exports {
		if function < 0 || function >= len(c.Funcs) {
			return false
		}
		typ := &c.Types[c.Funcs[function].TypeIndex]
		for _, values := range [][]ValueTypeDescriptor{typ.Params, typ.Results} {
			for _, value := range values {
				if value.Kind > ValueTypeF64 || value.Ref != (ReferenceTypeDescriptor{}) {
					return false
				}
			}
		}
	}
	// The struct-dispatch helper family also owns dynamic ref.test/ref.cast for
	// both defined aggregate kinds. An array-only rooted module can therefore
	// require it without declaring a struct descriptor. The fresh compiler
	// receipt and code digest authenticate the actual helper callsites.
	return (!structHelpers || hasStruct || hasArray) && (!arrayHelpers || hasArray) && int(c.syncHostSlots) >= requiredSlots
}

// rv64StateOnlyExternrefGlobals is the persisted-artifact witness for a
// bounded directory containing at least one immutable null externref global
// plus optional immutable literal scalar globals in an otherwise empty module.
// It must remain declaration-only: executable reference carriers and every
// imported or initializer-dependent owner path require separate qualification.
func rv64StateOnlyExternrefGlobals(c *Compiled) bool {
	if c == nil || !c.hasNoFunctionCode() || c.NumImports != 0 || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Types) != 0 || len(c.GCTypeDescs) != 0 || len(c.FuncTypeID) != 0 || len(c.Exports) != 0 {
		return false
	}
	if len(c.Globals) == 0 || len(c.Globals) > 256 || len(c.GlobalImports) != 0 || len(c.GlobalExports) > 256 || len(c.ValueTypes) == 0 || len(c.ValueTypes) > 5 || c.tableCount() != 0 || c.memoryCount() != 0 || c.tagImportCount() != 0 || c.HasStart {
		return false
	}
	if c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 {
		return false
	}
	for _, index := range c.GlobalExports {
		if index < 0 || index >= len(c.Globals) {
			return false
		}
	}
	plainExternref, _ := valueTypeDescriptorFromValType(ValExternRef)
	var kinds, used uint8
	for _, exact := range c.ValueTypes {
		if exact.Kind > ValueTypeF64 && exact != plainExternref || exact.Kind <= ValueTypeF64 && exact.Ref != (ReferenceTypeDescriptor{}) || kinds&(1<<exact.Kind) != 0 {
			return false
		}
		kinds |= 1 << exact.Kind
	}
	required := CoreFeatureReferenceTypes
	hasExternref := false
	for i, g := range c.Globals {
		exact, err := c.globalExactType(i)
		plain, ok := valueTypeDescriptorFromValType(g.Type)
		if err != nil || !ok || exact != plain || !g.HasValueType || uint64(g.ValueTypeIndex) >= uint64(len(c.ValueTypes)) || g.Mutable || g.V128 != (V128{}) || g.HasInitFunc || g.InitFunc != 0 || len(g.InitExpr) != 0 {
			return false
		}
		used |= 1 << g.ValueTypeIndex
		if g.Type != ValExternRef {
			if g.Type != ValI32 && g.Type != ValI64 && g.Type != ValF32 && g.Type != ValF64 || g.HasInitGlobal || g.InitGlobal != 0 || (g.Type == ValI32 || g.Type == ValF32) && g.Bits>>32 != 0 {
				return false
			}
			continue
		}
		hasExternref = true
		if exact != plainExternref || g.Bits != 0 {
			return false
		}
		if g.HasInitGlobal {
			if g.InitGlobal < 0 || g.InitGlobal >= i || c.Globals[g.InitGlobal].Type != ValExternRef {
				return false
			}
			required |= CoreFeatureExtendedConstExpressions
		} else if g.InitGlobal != 0 {
			return false
		}
	}
	return hasExternref && used == uint8((1<<len(c.ValueTypes))-1) && c.requiredFeatures == required && compiledStructuralRequiredFeatures(c) == required
}

// rv64StateOnlyMutableFuncrefImport is the persisted-artifact witness for the
// first mutable funcref-global boundary. It admits exactly one store-bound,
// nullable funcref cell which is imported and re-exported without executable
// code. Executable reads, the exact null-only write, and broader mixed state use
// separate admission and lowering.
func rv64StateOnlyMutableFuncrefImport(c *Compiled) bool {
	if c == nil || !c.hasNoFunctionCode() || c.NumImports != 0 || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Types) != 0 || len(c.GCTypeDescs) != 0 || len(c.FuncTypeID) != 0 || len(c.Funcs) != 0 || len(c.Exports) != 0 {
		return false
	}
	if len(c.Globals) != 1 || len(c.GlobalImports) != 1 || len(c.GlobalExports) != 1 || len(c.ValueTypes) != 1 || c.tableCount() != 0 || len(c.tableExports) != 0 || c.memoryCount() != 0 || len(c.memoryExportMap()) != 0 || c.tagImportCount() != 0 || c.HasStart {
		return false
	}
	if c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || len(c.customInstructions) != 0 {
		return false
	}
	for _, index := range c.GlobalExports {
		if index != 0 {
			return false
		}
	}
	plain, _ := valueTypeDescriptorFromValType(ValFuncRef)
	imp, global := c.GlobalImports[0], c.Globals[0]
	exact, err := c.globalExactType(0)
	if err != nil || exact != plain || c.ValueTypes[0] != plain || !imp.HasValueType || imp.ValueTypeIndex != 0 || imp.Type != ValFuncRef || !imp.Mutable {
		return false
	}
	if global.Type != ValFuncRef || !global.Mutable || !global.HasValueType || global.ValueTypeIndex != 0 || global.HasInitFunc || global.InitFunc != 0 || global.HasInitGlobal || global.InitGlobal != 0 || len(global.InitExpr) != 0 || global.Bits != 0 || global.V128 != (V128{}) {
		return false
	}
	required := CoreFeatureReferenceTypes | CoreFeatureMutableGlobal
	return c.requiredFeatures == required && compiledStructuralRequiredFeatures(c) == required
}

// Loading and hand-built metadata must enforce the same bounded public ABI as
// source compilation before any executable image or host thunk is published.
func railshotValidateCompiled(c *Compiled) error {
	reject := func(detail string) error { return fmt.Errorf("%w: public RV64 %s", railshot.ErrUnsupported, detail) }
	stateOnlyMutableFuncrefImport := rv64StateOnlyMutableFuncrefImport(c)
	mutableFuncrefGlobalGet := rv64MutableFuncrefGlobalGet(c)
	mutableFuncrefGlobalClear := rv64MutableFuncrefGlobalClear(c)
	mutableFuncrefGlobalStoreSelf := rv64MutableFuncrefGlobalStoreSelf(c)
	mutableFuncrefGlobalCopy := rv64MutableFuncrefGlobalCopy(c)
	mutableFuncrefGlobalMove := rv64MutableFuncrefGlobalMove(c)
	mutableFuncrefGlobalBidirectionalCopy := rv64MutableFuncrefGlobalBidirectionalCopy(c)
	mutableFuncrefGlobalLocalCopy := rv64MutableFuncrefGlobalLocalCopy(c)
	mutableFuncrefGlobalLocalPublish := rv64MutableFuncrefGlobalLocalPublish(c)
	mutableFuncrefGlobalLocalTransfer := rv64MutableFuncrefGlobalLocalTransfer(c)
	mutableFuncrefGlobalLocalExport := rv64MutableFuncrefGlobalLocalExport(c)
	mutableFuncrefGlobalLocalObserve := rv64MutableFuncrefGlobalLocalObserve(c)
	mutableFuncrefGlobalLocalCall := rv64MutableFuncrefGlobalLocalCall(c)
	freshRootedGC := rv64FreshRootedGC(c)
	if rv64MutableFuncrefGlobalGetExecutionCode(c) && !mutableFuncrefGlobalGet {
		return reject("mutable funcref global.get image requires its exact retained metadata")
	}
	if rv64MutableFuncrefGlobalClearExecutionCode(c) && !mutableFuncrefGlobalClear {
		return reject("mutable funcref global.clear image requires its exact retained metadata")
	}
	if rv64MutableFuncrefGlobalStoreSelfExecutionCode(c) && !mutableFuncrefGlobalStoreSelf {
		return reject("mutable funcref self-store image requires its exact retained metadata")
	}
	if rv64MutableFuncrefGlobalCopyExecutionCode(c) && !mutableFuncrefGlobalCopy {
		return reject("mutable funcref copy image requires its exact retained metadata")
	}
	if rv64MutableFuncrefGlobalMoveExecutionCode(c) && !mutableFuncrefGlobalMove {
		return reject("mutable funcref move image requires its exact retained metadata")
	}
	if rv64MutableFuncrefGlobalBidirectionalCopyExecutionCode(c) && !mutableFuncrefGlobalBidirectionalCopy {
		return reject("mutable funcref bidirectional-copy image requires its exact retained metadata")
	}
	if rv64MutableFuncrefGlobalLocalCopyExecutionCode(c) && !mutableFuncrefGlobalLocalCopy {
		return reject("mutable funcref local-copy image requires its exact retained metadata")
	}
	if rv64MutableFuncrefGlobalLocalPublishExecutionCode(c) && !mutableFuncrefGlobalLocalPublish {
		return reject("mutable funcref local-publish image requires its exact retained metadata")
	}
	if rv64MutableFuncrefGlobalLocalTransferExecutionCode(c) && !mutableFuncrefGlobalLocalTransfer {
		return reject("mutable funcref local-transfer image requires its exact retained metadata")
	}
	if rv64MutableFuncrefGlobalLocalExportExecutionCode(c) && !mutableFuncrefGlobalLocalExport {
		return reject("mutable funcref local-export image requires its exact retained metadata")
	}
	if rv64MutableFuncrefGlobalLocalObserveExecutionCode(c) && !mutableFuncrefGlobalLocalObserve {
		return reject("mutable funcref local-observe image requires its exact retained metadata")
	}
	if rv64MutableFuncrefGlobalLocalCallExecutionCode(c) && !mutableFuncrefGlobalLocalCall {
		return reject("mutable funcref local-call image requires its exact retained metadata")
	}
	exactGCRefEq := rv64OfficialGCRefEq(c)
	exactGCRefTestTable := rv64GCRefTestTable(c)
	exactGCRefTestConcrete := rv64GCRefTestConcrete(c)
	exactGCRefCastConcrete := rv64GCRefCastConcrete(c)
	exactGCRefTestAbstract := rv64GCRefTestAbstract(c)
	exactGCExtern := rv64GCExtern(c)
	exactGCRefCastAbstract := rv64GCRefCastAbstract(c)
	exactGCBrOnCastAbstract := rv64GCBrOnCastAbstract(c)
	exactGCBrOnCastFailAbstract := rv64GCBrOnCastFailAbstract(c)
	exactGCBrOnCastConcrete := rv64GCBrOnCastConcrete(c)
	exactGCBrOnCastFailConcrete := rv64GCBrOnCastFailConcrete(c)
	exactGCBrOnCastNullability := rv64GCBrOnCastNullability(c)
	exactGCBrOnCastFailNullability := rv64GCBrOnCastFailNullability(c)
	exactGCTypeSubtypingDeclarations := rv64GCTypeSubtypingDeclarations(c)
	exactGCTypeSubtypingRecursiveFunctions := rv64GCTypeSubtypingRecursiveFunctions(c)
	exactGCTypeSubtypingRefFuncGlobals := rv64GCTypeSubtypingRefFuncGlobals(c)
	exactGCTypeSubtypingRuntimeFinalityCallCast := rv64GCTypeSubtypingRuntimeFinalityCallCast(c)
	exactGCTypeSubtypingRuntimeTypedTableCall := rv64GCTypeSubtypingRuntimeTypedTableCall(c)
	exactGCTypeSubtypingRefTestSingle := rv64GCTypeSubtypingRefTestSingle(c)
	exactGCTypeSubtypingLink := rv64GCTypeSubtypingLink(c)
	exactGCTypeSubtypingFinalityLink := rv64GCTypeSubtypingFinalityLink(c)
	exactGCTypeSubtypingStructLink := rv64GCTypeSubtypingStructLink(c)
	exactGCTypeSubtypingStructProjectionLink := rv64GCTypeSubtypingStructProjectionLink(c)
	exactGCTypeSubtypingStructMismatchLink := rv64GCTypeSubtypingStructMismatchLink(c)
	exactGCTypeSubtypingIndependentStructLink := rv64GCTypeSubtypingIndependentStructLink(c)
	exactGCTypeSubtypingExtendedProjectionLink := rv64GCTypeSubtypingExtendedProjectionLink(c)
	exactGCTypeSubtypingDuplicateRecursiveLink := rv64GCTypeSubtypingDuplicateRecursiveLink(c)
	exactGCTypeSubtypingExtendedRecursiveLink := rv64GCTypeSubtypingExtendedRecursiveLink(c)
	exactGCTypeSubtypingCrossGroupMismatchLink := rv64GCTypeSubtypingCrossGroupMismatchLink(c)
	exactGCTypeSubtypingTransitiveMismatchLink := rv64GCTypeSubtypingTransitiveMismatchLink(c)
	exactGCTypeSubtypingNonFlatExport := rv64GCTypeSubtypingNonFlatExport(c)
	exactGCTypeSubtypingRuntimeCallCast := rv64GCTypeSubtypingRuntimeCallCast(c) || exactGCTypeSubtypingRuntimeFinalityCallCast || exactGCTypeSubtypingRuntimeTypedTableCall
	exactGCStructGet := rv64GCStructGet(c)
	exactGCStructMutation := rv64GCStructMutation(c)
	exactGCStructNull := rv64GCStructNull(c)
	exactGCStructPacked := rv64GCStructPacked(c)
	exactGCStructBasic := rv64GCStructBasic(c)
	exactGCStructNumericGlobals := rv64GCStructNumericGlobals(c)
	exactGCArrayLocal := rv64GCArrayLocal(c)
	exactGCArrayUniform := rv64GCArrayUniform(c)
	exactGCArrayFixedLocal := rv64GCArrayFixedLocal(c)
	exactGCArrayFixedOfficial := rv64GCArrayFixedOfficial(c)
	exactGCArrayDefaultOfficial := rv64GCArrayDefaultOfficial(c)
	exactGCArrayPackedData := rv64GCArrayPackedData(c)
	exactGCArrayReference := rv64GCArrayReference(c)
	exactGCArrayInitElem := rv64GCArrayInitElem(c)
	exactGCArrayInitData := rv64GCArrayInitData(c)
	exactGCArrayFill := rv64GCArrayFill(c)
	exactGCArrayCopy := rv64GCArrayCopy(c)
	if rv64GCTypeSubtypingRecursiveFunctionsExecutionCode(c) && !exactGCTypeSubtypingRecursiveFunctions {
		return reject("gc/type-subtyping recursive-function image requires its exact retained metadata")
	}
	if rv64GCTypeSubtypingRecursiveFunctionsProfileWithCode(c, true, false) != 0 && !exactGCTypeSubtypingRecursiveFunctions {
		return reject("gc/type-subtyping recursive-function metadata requires its exact retained image")
	}
	if rv64GCTypeSubtypingRefFuncGlobalsExecutionCode(c) && rv64GCTypeSubtypingRefFuncGlobalsExecutionClaim(c) && !exactGCTypeSubtypingRefFuncGlobals && !exactGCTypeSubtypingStructLink && !exactGCTypeSubtypingStructProjectionLink && !exactGCTypeSubtypingStructMismatchLink && !exactGCTypeSubtypingIndependentStructLink && !exactGCTypeSubtypingExtendedProjectionLink && !exactGCTypeSubtypingDuplicateRecursiveLink && !exactGCTypeSubtypingExtendedRecursiveLink && !exactGCTypeSubtypingCrossGroupMismatchLink && !exactGCTypeSubtypingTransitiveMismatchLink {
		return reject("gc/type-subtyping ref.func-global image requires its exact retained metadata")
	}
	if rv64GCTypeSubtypingRefFuncGlobalsProfileWithCode(c, true, false) != 0 && !exactGCTypeSubtypingRefFuncGlobals {
		return reject("gc/type-subtyping ref.func-global metadata requires its exact retained image")
	}
	if rv64GCTypeSubtypingRuntimeCallCastExecutionCode(c) && !exactGCTypeSubtypingRuntimeCallCast {
		return reject("gc/type-subtyping runtime call/cast image requires its exact retained metadata")
	}
	if rv64GCTypeSubtypingRuntimeCallCastProfileWithCode(c, true, false) && !exactGCTypeSubtypingRuntimeCallCast {
		return reject("gc/type-subtyping runtime call/cast metadata requires its exact retained image")
	}
	if rv64GCTypeSubtypingRuntimeFinalityCallCastExecutionCode(c) && !exactGCTypeSubtypingRuntimeFinalityCallCast {
		return reject("gc/type-subtyping runtime finality call/cast image requires its exact retained metadata")
	}
	if rv64GCTypeSubtypingRuntimeFinalityCallCastProfileWithCode(c, true, false) && !exactGCTypeSubtypingRuntimeFinalityCallCast {
		return reject("gc/type-subtyping runtime finality call/cast metadata requires its exact retained image")
	}
	if rv64GCTypeSubtypingRuntimeTypedTableCallExecutionCode(c) && !exactGCTypeSubtypingRuntimeTypedTableCall {
		return reject("gc/type-subtyping runtime typed-table call image requires its exact retained metadata")
	}
	if rv64GCTypeSubtypingRuntimeTypedTableCallProfileWithCode(c, true, false) && !exactGCTypeSubtypingRuntimeTypedTableCall {
		return reject("gc/type-subtyping runtime typed-table call metadata requires its exact retained image")
	}
	if rv64GCTypeSubtypingRefTestSingleExecutionCode(c) && !exactGCTypeSubtypingRefTestSingle {
		return reject("gc/type-subtyping ref.test image requires its exact retained metadata")
	}
	if rv64GCTypeSubtypingRefTestSingleProfileWithCode(c, true, false) && !exactGCTypeSubtypingRefTestSingle {
		return reject("gc/type-subtyping ref.test metadata requires its exact retained image")
	}
	if rv64GCTypeSubtypingLinkExecutionCode(c) && rv64GCTypeSubtypingLinkProfileWithCode(c, true, false) != 0 && !exactGCTypeSubtypingLink {
		return reject("gc/type-subtyping link image requires its exact retained metadata")
	}
	if rv64GCTypeSubtypingLinkProfileWithCode(c, true, false) != 0 && !exactGCTypeSubtypingLink {
		return reject("gc/type-subtyping link metadata requires its exact retained image")
	}
	if rv64GCTypeSubtypingFinalityLinkExecutionCode(c) && rv64GCTypeSubtypingFinalityLinkProfileWithCode(c, true, false) != 0 && !exactGCTypeSubtypingFinalityLink {
		return reject("gc/type-subtyping finality-link image requires its exact retained metadata")
	}
	if rv64GCTypeSubtypingFinalityLinkProfileWithCode(c, true, false) != 0 && !exactGCTypeSubtypingFinalityLink {
		return reject("gc/type-subtyping finality-link metadata requires its exact retained image")
	}
	if rv64GCTypeSubtypingStructLinkExecutionCode(c) && rv64GCTypeSubtypingStructLinkProfileWithCode(c, true, false) != 0 && !exactGCTypeSubtypingStructLink {
		return reject("gc/type-subtyping struct-link image requires its exact retained metadata")
	}
	if rv64GCTypeSubtypingStructLinkProfileWithCode(c, true, false) != 0 && !exactGCTypeSubtypingStructLink {
		return reject("gc/type-subtyping struct-link metadata requires its exact retained image")
	}
	if rv64GCTypeSubtypingStructProjectionLinkExecutionCode(c) && rv64GCTypeSubtypingStructProjectionLinkProfileWithCode(c, true, false) != 0 && !exactGCTypeSubtypingStructProjectionLink {
		return reject("gc/type-subtyping struct-projection-link image requires its exact retained metadata")
	}
	if rv64GCTypeSubtypingStructProjectionLinkProfileWithCode(c, true, false) != 0 && !exactGCTypeSubtypingStructProjectionLink {
		return reject("gc/type-subtyping struct-projection-link metadata requires its exact retained image")
	}
	if rv64GCTypeSubtypingStructMismatchLinkExecutionCode(c) && rv64GCTypeSubtypingStructMismatchLinkProfileWithCode(c, true, false) != 0 && !exactGCTypeSubtypingStructMismatchLink {
		return reject("gc/type-subtyping struct-mismatch-link image requires its exact retained metadata")
	}
	if rv64GCTypeSubtypingStructMismatchLinkProfileWithCode(c, true, false) != 0 && !exactGCTypeSubtypingStructMismatchLink {
		return reject("gc/type-subtyping struct-mismatch-link metadata requires its exact retained image")
	}
	if rv64GCTypeSubtypingIndependentStructLinkExecutionCode(c) && rv64GCTypeSubtypingIndependentStructLinkProfileWithCode(c, true, false) != 0 && !exactGCTypeSubtypingIndependentStructLink {
		return reject("gc/type-subtyping independent-struct-link image requires its exact retained metadata")
	}
	if rv64GCTypeSubtypingIndependentStructLinkProfileWithCode(c, true, false) != 0 && !exactGCTypeSubtypingIndependentStructLink {
		return reject("gc/type-subtyping independent-struct-link metadata requires its exact retained image")
	}
	if rv64GCTypeSubtypingExtendedProjectionLinkExecutionCode(c) && rv64GCTypeSubtypingExtendedProjectionLinkProfileWithCode(c, true, false) != 0 && !exactGCTypeSubtypingExtendedProjectionLink {
		return reject("gc/type-subtyping extended-projection-link image requires its exact retained metadata")
	}
	if rv64GCTypeSubtypingExtendedProjectionLinkProfileWithCode(c, true, false) != 0 && !exactGCTypeSubtypingExtendedProjectionLink {
		return reject("gc/type-subtyping extended-projection-link metadata requires its exact retained image")
	}
	if rv64GCTypeSubtypingDuplicateRecursiveLinkExecutionCode(c) && rv64GCTypeSubtypingDuplicateRecursiveLinkProfileWithCode(c, true, false) != 0 && !exactGCTypeSubtypingDuplicateRecursiveLink {
		return reject("gc/type-subtyping duplicate-recursive-link image requires its exact retained metadata")
	}
	if rv64GCTypeSubtypingDuplicateRecursiveLinkProfileWithCode(c, true, false) != 0 && !exactGCTypeSubtypingDuplicateRecursiveLink {
		return reject("gc/type-subtyping duplicate-recursive-link metadata requires its exact retained image")
	}
	if rv64GCTypeSubtypingExtendedRecursiveLinkExecutionCode(c) && rv64GCTypeSubtypingExtendedRecursiveLinkProfileWithCode(c, true, false) != 0 && !exactGCTypeSubtypingExtendedRecursiveLink {
		return reject("gc/type-subtyping extended-recursive-link image requires its exact retained metadata")
	}
	if rv64GCTypeSubtypingExtendedRecursiveLinkProfileWithCode(c, true, false) != 0 && !exactGCTypeSubtypingExtendedRecursiveLink {
		return reject("gc/type-subtyping extended-recursive-link metadata requires its exact retained image")
	}
	if rv64GCTypeSubtypingCrossGroupMismatchLinkExecutionCode(c) && rv64GCTypeSubtypingCrossGroupMismatchLinkProfileWithCode(c, true, false) != 0 && !exactGCTypeSubtypingCrossGroupMismatchLink {
		return reject("gc/type-subtyping cross-group-mismatch-link image requires its exact retained metadata")
	}
	if rv64GCTypeSubtypingCrossGroupMismatchLinkProfileWithCode(c, true, false) != 0 && !exactGCTypeSubtypingCrossGroupMismatchLink {
		return reject("gc/type-subtyping cross-group-mismatch-link metadata requires its exact retained image")
	}
	if rv64GCTypeSubtypingTransitiveMismatchLinkExecutionCode(c) && rv64GCTypeSubtypingTransitiveMismatchLinkProfileWithCode(c, true, false) != 0 && !exactGCTypeSubtypingTransitiveMismatchLink {
		return reject("gc/type-subtyping transitive-mismatch-link image requires its exact retained metadata")
	}
	if rv64GCTypeSubtypingTransitiveMismatchLinkProfileWithCode(c, true, false) != 0 && !exactGCTypeSubtypingTransitiveMismatchLink {
		return reject("gc/type-subtyping transitive-mismatch-link metadata requires its exact retained image")
	}
	if rv64GCTypeSubtypingNonFlatExportExecutionCode(c) && !exactGCTypeSubtypingNonFlatExport {
		return reject("gc/type-subtyping non-flat-export image requires its exact retained metadata")
	}
	if rv64GCTypeSubtypingNonFlatExportProfileWithCode(c, true, false) && !exactGCTypeSubtypingNonFlatExport {
		return reject("gc/type-subtyping non-flat-export metadata requires its exact retained image")
	}
	if rv64OfficialGCRefEqExecutionCode(c) && !exactGCRefEq {
		return reject("allocating gc/ref.eq image requires its exact retained metadata")
	}
	if rv64GCRefTestTableExecutionCode(c) && !exactGCRefTestTable {
		return reject("gc object-table ref.test image requires its exact retained metadata")
	}
	if rv64GCRefTestConcreteExecutionCode(c) && !exactGCRefTestConcrete {
		return reject("gc concrete ref.test image requires its exact retained metadata")
	}
	if rv64GCRefCastConcreteExecutionCode(c) && !exactGCRefCastConcrete {
		return reject("gc concrete ref.cast image requires its exact retained metadata")
	}
	if rv64GCRefTestAbstractExecutionCode(c) && !exactGCRefTestAbstract {
		return reject("gc abstract ref.test image requires its exact retained metadata")
	}
	if rv64GCExternExecutionCode(c) && !exactGCExtern {
		return reject("gc extern conversion image requires its exact retained metadata")
	}
	if rv64GCRefCastAbstractExecutionCode(c) && !exactGCRefCastAbstract {
		return reject("gc abstract ref.cast image requires its exact retained metadata")
	}
	if rv64GCBrOnCastAbstractExecutionCode(c) && !exactGCBrOnCastAbstract {
		return reject("gc abstract br_on_cast image requires its exact retained metadata")
	}
	if rv64GCBrOnCastFailAbstractExecutionCode(c) && !exactGCBrOnCastFailAbstract {
		return reject("gc abstract br_on_cast_fail image requires its exact retained metadata")
	}
	if rv64GCBrOnCastConcreteExecutionCode(c) && !exactGCBrOnCastConcrete {
		return reject("gc concrete br_on_cast image requires its exact retained metadata")
	}
	if rv64GCBrOnCastFailConcreteExecutionCode(c) && !exactGCBrOnCastFailConcrete {
		return reject("gc concrete br_on_cast_fail image requires its exact retained metadata")
	}
	if rv64GCBrOnCastNullabilityExecutionCode(c) && !exactGCBrOnCastNullability {
		return reject("gc nullability br_on_cast image requires its exact retained metadata")
	}
	if rv64GCBrOnCastFailNullabilityExecutionCode(c) && !exactGCBrOnCastFailNullability {
		return reject("gc nullability br_on_cast_fail image requires its exact retained metadata")
	}
	if rv64GCStructGetExecutionCode(c) && !exactGCStructGet {
		return reject("allocating gc/struct.get image requires its exact retained metadata")
	}
	if rv64GCStructMutationExecutionCode(c) && !exactGCStructMutation {
		return reject("allocating gc/struct mutation image requires its exact retained metadata")
	}
	if rv64GCStructNullExecutionCode(c) && !exactGCStructNull {
		return reject("gc/struct null image requires its exact retained metadata")
	}
	if rv64GCStructPackedExecutionCode(c) && !exactGCStructPacked {
		return reject("gc/struct packed image requires its exact retained metadata")
	}
	if rv64GCStructBasicExecutionCode(c) && !exactGCStructBasic {
		return reject("gc/struct basic image requires its exact retained metadata")
	}
	if rv64GCArrayLocalExecutionCode(c) && !exactGCArrayLocal {
		return reject("allocating gc/array local image requires its exact retained metadata")
	}
	if rv64GCArrayUniformExecutionCode(c) && !exactGCArrayUniform {
		return reject("allocating gc/array uniform image requires its exact retained metadata")
	}
	if rv64GCArrayFixedLocalExecutionCode(c) && !exactGCArrayFixedLocal {
		return reject("allocating gc/array fixed local image requires its exact retained metadata")
	}
	if rv64GCArrayFixedOfficialExecutionCode(c) && !exactGCArrayFixedOfficial {
		return reject("allocating official gc/array fixed image requires its exact retained metadata")
	}
	if rv64GCArrayDefaultOfficialExecutionCode(c) && !exactGCArrayDefaultOfficial {
		return reject("allocating official gc/array default image requires its exact retained metadata")
	}
	if rv64GCArrayPackedDataExecutionCode(c) && !exactGCArrayPackedData {
		return reject("allocating gc/array packed data image requires its exact retained metadata")
	}
	if rv64GCArrayReferenceExecutionCode(c) && !exactGCArrayReference {
		return reject("allocating gc/array reference image requires its exact retained metadata")
	}
	if rv64GCArrayInitElemExecutionCode(c) && !exactGCArrayInitElem {
		return reject("allocating gc/array init elem image requires its exact retained metadata")
	}
	if rv64GCArrayInitDataExecutionCode(c) && !exactGCArrayInitData {
		return reject("allocating gc/array init data image requires its exact retained metadata")
	}
	if rv64GCArrayFillExecutionCode(c) && !exactGCArrayFill {
		return reject("allocating gc/array fill image requires its exact retained metadata")
	}
	if rv64GCArrayCopyExecutionCode(c) && !exactGCArrayCopy {
		return reject("allocating gc/array copy image requires its exact retained metadata")
	}
	if rv64I31CoreExecutionCode(c) && !rv64I31Core(c) {
		return reject("i31 execution image requires its exact retained metadata")
	}
	if c != nil && c.stagedGCI31Product() != 0 && !rv64I31Core(c) {
		return reject("i31 execution product requires its exact retained image")
	}
	exactAnyI31 := rv64I31OfficialAnyGlobal(c)
	typedI31 := rv64I31OfficialCore(c) || rv64I31OfficialTableGlobalInitializer(c) || rv64I31OfficialGlobalInitializer(c) || exactAnyI31
	exactCore3RefIsNull := rv64Core3RefIsNullExecution(c)
	if rv64Core3RefIsNullExecutionCode(c) && !exactCore3RefIsNull {
		return reject("Core 3 ref.is_null image requires its exact retained metadata")
	}
	if !c.nativeContextTrapABI {
		return reject("native-context trap ABI is required")
	}
	if len(c.code) > railshot.MaxCodeBytes || len(c.Funcs) > railshot.MaxFunctions || len(c.Types) > railshot.MaxFunctions || len(c.GCTypeDescs) > railshot.MaxFunctions || len(c.Globals) > 256 {
		return reject("bounded artifact shape")
	}
	if len(c.Funcs) == 0 && c.NumImports == 0 && !c.hasNoFunctionCode() {
		return reject("state-only artifact contains code or function entries")
	}
	if c.activeDataCount() > rv64runtime.MaxDataSegments || len(c.PassiveData) > rv64runtime.MaxDataSegments {
		return reject("data descriptor count bound")
	}
	remaining := railshot.MaxModuleBytes
	for i := 0; i < c.activeDataCount(); i++ {
		data := c.activeDataAt(i)
		if len(data.Bytes) > remaining {
			return reject("data payload size bound")
		}
		remaining -= len(data.Bytes)
	}
	for _, data := range c.PassiveData {
		if len(data.Bytes) > remaining {
			return reject("data payload size bound")
		}
		remaining -= len(data.Bytes)
	}
	if c.NumImports < 0 || c.NumImports > railshot.MaxHostFunctionImports || len(c.Imports) != c.NumImports || len(c.importFuncSigs) != c.NumImports || c.NumImports+len(c.Funcs) > railshot.MaxFunctions || c.dynamicImports != (c.NumImports > 0) {
		return reject("dynamic function import shape")
	}
	if c.NumImports+len(c.GlobalImports)+c.memoryImportCount()+c.tableImportCount() > railshot.MaxProductionImports {
		return reject("aggregate import bound")
	}
	if !freshRootedGC {
		if err := railshotValidateReferenceCalls(c); err != nil {
			return reject(err.Error())
		}
	}
	if err := rv64ValidateNativeTail(c); err != nil {
		return reject(err.Error())
	}
	if !freshRootedGC && !exactGCTypeSubtypingRefFuncGlobals && !exactGCTypeSubtypingLink && !exactGCTypeSubtypingFinalityLink && !exactGCTypeSubtypingStructLink && !exactGCTypeSubtypingStructProjectionLink && !exactGCTypeSubtypingStructMismatchLink && !exactGCTypeSubtypingIndependentStructLink && !exactGCTypeSubtypingExtendedProjectionLink && !exactGCTypeSubtypingDuplicateRecursiveLink && !exactGCTypeSubtypingExtendedRecursiveLink && !exactGCTypeSubtypingCrossGroupMismatchLink && !exactGCTypeSubtypingTransitiveMismatchLink {
		if err := rv64ValidateReferenceFunctions(c); err != nil {
			return reject(err.Error())
		}
	}
	if m := rv64NativeTailModule(c); m != nil {
		for _, p := range m.Plans {
			if p.Kind > 2 {
				return reject("dynamic tail requires a qualified native-owner plan")
			}
		}
	}
	vectorProfile := c.requiredFeatures.IsEnabled(CoreFeatureSIMD)
	if c.NumImports != 0 {
		if !c.needsFuncRefContextHeader {
			return reject("host imports require an instance context anchor")
		}
		maxImportArity := coreruntime.MaxHostArity
		if vectorProfile {
			maxImportArity = 128
		}
		publicHostReferenceBoundary := rv64PublicHostReferenceBoundary(c) || rv64PublicDynamicHostReferenceBoundary(c) || exactGCTypeSubtypingLink || exactGCTypeSubtypingFinalityLink || exactGCTypeSubtypingStructLink || exactGCTypeSubtypingStructProjectionLink || exactGCTypeSubtypingStructMismatchLink || exactGCTypeSubtypingIndependentStructLink || exactGCTypeSubtypingExtendedProjectionLink || exactGCTypeSubtypingDuplicateRecursiveLink || exactGCTypeSubtypingExtendedRecursiveLink || exactGCTypeSubtypingCrossGroupMismatchLink || exactGCTypeSubtypingTransitiveMismatchLink
		for _, sig := range c.importFuncSigs {
			if len(sig.Params) > maxImportArity || len(sig.Results) > maxImportArity {
				return reject("function import signature arity")
			}
			for _, values := range [][]ValType{sig.Params, sig.Results} {
				for _, typ := range values {
					if typ != ValI32 && typ != ValI64 && typ != ValF32 && typ != ValF64 && (!vectorProfile || typ != ValV128) && (!publicHostReferenceBoundary || typ != ValFuncRef && typ != ValExternRef) {
						return reject("unsupported function import signature")
					}
				}
			}
		}
	}
	if c.HasStart {
		if _, err := compiledStartIndex(c); err != nil {
			return reject(err.Error())
		}
	}

	if c.boundsMode == BoundsChecksSignalsBased || c.genericGCFrameRoots() != nil && !freshRootedGC || c.usesDynamicFuncRefTest() || c.memoryCount() > railshot.MaxProductionMemories {
		return reject("resource/host/reference lowering is pending")
	}
	if len(c.GlobalImports) > len(c.Globals) {
		return reject("global import index shape")
	}
	for i, global := range c.GlobalImports {
		reference := global.Type == ValFuncRef && (!global.Mutable || stateOnlyMutableFuncrefImport || mutableFuncrefGlobalGet || mutableFuncrefGlobalClear || mutableFuncrefGlobalStoreSelf || mutableFuncrefGlobalCopy || mutableFuncrefGlobalMove || mutableFuncrefGlobalBidirectionalCopy || mutableFuncrefGlobalLocalCopy || mutableFuncrefGlobalLocalPublish || mutableFuncrefGlobalLocalTransfer) && c.tableCount() <= 1
		if reference {
			plain, _ := valueTypeDescriptorFromValType(ValFuncRef)
			exact, err := exactValueType(global.Type, global.HasValueType, global.ValueTypeIndex, c.ValueTypes, c.Types)
			if err != nil || exact != plain {
				return reject("reference global import exact type")
			}
		}
		if (!reference && global.Type != ValI32 && global.Type != ValI64 && global.Type != ValF32 && global.Type != ValF64 && (!vectorProfile || global.Type != ValV128)) || global.Type != c.Globals[i].Type || global.Mutable != c.Globals[i].Mutable {
			return reject("global import type/mutability shape")
		}
	}
	for _, desc := range c.GCTypeDescs {
		if !freshRootedGC && !exactGCTypeSubtypingDeclarations && !exactGCTypeSubtypingRecursiveFunctions && !exactGCTypeSubtypingRefFuncGlobals && !exactGCTypeSubtypingRuntimeCallCast && !exactGCTypeSubtypingRefTestSingle && !exactGCTypeSubtypingLink && !exactGCTypeSubtypingFinalityLink && !exactGCTypeSubtypingStructLink && !exactGCTypeSubtypingStructProjectionLink && !exactGCTypeSubtypingStructMismatchLink && !exactGCTypeSubtypingIndependentStructLink && !exactGCTypeSubtypingExtendedProjectionLink && !exactGCTypeSubtypingDuplicateRecursiveLink && !exactGCTypeSubtypingExtendedRecursiveLink && !exactGCTypeSubtypingCrossGroupMismatchLink && !exactGCTypeSubtypingTransitiveMismatchLink && !exactGCTypeSubtypingNonFlatExport && !exactGCRefEq && !exactGCRefTestTable && !exactGCRefTestConcrete && !exactGCRefCastConcrete && !exactGCRefTestAbstract && !exactGCExtern && !exactGCRefCastAbstract && !exactGCBrOnCastAbstract && !exactGCBrOnCastFailAbstract && !exactGCBrOnCastConcrete && !exactGCBrOnCastFailConcrete && !exactGCBrOnCastNullability && !exactGCBrOnCastFailNullability && !exactGCStructGet && !exactGCStructMutation && !exactGCStructNull && !exactGCStructPacked && !exactGCStructBasic && !exactGCStructNumericGlobals && !exactGCArrayLocal && !exactGCArrayUniform && !exactGCArrayFixedLocal && !exactGCArrayFixedOfficial && !exactGCArrayDefaultOfficial && !exactGCArrayPackedData && !exactGCArrayReference && !exactGCArrayInitElem && !exactGCArrayInitData && !exactGCArrayFill && !exactGCArrayCopy && (desc.Kind != gc.KindFunc || !desc.Final || desc.HasSuper) {
			return reject("GC descriptor lowering is pending")
		}
	}
	qualifiedFeatures := platformCoreFeatures() | CoreFeatureTailCall
	if freshRootedGC {
		qualifiedFeatures |= CoreFeatureGC | CoreFeatureTypedFunctionReferences
	}
	if rv64I31Core(c) {
		qualifiedFeatures |= CoreFeatureGC
		if typedI31 {
			qualifiedFeatures |= CoreFeatureTypedFunctionReferences
		}
	}
	if exactGCTypeSubtypingDeclarations || exactGCTypeSubtypingRecursiveFunctions || exactGCTypeSubtypingRefFuncGlobals || exactGCTypeSubtypingRuntimeCallCast || exactGCTypeSubtypingRefTestSingle || exactGCTypeSubtypingLink || exactGCTypeSubtypingFinalityLink || exactGCTypeSubtypingStructLink || exactGCTypeSubtypingStructProjectionLink || exactGCTypeSubtypingStructMismatchLink || exactGCTypeSubtypingIndependentStructLink || exactGCTypeSubtypingExtendedProjectionLink || exactGCTypeSubtypingDuplicateRecursiveLink || exactGCTypeSubtypingExtendedRecursiveLink || exactGCTypeSubtypingCrossGroupMismatchLink || exactGCTypeSubtypingTransitiveMismatchLink || exactGCTypeSubtypingNonFlatExport || exactGCRefEq || exactGCRefTestTable || exactGCRefTestConcrete || exactGCRefCastConcrete || exactGCRefTestAbstract || exactGCExtern || exactGCRefCastAbstract || exactGCBrOnCastAbstract || exactGCBrOnCastFailAbstract || exactGCBrOnCastConcrete || exactGCBrOnCastFailConcrete || exactGCBrOnCastNullability || exactGCBrOnCastFailNullability || exactGCStructGet || exactGCStructMutation || exactGCStructNull || exactGCStructPacked || exactGCStructBasic || exactGCStructNumericGlobals || exactGCArrayLocal || exactGCArrayUniform || exactGCArrayFixedLocal || exactGCArrayFixedOfficial || exactGCArrayDefaultOfficial || exactGCArrayPackedData || exactGCArrayReference || exactGCArrayInitElem || exactGCArrayInitData || exactGCArrayFill || exactGCArrayCopy {
		qualifiedFeatures |= CoreFeatureGC
	}
	if exactGCTypeSubtypingDeclarations || exactGCTypeSubtypingRecursiveFunctions || exactGCTypeSubtypingRefFuncGlobals || exactGCTypeSubtypingRuntimeCallCast || exactGCTypeSubtypingRefTestSingle || exactGCTypeSubtypingLink || exactGCTypeSubtypingFinalityLink || exactGCTypeSubtypingStructLink || exactGCTypeSubtypingStructProjectionLink || exactGCTypeSubtypingStructMismatchLink || exactGCTypeSubtypingIndependentStructLink || exactGCTypeSubtypingExtendedProjectionLink || exactGCTypeSubtypingDuplicateRecursiveLink || exactGCTypeSubtypingExtendedRecursiveLink || exactGCTypeSubtypingCrossGroupMismatchLink || exactGCTypeSubtypingTransitiveMismatchLink || exactGCTypeSubtypingNonFlatExport || exactGCRefTestTable || exactGCRefTestConcrete || exactGCRefCastConcrete || exactGCRefTestAbstract || exactGCExtern || exactGCRefCastAbstract || exactGCBrOnCastAbstract || exactGCBrOnCastFailAbstract || exactGCBrOnCastConcrete || exactGCBrOnCastFailConcrete || exactGCBrOnCastNullability || exactGCBrOnCastFailNullability || exactGCStructMutation || exactGCStructNull || exactGCStructPacked || exactGCStructBasic || exactGCStructNumericGlobals || exactGCArrayLocal || exactGCArrayUniform || exactGCArrayFixedLocal || exactGCArrayFixedOfficial || exactGCArrayDefaultOfficial || exactGCArrayPackedData || exactGCArrayReference || exactGCArrayInitElem || exactGCArrayInitData || exactGCArrayFill || exactGCArrayCopy {
		qualifiedFeatures |= CoreFeatureTypedFunctionReferences
	}
	if exactGCTypeSubtypingRefTestSingle || exactGCArrayPackedData || exactGCArrayReference || exactGCArrayInitElem || exactGCArrayInitData {
		qualifiedFeatures |= CoreFeatureBulkMemoryOperations
	}
	if c.nativeTable64ABI == nativeTable64SizeABI && !rv64OwnedTwoTable32Size(c) && (!c.requiredFeatures.IsEnabled(CoreFeatureTable64) || !rv64OwnedTable64Size(c) && !rv64OwnedTwoTable64Size(c)) {
		return reject("table.size compiler witness requires its exact owned or indexed two-table profile")
	}
	if c.nativeTable64ABI == nativeTable64GrowABI && !rv64OwnedTwoTable32Grow(c) && (!c.requiredFeatures.IsEnabled(CoreFeatureTable64) || !rv64OwnedTable64Grow(c) && !rv64OwnedTwoTable64Grow(c)) {
		return reject("table.grow compiler witness requires its exact owned or indexed two-table profile")
	}
	if c.nativeTable64ABI == nativeTable64GetSetABI && !rv64OwnedTwoTable32GetSet(c) && (!c.requiredFeatures.IsEnabled(CoreFeatureTable64) || !rv64OwnedTable64GetSet(c) && !rv64OwnedTwoTable64GetSet(c)) {
		return reject("table.get/set compiler witness requires its exact owned or indexed two-table profile")
	}
	if c.nativeTable64ABI == nativeTable64IndirectABI && !rv64OwnedTwoTable32Indirect(c) && (!c.requiredFeatures.IsEnabled(CoreFeatureTable64) || !rv64OwnedTable64Indirect(c) && !rv64OwnedTwoTable64Indirect(c) && !rv64MixedTable64Indirect(c) && !rv64MixedTable64ActiveIndirect(c) && !rv64ImportedTable64Indirect(c)) {
		return reject("table indirect-call compiler witness requires its exact owned, indexed two-table32/table64, or imported table64 profile")
	}
	if c.nativeTable64ABI == nativeTable64FillABI && !rv64OwnedTwoTable32Fill(c) && (!c.requiredFeatures.IsEnabled(CoreFeatureTable64) || !rv64OwnedTable64Fill(c) && !rv64OwnedTwoTable64Fill(c)) {
		return reject("table.fill compiler witness requires its exact owned or indexed two-table profile")
	}
	if c.nativeTable64ABI == nativeTable64CopyABI && !rv64OwnedTwoTable32Copy(c) && (!c.requiredFeatures.IsEnabled(CoreFeatureTable64) || !rv64OwnedTable64Copy(c) && !rv64OwnedTwoTable64Copy(c)) {
		return reject("table.copy compiler witness requires its exact owned or indexed two-table profile")
	}
	if c.nativeTable64ABI == nativeTable64InitDropABI && !rv64OwnedTwoTable32InitDrop(c) && (!c.requiredFeatures.IsEnabled(CoreFeatureTable64) || !rv64OwnedTable64InitDrop(c) && !rv64OwnedTwoTable64InitDrop(c)) {
		return reject("table.init/drop compiler witness requires its exact owned or indexed two-table profile")
	}
	if c.nativeTable64ABI == nativeTable64ImportedSizeABI && (!c.requiredFeatures.IsEnabled(CoreFeatureTable64) || !rv64ImportedTable64Size(c) && !rv64MixedTable64Size(c)) {
		return reject("table64.size compiler witness requires its exact imported table64 profile")
	}
	if c.nativeTable64ABI == nativeTable64ImportedGetSetABI && (!c.requiredFeatures.IsEnabled(CoreFeatureTable64) || !rv64ImportedTable64GetSet(c) && !rv64MixedTable64GetSet(c)) {
		return reject("table64.get/set compiler witness requires its exact imported table64 profile")
	}
	if c.nativeTable64ABI == nativeTable64ImportedGrowABI && (!c.requiredFeatures.IsEnabled(CoreFeatureTable64) || !rv64ImportedTable64Grow(c) && !rv64MixedTable64Grow(c)) {
		return reject("table64.grow compiler witness requires its exact imported table64 profile")
	}
	if c.nativeTable64ABI == nativeTable64ImportedFillABI && (!c.requiredFeatures.IsEnabled(CoreFeatureTable64) || !rv64ImportedTable64Fill(c) && !rv64MixedTable64Fill(c)) {
		return reject("table64.fill compiler witness requires its exact imported table64 profile")
	}
	if c.nativeTable64ABI == nativeTable64ImportedCopyABI && (!c.requiredFeatures.IsEnabled(CoreFeatureTable64) || !rv64ImportedTable64Copy(c) && !rv64MixedTable64Copy(c) && !rv64MixedTable64CrossCopy(c)) {
		return reject("table64.copy compiler witness requires its exact imported table64 profile")
	}
	if c.nativeTable64ABI == nativeTable64ImportedInitDropABI && (!c.requiredFeatures.IsEnabled(CoreFeatureTable64) || !rv64ImportedTable64InitDrop(c) && !rv64MixedTable64InitDrop(c)) {
		return reject("table64.init/drop compiler witness requires its exact imported table64 profile")
	}
	if c.nativeTable64ABI > nativeTable64ImportedInitDropABI {
		return reject("unknown table64 compiler witness")
	}
	if c.requiredFeatures.IsEnabled(CoreFeatureTable64) && !rv64QualifiedTable64(c) {
		return reject("table64 requires an exact qualified owned-table profile")
	}
	if rv64HasReferenceTail(c) || c.nativeReferenceCallABI || c.nativeReferenceControlABI {
		qualifiedFeatures |= CoreFeatureTypedFunctionReferences
	}
	declarativeTyped := false
	if c.hasNoFunctionCode() && c.requiredFeatures.IsEnabled(CoreFeatureTypedFunctionReferences) && !exactGCStructNumericGlobals && !exactGCTypeSubtypingDeclarations {
		var err error
		declarativeTyped, err = rv64ValidateDeclarativeControlTypes(c)
		if err != nil {
			return reject(err.Error())
		}
		if !declarativeTyped {
			return reject("declarative typed feature requires an exact type witness")
		}
		qualifiedFeatures |= CoreFeatureTypedFunctionReferences
	}
	if c.requiredFeatures & ^qualifiedFeatures != 0 || c.requiredAMD64Features != 0 || c.preparedIsolatedTables {
		return reject("feature or ISA contract")
	}
	for i, entry := range c.Entry {
		if entry&15 != 0 || i >= len(c.InternalEntry) || c.InternalEntry[i] != entry {
			return reject("entry is not an aligned framed wrapper")
		}
	}
	if vectorProfile {
		if len(c.GlobalExports) > railshot.MaxFunctions {
			return reject("global export count bound")
		}
		for _, index := range c.GlobalExports {
			if index < 0 || index >= len(c.Globals) {
				return reject("global export index bound")
			}
		}
	}
	maxArity := 256
	if vectorProfile {
		maxArity = 128
	}
	mutableReferenceCarrier := rv64MutableFuncrefGlobalLocalCallCodeVariant(c)
	declarationExtern := exactGCRefTestAbstract || exactGCExtern || exactGCRefCastAbstract || exactGCBrOnCastAbstract || exactGCBrOnCastFailAbstract || exactCore3RefIsNull || rv64Core3RefDeclarations(c) || rv64Core3ReferenceDeclarations(c) || rv64Core3RefAsNonNullDeclarations(c) || rv64Core3RefIsNullDeclarations(c) || rv64PublicReferenceCallBoundary(c) || rv64PublicHostReferenceBoundary(c) || rv64PublicDynamicHostReferenceBoundary(c) || mutableReferenceCarrier >= 19 && mutableReferenceCarrier <= 28
	for _, sig := range c.Funcs {
		if len(sig.Params) > maxArity || len(sig.Results) > maxArity {
			return reject("signature arity")
		}
		for _, types := range [][]ValType{sig.Params, sig.Results} {
			for _, typ := range types {
				if typ != ValI32 && typ != ValI64 && typ != ValF32 && typ != ValF64 && (!vectorProfile || typ != ValV128) && (!c.nativeReferenceFunctionABI || typ != ValFuncRef) && (!declarationExtern || typ != ValExternRef) && (!typedI31 || typ != ValI31Ref) && (!(freshRootedGC || exactGCExtern || exactGCBrOnCastNullability || exactGCBrOnCastFailNullability || exactGCStructBasic || exactGCArrayFixedOfficial || exactGCArrayDefaultOfficial || exactGCArrayPackedData || exactGCArrayReference || exactGCArrayInitElem || exactGCArrayInitData || exactGCArrayFill || exactGCArrayCopy) || typ != ValAnyRef) {
					return reject("unsupported signature")
				}
			}
		}
	}
	if !freshRootedGC && !exactGCTypeSubtypingDeclarations && !exactGCTypeSubtypingRecursiveFunctions && !exactGCTypeSubtypingRefFuncGlobals && !exactGCTypeSubtypingRuntimeCallCast && !exactGCTypeSubtypingRefTestSingle && !exactGCTypeSubtypingLink && !exactGCTypeSubtypingFinalityLink && !exactGCTypeSubtypingStructLink && !exactGCTypeSubtypingStructProjectionLink && !exactGCTypeSubtypingStructMismatchLink && !exactGCTypeSubtypingIndependentStructLink && !exactGCTypeSubtypingExtendedProjectionLink && !exactGCTypeSubtypingDuplicateRecursiveLink && !exactGCTypeSubtypingExtendedRecursiveLink && !exactGCTypeSubtypingCrossGroupMismatchLink && !exactGCTypeSubtypingTransitiveMismatchLink && !exactGCTypeSubtypingNonFlatExport && !declarativeTyped && !typedI31 && !exactGCRefEq && !exactGCRefTestTable && !exactGCRefTestConcrete && !exactGCRefCastConcrete && !exactGCRefTestAbstract && !exactGCExtern && !exactGCRefCastAbstract && !exactGCBrOnCastAbstract && !exactGCBrOnCastFailAbstract && !exactGCBrOnCastConcrete && !exactGCBrOnCastFailConcrete && !exactGCBrOnCastNullability && !exactGCBrOnCastFailNullability && !exactGCStructGet && !exactGCStructMutation && !exactGCStructNull && !exactGCStructPacked && !exactGCStructBasic && !exactGCStructNumericGlobals && !exactGCArrayLocal && !exactGCArrayUniform && !exactGCArrayFixedLocal && !exactGCArrayFixedOfficial && !exactGCArrayDefaultOfficial && !exactGCArrayPackedData && !exactGCArrayReference && !exactGCArrayInitElem && !exactGCArrayInitData && !exactGCArrayFill && !exactGCArrayCopy {
		if err := rv64ValidateDeclaredControlTypes(c); err != nil {
			return reject(err.Error())
		}
	}
	if !exactGCTypeSubtypingRefFuncGlobals && !exactGCTypeSubtypingRefTestSingle && !exactGCTypeSubtypingLink && !exactGCTypeSubtypingFinalityLink && !exactGCTypeSubtypingStructLink && !exactGCTypeSubtypingStructProjectionLink && !exactGCTypeSubtypingStructMismatchLink && !exactGCTypeSubtypingIndependentStructLink && !exactGCTypeSubtypingExtendedProjectionLink && !exactGCTypeSubtypingDuplicateRecursiveLink && !exactGCTypeSubtypingExtendedRecursiveLink && !exactGCTypeSubtypingCrossGroupMismatchLink && !exactGCTypeSubtypingTransitiveMismatchLink {
		if err := rv64ValidateOwnedTable(c); err != nil {
			return reject(err.Error())
		}
	}
	localMutableReferences, err := rv64ValidateLocalMutableReferences(c)
	if err != nil {
		return reject(err.Error())
	}
	hasReferenceGlobal := false
	stateOnlyExternrefs := rv64StateOnlyExternrefGlobals(c)
	for i, global := range c.Globals {
		if exactGCTypeSubtypingRefFuncGlobals {
			continue // The exact metadata digest pins every indexed ref.func global.
		}
		if exactGCExtern && i < 2 {
			continue // The exact product pins both null conversion globals.
		}
		if exactGCStructPacked && i < 2 {
			continue // The exact product pins both rooted non-null struct initializers.
		}
		if exactGCStructBasic && i < 2 {
			continue // The exact product pins both rooted non-null struct initializers.
		}
		if exactGCStructNumericGlobals && i < 2 {
			continue // The exact state-only product pins both rooted non-null struct initializers.
		}
		if exactGCArrayFixedOfficial && i == 0 || exactGCArrayDefaultOfficial && i < 2 || exactGCArrayInitElem && i < 2 || exactGCArrayInitData && i < 3 || exactGCArrayFill && i < 2 || exactGCArrayCopy && i < 2 {
			continue // The exact product pins this rooted non-null array initializer.
		}
		if typedI31 && (global.Type == ValI31Ref || exactAnyI31 && global.Type == ValAnyRef) {
			continue // The exact product witness pins both globals and their initial values.
		}
		if global.Type == ValExternRef {
			if !stateOnlyExternrefs {
				return reject("externref globals require the exact state-only null directory")
			}
			continue
		}
		if global.Type == ValFuncRef {
			hasReferenceGlobal = true
			plain, _ := valueTypeDescriptorFromValType(ValFuncRef)
			exact, err := c.globalExactType(i)
			typedIndex, typed := rv64PrivateIndexedFunctionType(c, exact)
			callTyped := mutableFuncrefGlobalLocalCall && i == 0 && exact.Kind == ValueTypeReference && exact.Ref.Nullable && !exact.Ref.Exact && exact.Ref.Heap.Defined && exact.Ref.Heap.Abstract == 0 && exact.Ref.Heap.TypeIndex == 0
			mutableQualified := localMutableReferences && i >= len(c.GlobalImports) || (stateOnlyMutableFuncrefImport || mutableFuncrefGlobalGet || mutableFuncrefGlobalClear || mutableFuncrefGlobalStoreSelf || mutableFuncrefGlobalCopy || mutableFuncrefGlobalMove || mutableFuncrefGlobalBidirectionalCopy || mutableFuncrefGlobalLocalCopy || mutableFuncrefGlobalLocalPublish || mutableFuncrefGlobalLocalTransfer) && i < len(c.GlobalImports)
			if err != nil || exact != plain && !typed && !callTyped || global.Mutable && !mutableQualified || len(global.InitExpr) != 0 || global.Bits != 0 || global.V128 != (V128{}) || c.tableCount() > 1 || !c.requiredFeatures.IsEnabled(CoreFeatureReferenceTypes) || global.HasInitFunc && (uint64(global.InitFunc) >= uint64(c.NumImports+len(c.Funcs)) || !c.needsFuncRefDescs()) {
				return reject("qualified reference global and retained references required")
			}
			if typed && !callTyped {
				local := int(global.InitFunc) - c.NumImports
				if i < len(c.GlobalImports) || global.Mutable || c.NumImports != 0 || c.tableCount() != 0 || c.memoryCount() != 0 || c.activeDataCount() != 0 || len(c.PassiveData) != 0 || c.HasStart || !global.HasInitFunc || global.HasInitGlobal || global.InitGlobal != 0 || local < 0 || local >= len(c.Funcs) || !c.Funcs[local].HasTypeIndex || c.Funcs[local].TypeIndex != typedIndex || !c.requiredFeatures.IsEnabled(CoreFeatureTypedFunctionReferences) {
					return reject("private indexed reference global exact target")
				}
			}
			if i < len(c.GlobalImports) && (global.HasInitFunc || global.HasInitGlobal) {
				return reject("reference import initializer")
			}
			if global.HasInitGlobal && (global.HasInitFunc || !rv64InitializerGlobal(c, global.InitGlobal, ValFuncRef, i)) {
				return reject("reference initializer requires an earlier immutable global")
			}
			continue
		}
		if (global.Type != ValI32 && global.Type != ValI64 && global.Type != ValF32 && global.Type != ValF64 && (!vectorProfile || global.Type != ValV128)) || global.HasInitFunc {
			return reject("global initializer lowering is pending")
		}
		if len(global.InitExpr) != 0 {
			if i < len(c.GlobalImports) || global.HasInitGlobal {
				return reject("conflicting global initializer")
			}
			if err := rv64ValidateIntegerInitializer(c, global.InitExpr, global.Type, i); err != nil {
				return reject(err.Error())
			}
		}
		if global.HasInitGlobal && (i < len(c.GlobalImports) || !rv64InitializerGlobal(c, global.InitGlobal, global.Type, i)) {
			return reject("global initializer requires an available immutable global of the exact type")
		}
	}
	if hasReferenceGlobal {
		if len(c.GlobalExports) > railshot.MaxFunctions {
			return reject("reference-global export count bound")
		}
		for _, index := range c.GlobalExports {
			if index < 0 || index >= len(c.Globals) {
				return reject("reference global export index")
			}

		}
	}
	for i := 0; i < c.activeDataCount(); i++ {
		data := c.activeDataAt(i)
		if uint64(data.MemoryIndex) >= uint64(c.memoryCount()) {
			return reject("data initializer memory index")
		}
		offsetType := ValI32
		if c.memoryDef(int(data.MemoryIndex)).Addr64 {
			offsetType = ValI64
			if data.Offset.HasGlobal {
				return reject("memory64 global offset requires an i64 expression")
			}
		}
		if len(data.Offset.Expr) != 0 {
			if data.Offset.HasGlobal {
				return reject("conflicting data initializer")
			}
			if err := rv64ValidateIntegerInitializer(c, data.Offset.Expr, offsetType, len(c.Globals)); err != nil {
				return reject(err.Error())
			}
		}
		if data.Offset.HasGlobal && !rv64InitializerGlobal(c, data.Offset.Global, ValI32, len(c.Globals)) {
			return reject("data initializer lowering is pending")
		}
	}
	if c.memoryCount() > 1 && !c.requiredFeatures.IsEnabled(CoreFeatureMultiMemory) {
		return reject("memory directory requires multi-memory feature metadata")
	}
	for i := 0; i < c.memoryCount(); i++ {
		memory := c.memoryDef(i)
		maxTypePages := uint64(65536)
		if memory.Addr64 {
			maxTypePages = 1 << 48
			if !c.requiredFeatures.IsEnabled(CoreFeatureMemory64) {
				return reject("memory64 requires feature metadata")
			}
		}
		reservation, err := ownedMemoryReservationPages(memory)
		if memory.Shared || err != nil || (memory.HasMax && memory.Max > maxTypePages) {
			return reject("bounded unshared memory required")
		}
		if i == 0 && (!c.HasMemory || uint64(c.MemMinPages) != memory.Min || uint64(c.MemMaxPages) < memory.Min || uint64(c.MemMaxPages) > reservation) {
			return reject("primary memory reservation metadata mismatch")
		}
	}
	if len(c.memoryExportMap()) > railshot.MaxFunctions {
		return reject("memory export count bound")
	}
	for _, index := range c.memoryExportMap() {
		if index < 0 || index >= c.memoryCount() {
			return reject("memory export index bound")
		}
	}
	if c.memoryDir != nil && (len(c.memoryDir.ehTags) != 0 || len(c.memoryDir.gcStructGlobals) != 0 && !exactGCStructPacked && !exactGCStructBasic && !exactGCStructNumericGlobals || len(c.memoryDir.gcArrayGlobals) != 0 && !exactGCArrayFixedOfficial && !exactGCArrayDefaultOfficial && !exactGCArrayInitElem && !exactGCArrayInitData && !exactGCArrayFill && !exactGCArrayCopy || c.memoryDir.gcArrayElement != nil && !exactGCArrayReference || c.memoryDir.gcI31TableInit != nil) {
		return reject("GC/EH initializer lowering is pending")
	}
	return nil
}

// Every callable RV64 producer has a canonical context anchor, including
// import-free scalar exports used as retained proper-tail targets.
func railshotNeedsImportContextHeader(count int) bool { return count > 0 }

// Binding policy runs before prepareCodeMapping, including trusted artifacts.
// Numeric/vector native bindings use retained framed bridges. Go callbacks and
// owned host handles retain their separately qualified scalar protocol.
func railshotValidateResolvedImports(c *Compiled, imports resolvedImports, store *referenceStore) error {
	c = c.executionView()
	if c.NumImports == 0 && len(c.Imports) == 0 && c.tableImportCount() == 0 && len(c.GlobalImports) == 0 {
		return nil
	}
	if err := railshotValidateCompiled(c); err != nil {
		return err
	}
	if err := rv64ValidateTailImports(c, imports); err != nil {
		return err
	}
	if err := rv64ValidateResolvedGlobals(c, imports, store); err != nil {
		return err
	}
	if err := rv64ValidateResolvedTable(c, imports, store); err != nil {
		return err
	}
	hasReferenceGlobal := false
	for _, global := range c.Globals {
		if global.Type == ValFuncRef {
			hasReferenceGlobal = true
			break
		}
	}
	if hasReferenceGlobal {
		// Immutable exported reference chains are bounded and validated above.
		// Reject unqualified token owners before mapping or attaching any import.
		for _, index := range c.GlobalExports {
			global := c.Globals[index]
			if global.Type != ValFuncRef {
				continue
			}
			for global.HasInitGlobal {
				global = c.Globals[global.InitGlobal]
			}
			if !global.HasInitFunc || int(global.InitFunc) >= c.NumImports {
				continue
			}
			binding := imports[c.functionImportBindingKey(int(global.InitFunc))]
			if host, ok := binding.(*HostFuncRef); ok {
				if err := host.validateImport(store, c.importFuncSigs[global.InitFunc]); err != nil {
					return err
				}
				continue // The scalar signature gate below also applies.
			}
			ex, ok := binding.(*InstanceExport)
			if !ok || ex == nil || ex.inst == nil || store == nil {
				return fmt.Errorf("%w: RV64 reference export requires a same-Runtime native function", railshot.ErrUnsupported)
			}
			ex.inst.lifeMu.Lock()
			sameStore := ex.inst.refStore == store
			ex.inst.lifeMu.Unlock()
			if !sameStore {
				return fmt.Errorf("%w: RV64 reference export requires the same Runtime", railshot.ErrUnsupported)
			}
		}
	}
	var reexported [railshot.MaxHostFunctionImports]bool
	for _, index := range c.Exports {
		if index < 0 || index >= c.NumImports+len(c.Funcs) {
			return fmt.Errorf("%w: public RV64 function export index", railshot.ErrUnsupported)
		}
		if index < c.NumImports {
			reexported[index] = true
		}
	}
	for i, key := range c.Imports {
		value := imports[c.functionImportBindingKey(i)]
		if reexported[i] {
			if _, native := value.(*InstanceExport); !native {
				return fmt.Errorf("%w: public RV64 import %q reexport requires a native instance export", railshot.ErrUnsupported, key)
			}
		}
		switch value := value.(type) {
		case *HostFuncRef:
			if !rv64PublicHostReferenceBoundary(c) && !rv64PublicDynamicHostReferenceBoundary(c) {
				if err := rv64ScalarHostSignature(c.importFuncSigs[i]); err != nil {
					return fmt.Errorf("public RV64 import %q: %w", key, err)
				}
			}
			// The shared attachment path repeats this check under its ownership
			// lock before retaining the handle; reject invalid input before mapping.
			if err := value.validateImport(store, c.importFuncSigs[i]); err != nil {
				return fmt.Errorf("public RV64 import %q: %w", key, err)
			}
			continue
		case *InstanceExport:
			if err := railshotValidateNativeExport(c, i, value); err != nil {
				return fmt.Errorf("public RV64 import %q: %w", key, err)
			}
			continue
		case I32HostEvent, gatedI32HostEvent:
			return fmt.Errorf("%w: public RV64 import %q target ownership/protocol is pending", railshot.ErrUnsupported, key)
		}
		if !rv64PublicHostReferenceBoundary(c) && !rv64PublicDynamicHostReferenceBoundary(c) {
			if err := rv64ScalarHostSignature(c.importFuncSigs[i]); err != nil {
				return fmt.Errorf("public RV64 import %q: %w", key, err)
			}
		}
		if _, err := bindSyncHostImport(value, c.importFuncSigs[i]); err != nil {
			return fmt.Errorf("public RV64 import %q: %w", key, err)
		}
	}
	return nil
}

func rv64ImportedInitializer(c *Compiled, index int, typ ValType) bool {
	return index >= 0 && index < len(c.GlobalImports) && !c.GlobalImports[index].Mutable && c.GlobalImports[index].Type == typ
}

// This binding-dependent gate runs for every host import, including unused
// signatures and trusted artifacts, before executable mapping or owner retention.
func rv64ScalarHostSignature(sig FuncSig) error {
	if len(sig.Params) > coreruntime.MaxHostArity || len(sig.Results) > coreruntime.MaxHostArity {
		return fmt.Errorf("%w: public RV64 scalar host signature arity", railshot.ErrUnsupported)
	}
	for _, types := range [][]ValType{sig.Params, sig.Results} {
		for _, typ := range types {
			if typ != ValI32 && typ != ValI64 && typ != ValF32 && typ != ValF64 {
				return fmt.Errorf("%w: public RV64 nonscalar host signature", railshot.ErrUnsupported)
			}
		}
	}
	return nil
}

// Reject incompatible storage/targets before executable mapping or element writes.
// A temporary physical lease makes row authentication safe even during retirement.
func rv64ValidateResolvedTable(c *Compiled, imports resolvedImports, store *referenceStore) error {
	def, imported := c.tableImportAt(0)
	if !imported {
		return nil
	}
	table, ok := imports.table(c.tableImportBindingKey(0))
	if !ok || table == nil {
		return fmt.Errorf("missing imported table %q", def.Key)
	}
	exact, err := c.tableExactType(0)
	if err != nil {
		return err
	}
	if err := table.validateImportWithCollector(ValFuncRef, exact, c.Types, store, nil, def.Addr64); err != nil {
		return err
	}
	// The descriptor itself also needs a lease: host tables have no instance root.
	if err := table.attachImporterWithCollector(ValFuncRef, exact, c.Types, store, nil, def.Addr64); err != nil {
		return err
	}
	defer table.detachImporter(store)
	var roots rv64NativeTableTargets
	// Final-root release can reenter native execution. Unlock first.
	defer roots.release()
	nativeExecutionMu.Lock()
	defer nativeExecutionMu.Unlock()
	edges := 0
	if err := roots.tableRoots(table, &edges); err != nil {
		return fmt.Errorf("%w: %v", railshot.ErrUnsupported, err)
	}
	table.owner.mu.Lock()
	table.mu.Lock()
	size, capacity := uint64(binary.LittleEndian.Uint32(table.desc)), uint64(binary.LittleEndian.Uint32(table.desc[4:]))
	valid := size >= def.Min && size <= capacity && capacity <= railshot.MaxOwnedTableEntries && 8+capacity*coreruntime.TableEntryBytes <= uint64(len(table.desc)) && (!def.HasMax || table.owner.declaredHasMax && capacity <= def.Max)
	table.mu.Unlock()
	table.owner.mu.Unlock()
	if !valid {
		return fmt.Errorf("%w: imported table capacity/limits/extent", railshot.ErrUnsupported)
	}
	if err := roots.validateReachable(); err != nil {
		return fmt.Errorf("%w: %v", railshot.ErrUnsupported, err)
	}
	if err := roots.validateTable(table.desc, coreruntime.TableEntryBytes); err != nil {
		return fmt.Errorf("%w: %v", railshot.ErrUnsupported, err)
	}
	return nil
}

// Compact local references need their own feature witness; the codec's generic
// structural feature scan does not infer local scope from these indices.
func rv64InitializerGlobal(c *Compiled, index int, expected ValType, limit int) bool {
	if index < 0 || index >= limit || index >= len(c.Globals) {
		return false
	}
	if index < len(c.GlobalImports) {
		return rv64ImportedInitializer(c, index, expected)
	}
	g := c.Globals[index]
	return c.requiredFeatures.IsEnabled(CoreFeatureExtendedConstExpressions) && !g.Mutable && g.Type == expected
}

func rv64ValidateIntegerInitializer(c *Compiled, body []byte, expected ValType, limit int) error {
	if (expected != ValI32 && expected != ValI64) || (!c.requiredFeatures.IsEnabled(CoreFeatureExtendedConst) && !c.requiredFeatures.IsEnabled(CoreFeatureExtendedConstExpressions) && !rv64CoreIntegerInitializer(body)) {
		return fmt.Errorf("integer expression requires its numeric feature metadata")
	}
	return railshot.ValidateIntegerInitializer(body, rv64NumericWasmType(expected), func(index uint32) (wasm.ValType, bool) {
		if uint64(index) >= uint64(len(c.Globals)) {
			return wasm.ValType{}, false
		}
		g := c.Globals[index]
		return rv64NumericWasmType(g.Type), rv64InitializerGlobal(c, int(index), g.Type, limit)
	})
}

// Memory64 keeps even literal/global offsets as Expr to retain all 64 bits.
// This representation alone does not imply extended-constant instructions.
// The common validator below still proves the result type and global scope.
func rv64CoreIntegerInitializer(body []byte) bool {
	if len(body) > railshot.MaxIntegerInitializerBytes {
		return false
	}
	r := wasm.ReaderFrom(body)
	op, err := r.Byte()
	if err != nil {
		return false
	}
	switch op {
	case 0x41:
		_, err = r.I32()
	case 0x42:
		_, err = r.I64()
	case 0x23:
		_, err = r.U32()
	default:
		return false
	}
	if err != nil {
		return false
	}
	end, err := r.Byte()
	return err == nil && end == 0x0b && !r.HasNext()
}
