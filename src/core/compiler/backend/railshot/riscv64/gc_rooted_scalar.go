//go:build linux && !tinygo

package riscv64

import (
	"bytes"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

const maxRootedScalarArrayFixed = 254
const maxRootedScalarHostImports = MaxHostFunctionImports
const maxRootedScalarMemories = 4

// admitRV64GCRootedScalarModule is the first source-shape admission shared by
// generic GC programs instead of a byte-for-byte product fixture. It keeps the
// runtime boundary deliberately closed while admitting ordinary scalar control
// over bounded numeric scalar structs. Instruction admission remains the
// responsibility of the typed integer walker, and exact allocation roots are
// authenticated independently by validateGCFrameRootPlans.
func admitRV64GCRootedScalarModule(m *wasm.Module) bool {
	if m == nil || len(m.Code) == 0 || len(m.Code) > MaxFunctions || len(m.Types) == 0 || len(m.Types) > MaxFunctions || len(m.Exports) > MaxFunctions ||
		len(m.Tables) > 2 || len(m.Memories) > maxRootedScalarMemories || len(m.Globals) > 256 ||
		len(m.Elements) > 256 || len(m.Data) > 256 || len(m.Tags) != 0 ||
		len(m.StringRefs) != 0 || len(m.Customs) != 0 || m.HasNameMetadata() ||
		m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	importedFunctions := m.ImportedFuncCount()
	importedGlobals := m.ImportedGlobalCount()
	importedMemories := m.ImportedMemCount()
	importedTables := m.ImportedTableCount()
	memoryCount := importedMemories + len(m.Memories)
	tableCount := importedTables + len(m.Tables)
	if importedFunctions+importedGlobals+importedMemories+importedTables != len(m.Imports) || importedFunctions > maxRootedScalarHostImports || importedFunctions+len(m.Code) > MaxFunctions ||
		importedGlobals+len(m.Globals) > 256 || memoryCount > maxRootedScalarMemories || tableCount > 2 {
		return false
	}
	if len(m.Imports) != 0 {
		// The generic host boundary remains numeric. Imported references must not
		// become reachable through private tables.
		for i := range m.Imports {
			im := &m.Imports[i]
			switch im.Type.Kind {
			case wasm.ExternFunc:
				ft, ok := m.ImportFuncType(i)
				if !ok || len(ft.Params) > coreruntime.MaxHostArity || len(ft.Results) > coreruntime.MaxHostArity {
					return false
				}
				for _, values := range [][]wasm.ValType{ft.Params, ft.Results} {
					for _, typ := range values {
						if !scalarType(typ, true) {
							return false
						}
					}
				}
			case wasm.ExternGlobal:
				typ := im.Type.GlobalType().Type
				if !wasm.EqualValType(typ, wasm.I32) && !wasm.EqualValType(typ, wasm.I64) && !wasm.EqualValType(typ, wasm.F32) && !wasm.EqualValType(typ, wasm.F64) {
					return false
				}
			case wasm.ExternMem:
				memory := im.Type.MemType()
				if memory.Shared || memory.Limits.Addr64 || !memory.Limits.HasMax || memory.Limits.Min > memory.Limits.Max || memory.Limits.Max > 256 {
					return false
				}
			case wasm.ExternTable:
				table := im.Type.TableType()
				if !wasm.EqualValType(wasm.RefVal(table.Ref), wasm.FuncRef) || table.Limits.Addr64 || !table.Limits.HasMax ||
					table.Limits.Min > table.Limits.Max || table.Limits.Max > 256 {
					return false
				}
			default:
				return false
			}
		}
	}
	if m.DataCount != nil && (uint64(*m.DataCount) != uint64(len(m.Data)) || *m.DataCount > 256) {
		return false
	}
	if admitProductionStart(m) != nil {
		return false
	}
	for i := range m.Memories {
		memory := m.Memories[i]
		if memory.Shared || memory.Limits.Addr64 || !memory.Limits.HasMax || memory.Limits.Min > memory.Limits.Max || memory.Limits.Max > 256 {
			return false
		}
	}
	publicTables := make([]bool, tableCount)
	for i := 0; i < importedTables; i++ {
		publicTables[i] = true
	}
	for i := range m.Exports {
		export := m.Exports[i]
		if export.Index.Kind == wasm.ExternTable && uint64(export.Index.Index) < uint64(tableCount) {
			publicTables[export.Index.Index] = true
		}
	}
	hasPrivateTable := false
	for _, public := range publicTables {
		hasPrivateTable = hasPrivateTable || !public
	}
	for i := range m.Tables {
		table := &m.Tables[i]
		elem := wasm.RefVal(table.Type.Ref)
		rootedTable := rootedScalarTableReference(elem, m)
		if (!wasm.EqualValType(elem, wasm.FuncRef) && !rootedTable) || table.Type.Limits.Addr64 || !table.Type.Limits.HasMax ||
			table.Type.Limits.Min > table.Type.Limits.Max || table.Type.Limits.Max > 256 {
			return false
		}
		if rootedTable && publicTables[importedTables+i] {
			// Collector references remain private until the public Table API has
			// an independently qualified ownership and type-identity contract.
			return false
		}
		if table.Init != nil {
			if rootedTable {
				// The default null initializer is represented by a missing expression.
				// Non-null table initializers need allocation-root qualification.
				return false
			}
			value, err := tableElementExpression(*table.Init)
			public := publicTables[importedTables+i]
			if err != nil || !value.Null && ((!public && uint64(value.FuncIndex) < uint64(importedFunctions)) || uint64(value.FuncIndex) >= uint64(importedFunctions+len(m.FuncTypes))) {
				return false
			}
		}
	}
	remainingElements := 256
	for i := range m.Elements {
		elem := &m.Elements[i]
		if tableCount == 0 {
			return false
		}
		values := 0
		allowImported := !hasPrivateTable
		if elem.Mode.Kind == wasm.ElemActive {
			if uint64(elem.Mode.Table) >= uint64(tableCount) {
				return false
			}
			table, ok := m.TableType(uint32(elem.Mode.Table))
			if !ok || !wasm.EqualValType(wasm.RefVal(table.Ref), wasm.FuncRef) && !rootedScalarTableReference(wasm.RefVal(table.Ref), m) {
				return false
			}
			typed := elem.Kind.Kind == wasm.ElemTypedExprs
			if (typed && !wasm.EqualValType(wasm.RefVal(elem.Kind.Ref), wasm.RefVal(table.Ref))) ||
				(!typed && !wasm.EqualValType(wasm.RefVal(table.Ref), wasm.FuncRef)) {
				return false
			}
			allowImported = publicTables[elem.Mode.Table]
		}
		switch elem.Kind.Kind {
		case wasm.ElemFuncs:
			if len(elem.Kind.Exprs) != 0 || elem.Kind.Ref != (wasm.RefType{}) {
				return false
			}
			values = len(elem.Kind.Funcs)
			for _, function := range elem.Kind.Funcs {
				if (!allowImported && uint64(function) < uint64(importedFunctions)) || uint64(function) >= uint64(importedFunctions+len(m.FuncTypes)) {
					return false
				}
			}
		case wasm.ElemFuncExprs, wasm.ElemTypedExprs:
			aggregate := elem.Kind.Kind == wasm.ElemTypedExprs && rootedScalarTableReference(wasm.RefVal(elem.Kind.Ref), m)
			if len(elem.Kind.Funcs) != 0 || elem.Kind.Kind == wasm.ElemFuncExprs && elem.Kind.Ref != (wasm.RefType{}) ||
				elem.Kind.Kind == wasm.ElemTypedExprs && !aggregate && !wasm.EqualValType(wasm.RefVal(elem.Kind.Ref), wasm.FuncRef) {
				return false
			}
			values = len(elem.Kind.Exprs)
			for _, expr := range elem.Kind.Exprs {
				if aggregate {
					if !rootedScalarAggregateElementExpression(expr, elem.Kind.Ref, m) {
						return false
					}
					continue
				}
				value, err := tableElementExpression(expr)
				if err != nil || !value.Null && ((!allowImported && uint64(value.FuncIndex) < uint64(importedFunctions)) || uint64(value.FuncIndex) >= uint64(importedFunctions+len(m.FuncTypes))) {
					return false
				}
			}
		default:
			return false
		}
		if values > remainingElements {
			return false
		}
		switch elem.Mode.Kind {
		case wasm.ElemActive:
			if uint64(elem.Mode.Table) >= uint64(tableCount) || len(elem.Mode.Offset.Instrs) != 0 {
				return false
			}
			if _, err := plainScalarConst(elem.Mode.Offset, wasm.I32, false); err != nil {
				return false
			}
		case wasm.ElemPassive, wasm.ElemDeclarative:
			if elem.Mode.Table != 0 || len(elem.Mode.Offset.Instrs) != 0 || len(elem.Mode.Offset.BodyBytes) != 0 {
				return false
			}
		default:
			return false
		}
		remainingElements -= values
	}
	if importedFunctions != 0 && hasPrivateTable {
		classifier := wasm.NewModuleInstructionClassifier(m, true)
		for i := range m.Code {
			r := wasm.NewReader(m.Code[i].BodyBytes)
			for r.HasNext() {
				op, err := r.Byte()
				if err != nil {
					return false
				}
				var imm wasm.InstructionImmediate
				if err = classifier.ClassifyInto(r, op, &imm); err != nil || imm.Kind == wasm.InstrRefFunc && uint64(imm.Index) < uint64(importedFunctions) {
					return false
				}
			}
		}
	}
	for i := range m.Globals {
		global := &m.Globals[i]
		if len(global.Init.Instrs) != 0 {
			return false
		}
		if global.Type.Type.Kind() == wasm.ValRef {
			if !rootedScalarNullGlobal(*global, m) {
				return false
			}
			continue
		}
		if !scalarType(global.Type.Type, true) {
			return false
		}
		if wasm.EqualValType(global.Type.Type, wasm.I32) || wasm.EqualValType(global.Type.Type, wasm.I64) {
			if err := admitModuleIntegerInitializer(global.Init, global.Type.Type, m, importedGlobals+i); err != nil {
				return false
			}
		} else if len(global.Init.BodyBytes) != 0 && global.Init.BodyBytes[0] == 0x23 {
			if err := admitGlobalExpression(global.Init, m, global.Type.Type, importedGlobals+i); err != nil {
				return false
			}
		} else if _, err := plainScalarConst(global.Init, global.Type.Type, true); err != nil {
			return false
		}
	}
	remainingData := MaxModuleBytes
	for i := range m.Data {
		data := &m.Data[i]
		if len(data.Init) > remainingData {
			return false
		}
		switch data.Mode.Kind {
		case wasm.DataActive:
			if uint64(data.Mode.Mem) >= uint64(memoryCount) || len(data.Mode.Offset.Instrs) != 0 {
				return false
			}
			if _, err := plainScalarConst(data.Mode.Offset, wasm.I32, false); err != nil {
				return false
			}
		case wasm.DataPassive:
			if data.Mode.Mem != 0 || len(data.Mode.Offset.Instrs) != 0 || len(data.Mode.Offset.BodyBytes) != 0 {
				return false
			}
		default:
			return false
		}
		remainingData -= len(data.Init)
	}
	for i := range m.Exports {
		export := &m.Exports[i]
		switch export.Index.Kind {
		case wasm.ExternFunc:
			if uint64(export.Index.Index) < uint64(importedFunctions) || uint64(export.Index.Index) >= uint64(importedFunctions+len(m.FuncTypes)) {
				return false
			}
		case wasm.ExternMem:
			if uint64(export.Index.Index) >= uint64(memoryCount) {
				return false
			}
		case wasm.ExternGlobal:
			global, ok := m.GlobalTypeByIndex(export.Index.Index)
			if !ok || global.Type.Kind() == wasm.ValRef {
				return false
			}
		case wasm.ExternTable:
			if uint64(export.Index.Index) >= uint64(tableCount) {
				return false
			}
		default:
			return false
		}
	}
	hasAggregate := false
	flatTypes := 0
	admittedAggregates := make([]bool, 0, len(m.Types))
	rootedValue := func(typ wasm.ValType) bool {
		if scalarType(typ, true) {
			return true
		}
		if typ.Kind() != wasm.ValRef || typ.Ref().Exact() {
			return false
		}
		heap := typ.Ref().Heap()
		if heap.Kind() == wasm.HeapAbs {
			return rootedAbstractObjectHeap(heap.Abs())
		}
		return heap.Kind() == wasm.HeapTypeIndex && !heap.Type().Rec && int(heap.Type().Index) < flatTypes && admittedAggregates[heap.Type().Index]
	}
	for i := range m.Types {
		group := &m.Types[i]
		if len(group.SubTypes) != 1 {
			return false
		}
		sub := &group.SubTypes[0]
		if !sub.Final || sub.HasPrefix || len(sub.Supers) != 0 ||
			sub.Metadata.Describes.Present() || sub.Metadata.Descriptor.Present() {
			return false
		}
		switch sub.Comp.Kind {
		case wasm.CompStruct:
			if len(sub.Comp.Fields) == 0 || len(sub.Comp.Fields) > 255 {
				return false
			}
			for _, field := range sub.Comp.Fields {
				storage := field.Storage()
				if storage.Packed() {
					if storage.Pack() != wasm.PackI8 && storage.Pack() != wasm.PackI16 {
						return false
					}
				} else if !scalarType(storage.Val(), true) {
					value := storage.Val()
					if value.Kind() != wasm.ValRef || value.Ref().Exact() {
						return false
					}
					heap := value.Ref().Heap()
					if heap.Kind() == wasm.HeapAbs && heap.Abs() == wasm.HeapFunc {
						continue
					}
					if heap.Kind() != wasm.HeapTypeIndex || heap.Type().Rec || int(heap.Type().Index) >= flatTypes || !admittedAggregates[heap.Type().Index] {
						return false
					}
				}
			}
			admittedAggregates = append(admittedAggregates, true)
			hasAggregate = true
		case wasm.CompArray:
			storage := sub.Comp.Array.Storage()
			if storage.Packed() {
				if storage.Pack() != wasm.PackI8 && storage.Pack() != wasm.PackI16 {
					return false
				}
			} else if !scalarType(storage.Val(), true) {
				value := storage.Val()
				if value.Kind() != wasm.ValRef || value.Ref().Exact() {
					return false
				}
				heap := value.Ref().Heap()
				if !(heap.Kind() == wasm.HeapAbs && heap.Abs() == wasm.HeapFunc) &&
					(heap.Kind() != wasm.HeapTypeIndex || heap.Type().Rec || int(heap.Type().Index) >= flatTypes || !admittedAggregates[heap.Type().Index]) {
					return false
				}
			}
			admittedAggregates = append(admittedAggregates, true)
			hasAggregate = true
		case wasm.CompFunc:
			if len(sub.Comp.Params) > 256 || len(sub.Comp.Results) > 256 {
				return false
			}
			for _, values := range [][]wasm.ValType{sub.Comp.Params, sub.Comp.Results} {
				for _, typ := range values {
					if !rootedValue(typ) {
						return false
					}
				}
			}
			admittedAggregates = append(admittedAggregates, false)
		default:
			return false
		}
		flatTypes++
	}
	if !hasAggregate || flatTypes > MaxFunctions || len(m.FuncTypes) != len(m.Code) {
		return false
	}
	for _, typ := range m.FuncTypes {
		if typ.Rec || int(typ.Index) >= flatTypes {
			return false
		}
		ft, ok := m.TypeFunc(typ.Index)
		if !ok || ft.Kind != wasm.CompFunc {
			return false
		}
	}
	// Reference values are private collector carriers. The public host adapter
	// remains scalar-only until it has an independently qualified ownership ABI.
	for i := range m.Exports {
		if m.Exports[i].Index.Kind == wasm.ExternMem || m.Exports[i].Index.Kind == wasm.ExternGlobal || m.Exports[i].Index.Kind == wasm.ExternTable {
			continue
		}
		index := m.Exports[i].Index.Index
		if uint64(index) < uint64(importedFunctions) || uint64(index) >= uint64(importedFunctions+len(m.FuncTypes)) {
			return false
		}
		ft, ok := m.TypeFunc(m.FuncTypes[index-uint32(importedFunctions)].Index)
		if !ok {
			return false
		}
		for _, values := range [][]wasm.ValType{ft.Params, ft.Results} {
			for _, typ := range values {
				if !scalarType(typ, true) {
					return false
				}
			}
		}
	}
	return true
}

// rootedScalarNullGlobal admits the first generic persistent collector state:
// a concrete nullable reference to an already-declared aggregate, initialized to
// null. Non-null constant expressions remain closed until their allocation and
// construction roots can be authenticated independently.
func rootedScalarNullGlobal(global wasm.Global, m *wasm.Module) bool {
	ref := global.Type.Type.Ref()
	heap := ref.Heap()
	if !ref.Nullable() || ref.Exact() || heap.Kind() != wasm.HeapTypeIndex || heap.Type().Rec ||
		!rootedScalarAggregateAt(m, heap.Type().Index) ||
		len(global.Init.Instrs) != 0 || len(global.Init.BodyBytes) < 3 || len(global.Init.BodyBytes) > 7 || global.Init.BodyBytes[0] != 0xd0 {
		return false
	}
	r := wasm.ReaderFrom(global.Init.BodyBytes[1:])
	index, err := r.S33()
	if err != nil || index < 0 || uint64(index) != uint64(heap.Type().Index) {
		return false
	}
	end, err := r.Byte()
	return err == nil && end == 0x0b && !r.HasNext()
}

func rootedScalarAggregateAt(m *wasm.Module, index uint32) bool {
	if m == nil {
		return false
	}
	flat := uint32(0)
	for group := range m.Types {
		for member := range m.Types[group].SubTypes {
			if flat == index {
				kind := m.Types[group].SubTypes[member].Comp.Kind
				return kind == wasm.CompStruct || kind == wasm.CompArray
			}
			flat++
		}
	}
	return false
}

func rootedScalarTableReference(typ wasm.ValType, m *wasm.Module) bool {
	if typ.Kind() != wasm.ValRef {
		return false
	}
	ref := typ.Ref()
	heap := ref.Heap()
	return ref.Nullable() && !ref.Exact() && heap.Kind() == wasm.HeapTypeIndex && !heap.Type().Rec && rootedScalarAggregateAt(m, heap.Type().Index)
}

func rootedScalarAggregateNullExpression(expr wasm.Expr, ref wasm.RefType) bool {
	if len(expr.Instrs) != 0 || len(expr.BodyBytes) < 3 || len(expr.BodyBytes) > 7 || ref.Heap().Kind() != wasm.HeapTypeIndex {
		return false
	}
	r := wasm.ReaderFrom(expr.BodyBytes)
	op, err := r.Byte()
	heap, heapErr := r.S33()
	end, endErr := r.Byte()
	return err == nil && op == 0xd0 && heapErr == nil && heap >= 0 && uint64(heap) == uint64(ref.Heap().Type().Index) && endErr == nil && end == 0x0b && !r.HasNext()
}

func rootedScalarAggregateType(m *wasm.Module, index uint32) *wasm.CompType {
	if m == nil {
		return nil
	}
	flat := uint32(0)
	for group := range m.Types {
		for member := range m.Types[group].SubTypes {
			if flat == index {
				return &m.Types[group].SubTypes[member].Comp
			}
			flat++
		}
	}
	return nil
}

func rootedScalarAggregateConstOperandOpcode(r *wasm.Reader, storage wasm.StorageType, op byte, m *wasm.Module) bool {
	want := storage.Val()
	if storage.Packed() {
		want = wasm.I32
	}
	var err error
	switch {
	case wasm.EqualValType(want, wasm.I32):
		_, err = r.I32()
		return op == 0x41 && err == nil
	case wasm.EqualValType(want, wasm.I64):
		_, err = r.I64()
		return op == 0x42 && err == nil
	case wasm.EqualValType(want, wasm.F32):
		_, err = r.Bytes(4)
		return op == 0x43 && err == nil
	case wasm.EqualValType(want, wasm.F64):
		_, err = r.Bytes(8)
		return op == 0x44 && err == nil
	case wasm.EqualValType(want, wasm.V128):
		sub, subErr := r.U32()
		_, err = r.Bytes(16)
		return op == 0xfd && subErr == nil && sub == 12 && err == nil
	case want.Kind() == wasm.ValRef:
		switch op {
		case 0xd0:
			heap, heapErr := r.S33()
			if heapErr != nil || !want.Ref().Nullable() {
				return false
			}
			var actual wasm.HeapType
			if heap >= 0 {
				actual = wasm.IndexedHeap(wasm.TypeIdx{Index: uint32(heap)})
			} else {
				actual = wasm.AbsHeap(wasm.AbsHeapType(byte(heap & 0x7f)))
			}
			return m.ReferenceTypeSubtype(wasm.Ref(true, actual, false), want.Ref())
		case 0xd2:
			index, indexErr := r.U32()
			if indexErr != nil || index < uint32(m.ImportedFuncCount()) {
				return false
			}
			typeIndex, ok := m.FuncTypeIndex(index)
			return ok && !typeIndex.Rec && m.ReferenceTypeSubtype(wasm.Ref(false, wasm.IndexedHeap(typeIndex), false), want.Ref())
		default:
			return false
		}
	default:
		return false
	}
}

func rootedScalarAggregateSupportedStorage(storage wasm.StorageType) bool {
	if storage.Packed() {
		return true
	}
	want := storage.Val()
	return wasm.EqualValType(want, wasm.I32) || wasm.EqualValType(want, wasm.I64) || wasm.EqualValType(want, wasm.F32) || wasm.EqualValType(want, wasm.F64) || wasm.EqualValType(want, wasm.V128) || want.Kind() == wasm.ValRef
}

func rootedScalarAggregateDefaultableStorage(storage wasm.StorageType) bool {
	return rootedScalarAggregateSupportedStorage(storage) && (storage.Packed() || storage.Val().Kind() != wasm.ValRef || storage.Val().Ref().IsDefaultable())
}

func rootedScalarAggregateBoundedLength(r *wasm.Reader) bool {
	op, err := r.Byte()
	if err != nil || op != 0x41 {
		return false
	}
	length, err := r.I32()
	return err == nil && uint32(length) <= 256
}

func rootedScalarAggregateConstructor(r *wasm.Reader, sub, typeIndex uint32) bool {
	op, err := r.Byte()
	gotSub, subErr := r.U32()
	constructed, typeErr := r.U32()
	return err == nil && op == 0xfb && subErr == nil && gotSub == sub && typeErr == nil && constructed == typeIndex
}

func rootedScalarAggregateConstructorEnd(r *wasm.Reader, sub, typeIndex uint32) bool {
	if !rootedScalarAggregateConstructor(r, sub, typeIndex) {
		return false
	}
	end, endErr := r.Byte()
	return endErr == nil && end == 0x0b && !r.HasNext()
}

func rootedScalarAggregateReferenceOperand(r *wasm.Reader, storage wasm.StorageType, m *wasm.Module, allocations *int, depth int) bool {
	want := storage.Val()
	if storage.Packed() || want.Kind() != wasm.ValRef || want.Ref().Heap().Kind() != wasm.HeapTypeIndex || want.Ref().Heap().Type().Rec || *allocations >= 256 || depth >= 16 {
		return false
	}
	typeIndex := want.Ref().Heap().Type().Index
	comp := rootedScalarAggregateType(m, typeIndex)
	if comp == nil {
		return false
	}
	switch comp.Kind {
	case wasm.CompStruct:
		candidate := *r
		candidateAllocations := *allocations
		explicit := true
		for _, field := range comp.Fields {
			if !rootedScalarAggregateConstOperand(&candidate, field.Storage(), m, &candidateAllocations, depth+1) {
				explicit = false
				break
			}
		}
		if explicit && candidateAllocations < 256 && rootedScalarAggregateConstructor(&candidate, 0, typeIndex) {
			*r = candidate
			*allocations = candidateAllocations + 1
			return true
		}
		for _, field := range comp.Fields {
			if !rootedScalarAggregateDefaultableStorage(field.Storage()) {
				return false
			}
		}
		candidate = *r
		if rootedScalarAggregateConstructor(&candidate, 1, typeIndex) {
			*r = candidate
			(*allocations)++
			return true
		}
	case wasm.CompArray:
		candidate := *r
		candidateAllocations := *allocations
		count := uint32(0)
		for count < 256 {
			start := candidate
			if !rootedScalarAggregateConstOperand(&candidate, comp.Array.Storage(), m, &candidateAllocations, depth+1) {
				candidate = start
				break
			}
			count++
		}
		fixed := candidate
		declaredOK := rootedScalarAggregateConstructor(&fixed, 8, typeIndex)
		declared, declaredErr := fixed.U32()
		if declaredOK && declaredErr == nil && declared == count && candidateAllocations < 256 {
			*r = fixed
			*allocations = candidateAllocations + 1
			return true
		}
		candidate = *r
		candidateAllocations = *allocations
		if rootedScalarAggregateConstOperand(&candidate, comp.Array.Storage(), m, &candidateAllocations, depth+1) && rootedScalarAggregateBoundedLength(&candidate) && candidateAllocations < 256 && rootedScalarAggregateConstructor(&candidate, 6, typeIndex) {
			*r = candidate
			*allocations = candidateAllocations + 1
			return true
		}
		candidate = *r
		if rootedScalarAggregateDefaultableStorage(comp.Array.Storage()) && rootedScalarAggregateBoundedLength(&candidate) && rootedScalarAggregateConstructor(&candidate, 7, typeIndex) {
			*r = candidate
			(*allocations)++
			return true
		}
	default:
		return false
	}
	return false
}

func rootedScalarAggregateConstOperand(r *wasm.Reader, storage wasm.StorageType, m *wasm.Module, allocations *int, depth int) bool {
	start := *r
	op, err := r.Byte()
	if err == nil && rootedScalarAggregateConstOperandOpcode(r, storage, op, m) {
		return true
	}
	*r = start
	return rootedScalarAggregateReferenceOperand(r, storage, m, allocations, depth)
}

// Generic table initialization admits bounded aggregate constants composed of
// scalar/null literals and bounded nested reference constructors. The retained
// constant-expression evaluator supports more; this narrower source witness
// keeps instantiation ownership independently reviewable.
func rootedScalarAggregateElementExpression(expr wasm.Expr, ref wasm.RefType, m *wasm.Module) bool {
	if rootedScalarAggregateNullExpression(expr, ref) {
		return true
	}
	if len(expr.Instrs) != 0 || len(expr.BodyBytes) < 4 || len(expr.BodyBytes) > 4096 || ref.Heap().Kind() != wasm.HeapTypeIndex {
		return false
	}
	typeIndex := ref.Heap().Type().Index
	comp := rootedScalarAggregateType(m, typeIndex)
	if comp == nil {
		return false
	}
	r := wasm.ReaderFrom(expr.BodyBytes)
	allocations := 1
	switch comp.Kind {
	case wasm.CompStruct:
		scalarDefault := true
		for _, field := range comp.Fields {
			scalarDefault = scalarDefault && rootedScalarAggregateDefaultableStorage(field.Storage())
		}
		for _, field := range comp.Fields {
			if !rootedScalarAggregateConstOperand(&r, field.Storage(), m, &allocations, 1) {
				r = wasm.ReaderFrom(expr.BodyBytes)
				return scalarDefault && rootedScalarAggregateConstructorEnd(&r, 1, typeIndex)
			}
		}
		if rootedScalarAggregateConstructorEnd(&r, 0, typeIndex) {
			return true
		}
		r = wasm.ReaderFrom(expr.BodyBytes)
		return scalarDefault && rootedScalarAggregateConstructorEnd(&r, 1, typeIndex)
	case wasm.CompArray:
		if !rootedScalarAggregateSupportedStorage(comp.Array.Storage()) {
			return false
		}
		fixed := func() bool {
			fixedReader := wasm.ReaderFrom(expr.BodyBytes)
			fixedAllocations := allocations
			count := uint32(0)
			for {
				start := fixedReader
				if count < 256 && rootedScalarAggregateConstOperand(&fixedReader, comp.Array.Storage(), m, &fixedAllocations, 1) {
					count++
					continue
				}
				fixedReader = start
				op, err := fixedReader.Byte()
				if err != nil || op != 0xfb {
					return false
				}
				break
			}
			sub, subErr := fixedReader.U32()
			constructed, typeErr := fixedReader.U32()
			declared, countErr := fixedReader.U32()
			end, endErr := fixedReader.Byte()
			return subErr == nil && sub == 8 && typeErr == nil && constructed == typeIndex && countErr == nil && declared == count && endErr == nil && end == 0x0b && !fixedReader.HasNext()
		}
		if fixed() {
			return true
		}
		r = wasm.ReaderFrom(expr.BodyBytes)
		allocations = 1
		if rootedScalarAggregateConstOperand(&r, comp.Array.Storage(), m, &allocations, 1) && rootedScalarAggregateBoundedLength(&r) && rootedScalarAggregateConstructorEnd(&r, 6, typeIndex) {
			return true
		}
		r = wasm.ReaderFrom(expr.BodyBytes)
		return rootedScalarAggregateDefaultableStorage(comp.Array.Storage()) && rootedScalarAggregateBoundedLength(&r) && rootedScalarAggregateConstructorEnd(&r, 7, typeIndex)
	default:
		return false
	}
}

func admitRV64GCRootedScalarWire(data []byte) bool {
	if len(data) < 8 || len(data) > MaxModuleBytes || !bytes.Equal(data[:8], []byte{0, 'a', 's', 'm', 1, 0, 0, 0}) {
		return false
	}
	r := wasm.ReaderFrom(data[8:])
	const required = uint16(1<<1 | 1<<3 | 1<<7 | 1<<10)
	const allowed = required | 1<<2 | 1<<4 | 1<<5 | 1<<6 | 1<<8 | 1<<9 | 1<<11 | 1<<12
	var seen uint16
	var previousRank byte
	for r.HasNext() {
		id, err := r.Byte()
		rank := id
		if id == 12 {
			rank = 9 // data-count is ordered after start and before code
		}
		if err != nil || id > 12 || allowed&(1<<id) == 0 || seen&(1<<id) != 0 || rank <= previousRank {
			return false
		}
		n, err := r.U32()
		if err != nil || n == 0 || uint64(n) > uint64(r.BytesLeft()) {
			return false
		}
		start, end := r.Offset(), r.Offset()+int(n)
		payload := data[8+start : 8+end]
		probe := wasm.ReaderFrom(payload)
		count, err := probe.U32()
		if err != nil || (id == 8 || id == 12) && probe.HasNext() || id != 8 && id != 12 && count == 0 {
			return false
		}
		// Public decoding flattens a one-element explicit rec group into the same
		// module shape as an implicit singleton. Preserve that wire distinction by
		// parsing entry boundaries instead of scanning raw bytes: 0x4e is also a
		// perfectly valid vector length (78) inside a struct or function type.
		if id == 1 && !admitRV64GCRootedScalarTypeWire(payload) {
			return false
		}
		if err := r.JumpTo(end); err != nil {
			return false
		}
		seen |= 1 << id
		previousRank = rank
	}
	if seen&required != required {
		return false
	}
	m, err := wasm.DecodeModule(data)
	return err == nil && admitRV64GCRootedScalarModule(m)
}

// AdmitGCRootedScalarSource reports the bounded generic struct source witness.
// The decoded backend and root-plan authenticator independently recheck it.
func AdmitGCRootedScalarSource(data []byte) bool { return admitRV64GCRootedScalarWire(data) }

func admitRV64GCRootedScalarTypeWire(payload []byte) bool {
	r := wasm.ReaderFrom(payload)
	count, err := r.U32()
	if err != nil || count == 0 || count > MaxFunctions {
		return false
	}
	kinds := make([]byte, 0, count)
	valueTypes := func(owner uint32) bool {
		n, err := r.U32()
		if err != nil || n > 256 {
			return false
		}
		for range n {
			typ, err := r.Byte()
			if err != nil {
				return false
			}
			if typ >= 0x7c && typ <= 0x7f {
				continue
			}
			if rootedAbstractObjectHeap(wasm.AbsHeapType(typ)) {
				continue
			}
			if typ != 0x63 && typ != 0x64 {
				return false
			}
			heap, err := r.S33()
			if err != nil {
				return false
			}
			if heap < 0 {
				if !rootedAbstractObjectHeapS33(heap) {
					return false
				}
				continue
			}
			if uint64(heap) >= uint64(owner) || kinds[heap] != 0x5f && kinds[heap] != 0x5e {
				return false
			}
		}
		return true
	}
	storageType := func(owner uint32, references bool) bool {
		storage, err := r.Byte()
		if err != nil {
			return false
		}
		if storage == byte(wasm.PackI8) || storage == byte(wasm.PackI16) || storage >= 0x7c && storage <= 0x7f {
			return true
		}
		if references && storage == 0x70 { // funcref
			return true
		}
		if !references || storage != 0x63 && storage != 0x64 {
			return false
		}
		heap, err := r.S33()
		return err == nil && (heap == -16 || heap >= 0 && uint64(heap) < uint64(owner) && (kinds[heap] == 0x5f || kinds[heap] == 0x5e))
	}
	for owner := uint32(0); owner < count; owner++ {
		form, err := r.Byte()
		if err != nil {
			return false
		}
		switch form {
		case 0x5f:
			fields, err := r.U32()
			if err != nil || fields == 0 || fields > 255 {
				return false
			}
			for range fields {
				if !storageType(owner, true) {
					return false
				}
				mut, err := r.Byte()
				if err != nil || mut > 1 {
					return false
				}
			}
		case 0x60:
			if !valueTypes(owner) || !valueTypes(owner) {
				return false
			}
		case 0x5e:
			if !storageType(owner, true) {
				return false
			}
			mut, err := r.Byte()
			if err != nil || mut > 1 {
				return false
			}
		default:
			// Includes the explicit rec-group marker 0x4e and subtype prefixes.
			return false
		}
		kinds = append(kinds, form)
	}
	return !r.HasNext()
}

func rootedScalarHelperSlots(m *wasm.Module) int {
	slots := 4 // struct.set is the largest non-allocation helper.
	if m == nil {
		return slots
	}
	for _, group := range m.Types {
		if len(group.SubTypes) != 1 || group.SubTypes[0].Comp.Kind != wasm.CompStruct {
			continue
		}
		slots = max(slots, len(group.SubTypes[0].Comp.Fields)+1)
	}
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	for i := range m.Code {
		r := wasm.NewReader(m.Code[i].BodyBytes)
		for r.HasNext() {
			op, err := r.Byte()
			if err != nil {
				return 65
			}
			var imm wasm.InstructionImmediate
			if err = classifier.ClassifyInto(r, op, &imm); err != nil {
				return 65
			}
			if imm.Kind == wasm.InstrArrayNewFixed {
				if imm.Index2 > maxRootedScalarArrayFixed {
					return maxRootedScalarArrayFixed + 3
				}
				slots = max(slots, int(imm.Index2)+2)
			}
			switch imm.Kind {
			case wasm.InstrTableInit:
				slots = max(slots, 5)
			case wasm.InstrTableFill:
				slots = max(slots, 4)
			case wasm.InstrTableCopy:
				slots = max(slots, 5)
			}
		}
	}
	return slots
}

func rootedScalarHelperProfile(m *wasm.Module) (structs, arrays, ok bool) {
	if m == nil {
		return false, false, false
	}
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	for i := range m.Code {
		r := wasm.NewReader(m.Code[i].BodyBytes)
		for r.HasNext() {
			op, err := r.Byte()
			if err != nil {
				return false, false, false
			}
			var imm wasm.InstructionImmediate
			if err = classifier.ClassifyInto(r, op, &imm); err != nil {
				return false, false, false
			}
			switch imm.Kind {
			case wasm.InstrStructNew, wasm.InstrStructNewDefault, wasm.InstrStructGet,
				wasm.InstrStructGetS, wasm.InstrStructGetU, wasm.InstrStructSet:
				structs = true
			case wasm.InstrRefTest, wasm.InstrRefCast, wasm.InstrBrOnCast, wasm.InstrBrOnCastFail:
				structs = true
			case wasm.InstrArrayNew, wasm.InstrArrayNewDefault, wasm.InstrArrayNewFixed,
				wasm.InstrArrayGet, wasm.InstrArrayGetS, wasm.InstrArrayGetU,
				wasm.InstrArraySet, wasm.InstrArrayLen, wasm.InstrArrayFill,
				wasm.InstrArrayCopy, wasm.InstrArrayInitData, wasm.InstrArrayInitElem,
				wasm.InstrArrayNewData, wasm.InstrArrayNewElem:
				arrays = true
			}
		}
	}
	return structs, arrays, true
}
