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
		// Reference imports are provisionally restricted to GC carriers here,
		// then matched against the fully flattened rooted type directory below;
		// exact references are limited there to prior indexed aggregates. Imported
		// functions must not become reachable through private tables.
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
						if !scalarType(typ, true) && !wasm.EqualValType(typ, wasm.V128) && typ.Kind() != wasm.ValRef {
							return false
						}
					}
				}
			case wasm.ExternGlobal:
				typ := im.Type.GlobalType().Type
				if !wasm.EqualValType(typ, wasm.I32) && !wasm.EqualValType(typ, wasm.I64) && !wasm.EqualValType(typ, wasm.F32) && !wasm.EqualValType(typ, wasm.F64) && !rootedScalarGlobalReference(typ, m) {
					return false
				}
			case wasm.ExternMem:
				memory := im.Type.MemType()
				if memory.Shared || memory.Limits.Addr64 || !memory.Limits.HasMax || memory.Limits.Min > memory.Limits.Max || memory.Limits.Max > 256 {
					return false
				}
			case wasm.ExternTable:
				table := im.Type.TableType()
				elem := wasm.RefVal(table.Ref)
				if !wasm.EqualValType(elem, wasm.FuncRef) && !rootedScalarTableReference(elem, m) || table.Limits.Addr64 || !table.Limits.HasMax ||
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
	remainingElements := 256
	retainedElementSegments := len(m.Elements)
	aggregateParser := newRootedScalarAggregateParser(m)
	for i := range m.Tables {
		table := &m.Tables[i]
		elem := wasm.RefVal(table.Type.Ref)
		rootedTable := rootedScalarTableReference(elem, m)
		if (!wasm.EqualValType(elem, wasm.FuncRef) && !rootedTable) || table.Type.Limits.Addr64 || !table.Type.Limits.HasMax ||
			table.Type.Limits.Min > table.Type.Limits.Max || table.Type.Limits.Max > 256 {
			return false
		}
		if table.Init != nil {
			if rootedTable {
				if !rootedScalarAggregateElementExpressionWithParser(*table.Init, table.Type.Ref, aggregateParser, importedGlobals+len(m.Globals)) {
					return false
				}
				if !rootedScalarAggregateNullExpression(*table.Init, table.Type.Ref) {
					retainedElementSegments++
					if table.Type.Limits.Min == 0 || retainedElementSegments > 256 || table.Type.Limits.Min > uint64(remainingElements) {
						return false
					}
					remainingElements -= int(table.Type.Limits.Min)
				}
				continue
			}
			value, err := tableElementExpression(*table.Init)
			public := publicTables[importedTables+i]
			if err != nil || !value.Null && ((!public && uint64(value.FuncIndex) < uint64(importedFunctions)) || uint64(value.FuncIndex) >= uint64(importedFunctions+len(m.FuncTypes))) {
				return false
			}
		}
	}
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
					if !rootedScalarAggregateElementExpressionWithParser(expr, elem.Kind.Ref, aggregateParser, importedGlobals+len(m.Globals)) {
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
			if !rootedScalarAggregateGlobal(*global, aggregateParser, importedGlobals+i) {
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
			if _, ok := m.GlobalTypeByIndex(export.Index.Index); !ok {
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
		if scalarType(typ, true) || wasm.EqualValType(typ, wasm.V128) {
			return true
		}
		if typ.Kind() != wasm.ValRef {
			return false
		}
		heap := typ.Ref().Heap()
		if heap.Kind() == wasm.HeapAbs {
			return !typ.Ref().Exact() && (rootedAbstractObjectHeap(heap.Abs()) || heap.Abs() == wasm.HeapI31)
		}
		return heap.Kind() == wasm.HeapTypeIndex && !heap.Type().Rec && int(heap.Type().Index) < flatTypes && admittedAggregates[heap.Type().Index]
	}
	for i := range m.Types {
		group := &m.Types[i]
		if len(group.SubTypes) == 0 || flatTypes+len(group.SubTypes) > MaxFunctions {
			return false
		}
		multi := len(group.SubTypes) > 1
		for member := range group.SubTypes {
			sub := &group.SubTypes[member]
			if sub.Metadata.Describes.Present() || sub.Metadata.Descriptor.Present() {
				return false
			}
			aggregateSubtype := sub.Comp.Kind == wasm.CompStruct || sub.Comp.Kind == wasm.CompArray
			if multi && (!aggregateSubtype || sub.HasPrefix || !sub.Final || len(sub.Supers) != 0) {
				return false
			}
			if !sub.HasPrefix {
				if !sub.Final || len(sub.Supers) != 0 {
					return false
				}
			} else if !aggregateSubtype || len(sub.Supers) > 1 {
				return false
			} else if len(sub.Supers) == 1 {
				super := sub.Supers[0]
				if super.Rec || int(super.Index) >= flatTypes || !admittedAggregates[super.Index] {
					return false
				}
				superComp := rootedScalarAggregateType(m, super.Index)
				if superComp == nil || superComp.Kind != sub.Comp.Kind {
					return false
				}
			}
			rootedStorage := func(storage wasm.StorageType) bool {
				if storage.Packed() {
					return storage.Pack() == wasm.PackI8 || storage.Pack() == wasm.PackI16
				}
				if scalarType(storage.Val(), true) {
					return true
				}
				value := storage.Val()
				if value.Kind() != wasm.ValRef {
					return false
				}
				heap := value.Ref().Heap()
				if !value.Ref().Exact() && heap.Kind() == wasm.HeapAbs && heap.Abs() == wasm.HeapI31 {
					return true
				}
				if !value.Ref().Exact() && heap.Kind() == wasm.HeapAbs && heap.Abs() == wasm.HeapFunc {
					return true
				}
				if heap.Kind() != wasm.HeapTypeIndex || value.Ref().Exact() && heap.Type().Rec {
					return false
				}
				if heap.Type().Rec {
					return int(heap.Type().Index) < len(group.SubTypes) &&
						(group.SubTypes[heap.Type().Index].Comp.Kind == wasm.CompStruct || group.SubTypes[heap.Type().Index].Comp.Kind == wasm.CompArray)
				}
				return int(heap.Type().Index) < flatTypes && admittedAggregates[heap.Type().Index]
			}
			switch sub.Comp.Kind {
			case wasm.CompStruct:
				if len(sub.Comp.Fields) == 0 || len(sub.Comp.Fields) > 255 {
					return false
				}
				for _, field := range sub.Comp.Fields {
					storage := field.Storage()
					if !rootedStorage(storage) {
						return false
					}
				}
				admittedAggregates = append(admittedAggregates, true)
				hasAggregate = true
			case wasm.CompArray:
				storage := sub.Comp.Array.Storage()
				if !rootedStorage(storage) {
					return false
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
	// The shared public host adapter translates collector objects through owned
	// GC tokens and keeps i31 values immediate. Tokens always occupy more than
	// 32 bits, so abstract object carriers can safely distinguish the two forms.
	publicRootedValue := func(typ wasm.ValType) bool {
		if scalarType(typ, true) || wasm.EqualValType(typ, wasm.V128) {
			return true
		}
		if typ.Kind() != wasm.ValRef {
			return false
		}
		heap := typ.Ref().Heap()
		if heap.Kind() == wasm.HeapAbs {
			return !typ.Ref().Exact() && (rootedAbstractObjectHeap(heap.Abs()) || heap.Abs() == wasm.HeapI31)
		}
		return heap.Kind() == wasm.HeapTypeIndex && !heap.Type().Rec && int(heap.Type().Index) < flatTypes && admittedAggregates[heap.Type().Index]
	}
	for i := range m.Imports {
		if m.Imports[i].Type.Kind != wasm.ExternFunc {
			continue
		}
		ft, ok := m.ImportFuncType(i)
		if !ok {
			return false
		}
		for _, values := range [][]wasm.ValType{ft.Params, ft.Results} {
			for _, typ := range values {
				if !publicRootedValue(typ) {
					return false
				}
			}
		}
	}
	for i := range m.Exports {
		if m.Exports[i].Index.Kind == wasm.ExternGlobal {
			global, ok := m.GlobalTypeByIndex(m.Exports[i].Index.Index)
			if !ok || global.Type.Kind() == wasm.ValRef && !publicRootedValue(global.Type) {
				return false
			}
			continue
		}
		if m.Exports[i].Index.Kind == wasm.ExternMem || m.Exports[i].Index.Kind == wasm.ExternTable {
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
				if !publicRootedValue(typ) {
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
	comp := rootedScalarAggregateType(m, index)
	if comp == nil {
		return false
	}
	return comp.Kind == wasm.CompStruct || comp.Kind == wasm.CompArray
}

func rootedScalarTableReference(typ wasm.ValType, m *wasm.Module) bool {
	if typ.Kind() != wasm.ValRef {
		return false
	}
	ref := typ.Ref()
	heap := ref.Heap()
	if !ref.Nullable() || ref.Exact() {
		return false
	}
	switch heap.Kind() {
	case wasm.HeapAbs:
		switch heap.Abs() {
		case wasm.HeapAny, wasm.HeapEq, wasm.HeapI31, wasm.HeapStruct, wasm.HeapArray:
			return true
		default:
			return false
		}
	case wasm.HeapTypeIndex:
		return !heap.Type().Rec && rootedScalarAggregateAt(m, heap.Type().Index)
	default:
		return false
	}
}

func rootedScalarGlobalReference(typ wasm.ValType, m *wasm.Module) bool {
	if rootedScalarTableReference(typ, m) {
		return true
	}
	if typ.Kind() != wasm.ValRef {
		return false
	}
	ref := typ.Ref()
	if !ref.Nullable() || ref.Exact() || ref.Heap().Kind() != wasm.HeapAbs {
		return false
	}
	switch ref.Heap().Abs() {
	case wasm.HeapAny, wasm.HeapEq, wasm.HeapStruct, wasm.HeapArray:
		return true
	default:
		return false
	}
}

func rootedScalarAggregateNullExpression(expr wasm.Expr, ref wasm.RefType) bool {
	if len(expr.Instrs) != 0 || len(expr.BodyBytes) < 3 || len(expr.BodyBytes) > 7 {
		return false
	}
	r := wasm.ReaderFrom(expr.BodyBytes)
	op, err := r.Byte()
	heap, heapErr := r.S33()
	end, endErr := r.Byte()
	if err != nil || op != 0xd0 || heapErr != nil || endErr != nil || end != 0x0b || r.HasNext() {
		return false
	}
	switch ref.Heap().Kind() {
	case wasm.HeapAbs:
		return heap < 0 && wasm.AbsHeapType(byte(heap&0x7f)) == ref.Heap().Abs()
	case wasm.HeapTypeIndex:
		return heap >= 0 && uint64(heap) == uint64(ref.Heap().Type().Index)
	default:
		return false
	}
}

func rootedScalarAggregateType(m *wasm.Module, index uint32) *wasm.CompType {
	if m == nil {
		return nil
	}
	sub, ok := m.TypeSubType(index)
	if !ok {
		return nil
	}
	return &sub.Comp
}

func rootedScalarFlatTypeCount(m *wasm.Module) int {
	if m == nil {
		return 0
	}
	total := 0
	for i := range m.Types {
		total += len(m.Types[i].SubTypes)
	}
	return total
}

type rootedScalarAggregateParser struct {
	m           *wasm.Module
	valid       bool
	entry, exit []uint32
}

func newRootedScalarAggregateParser(m *wasm.Module) *rootedScalarAggregateParser {
	p := &rootedScalarAggregateParser{m: m}
	flatCount := rootedScalarFlatTypeCount(m)
	if m == nil || flatCount == 0 || flatCount > MaxFunctions {
		return p
	}
	children := make([][]uint32, flatCount)
	roots := make([]uint32, 0, flatCount)
	flat := uint32(0)
	for gi := range m.Types {
		group := &m.Types[gi]
		for si := range group.SubTypes {
			sub := &group.SubTypes[si]
			if len(sub.Supers) == 0 {
				roots = append(roots, flat)
				flat++
				continue
			}
			if len(sub.Supers) != 1 || sub.Supers[0].Rec || sub.Supers[0].Index >= flat {
				return p
			}
			children[sub.Supers[0].Index] = append(children[sub.Supers[0].Index], flat)
			flat++
		}
	}
	p.entry = make([]uint32, flatCount)
	p.exit = make([]uint32, flatCount)
	clock := uint32(0)
	var visit func(uint32)
	visit = func(index uint32) {
		p.entry[index] = clock
		clock++
		for _, child := range children[index] {
			visit(child)
		}
		p.exit[index] = clock
	}
	for _, root := range roots {
		visit(root)
	}
	p.valid = clock == uint32(flatCount)
	return p
}

func (p *rootedScalarAggregateParser) constructedSubtype(typeIndex uint32, required wasm.RefType) bool {
	if p == nil || !p.valid {
		return false
	}
	if required.Heap().Kind() == wasm.HeapAbs {
		actual := wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: typeIndex}), false)
		return p.m.ReferenceTypeSubtype(actual, required)
	}
	if required.Heap().Kind() != wasm.HeapTypeIndex || required.Heap().Type().Rec {
		return false
	}
	target := required.Heap().Type().Index
	if uint64(typeIndex) >= uint64(len(p.entry)) || uint64(target) >= uint64(len(p.entry)) {
		return false
	}
	actualComp, targetComp := rootedScalarAggregateType(p.m, typeIndex), rootedScalarAggregateType(p.m, target)
	if actualComp == nil || targetComp == nil || actualComp.Kind != targetComp.Kind || actualComp.Kind != wasm.CompStruct && actualComp.Kind != wasm.CompArray {
		return false
	}
	if required.Exact() {
		return typeIndex == target
	}
	return p.entry[target] <= p.entry[typeIndex] && p.entry[typeIndex] < p.exit[target]
}

type rootedScalarAggregateStackValue struct {
	typ         wasm.ValType
	constructed bool
	typeIndex   uint32
	depth       int
	knownI32    bool
	i32         uint32
}

func (p *rootedScalarAggregateParser) valueMatches(value rootedScalarAggregateStackValue, storage wasm.StorageType, owner uint32) bool {
	want := storage.Val()
	if storage.Packed() {
		want = wasm.I32
	}
	if want.Kind() == wasm.ValRef {
		ref := want.Ref()
		heap := ref.Heap()
		if heap.Kind() == wasm.HeapTypeIndex && heap.Type().Rec {
			if heap.Type().Index != 0 {
				return false
			}
			want = wasm.RefVal(wasm.Ref(ref.Nullable(), wasm.IndexedHeap(wasm.TypeIdx{Index: owner}), ref.Exact()))
		}
	}
	if want.Kind() != wasm.ValRef {
		return wasm.EqualValType(value.typ, want)
	}
	if value.typ.Kind() != wasm.ValRef {
		return false
	}
	if value.constructed {
		return p.constructedSubtype(value.typeIndex, want.Ref())
	}
	return p.m.ReferenceTypeSubtype(value.typ.Ref(), want.Ref())
}

func (p *rootedScalarAggregateParser) validateExpression(expr wasm.Expr, required wasm.RefType, globalLimit int) bool {
	if p == nil || !p.valid || globalLimit < 0 || globalLimit > p.m.ImportedGlobalCount()+len(p.m.Globals) ||
		len(expr.Instrs) != 0 || len(expr.BodyBytes) < 3 || len(expr.BodyBytes) > 4096 {
		return false
	}
	var stack [512]rootedScalarAggregateStackValue
	stackLen, allocations, work := 0, 0, 0
	push := func(value rootedScalarAggregateStackValue) bool {
		if stackLen == len(stack) {
			return false
		}
		stack[stackLen] = value
		stackLen++
		return true
	}
	r := wasm.ReaderFrom(expr.BodyBytes)
	for r.HasNext() {
		work++
		if work > rootedScalarAggregateParseWork {
			return false
		}
		op, err := r.Byte()
		if err != nil {
			return false
		}
		value := rootedScalarAggregateStackValue{}
		switch op {
		case 0x41:
			var literal int32
			literal, err = r.I32()
			value.typ, value.knownI32, value.i32 = wasm.I32, true, uint32(literal)
		case 0x42:
			_, err = r.I64()
			value.typ = wasm.I64
		case 0x43:
			_, err = r.Bytes(4)
			value.typ = wasm.F32
		case 0x44:
			_, err = r.Bytes(8)
			value.typ = wasm.F64
		case 0x23:
			index, indexErr := r.U32()
			global, ok := p.m.GlobalTypeByIndex(index)
			if indexErr != nil || int(index) >= globalLimit || !ok || global.Mutable {
				return false
			}
			value.typ = global.Type
		case 0xd0:
			heap, heapErr := r.S33()
			if heapErr != nil {
				return false
			}
			if heap >= 0 {
				value.typ = wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: uint32(heap)}), false))
			} else {
				value.typ = wasm.RefVal(wasm.Ref(true, wasm.AbsHeap(wasm.AbsHeapType(byte(heap&0x7f))), false))
			}
		case 0xd2:
			index, indexErr := r.U32()
			if indexErr != nil || index < uint32(p.m.ImportedFuncCount()) {
				return false
			}
			typeIndex, ok := p.m.FuncTypeIndex(index)
			if !ok || typeIndex.Rec {
				return false
			}
			value.typ = wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(typeIndex), false))
		case 0xfd:
			sub, subErr := r.U32()
			if subErr != nil || sub != 12 {
				return false
			}
			_, err = r.Bytes(16)
			value.typ = wasm.V128
		case 0xfb:
			sub, subErr := r.U32()
			if subErr != nil {
				return false
			}
			if sub == 28 { // ref.i31
				if stackLen < 1 || !wasm.EqualValType(stack[stackLen-1].typ, wasm.I32) {
					return false
				}
				stackLen--
				value.typ = wasm.RefVal(wasm.Ref(false, wasm.AbsHeap(wasm.HeapI31), false))
				break
			}
			typeIndex, typeErr := r.U32()
			if typeErr != nil {
				return false
			}
			comp := rootedScalarAggregateType(p.m, typeIndex)
			if comp == nil || allocations == 256 {
				return false
			}
			base, constructorDepth := stackLen, 1
			switch sub {
			case 0:
				if comp.Kind != wasm.CompStruct || len(comp.Fields) > stackLen {
					return false
				}
				base = stackLen - len(comp.Fields)
				for i := range comp.Fields {
					if !p.valueMatches(stack[base+i], comp.Fields[i].Storage(), typeIndex) {
						return false
					}
					constructorDepth = max(constructorDepth, stack[base+i].depth+1)
				}
			case 1:
				if comp.Kind != wasm.CompStruct {
					return false
				}
				for i := range comp.Fields {
					if !rootedScalarAggregateDefaultableStorage(comp.Fields[i].Storage()) {
						return false
					}
				}
			case 6:
				if comp.Kind != wasm.CompArray || stackLen < 2 || !p.valueMatches(stack[stackLen-2], comp.Array.Storage(), typeIndex) ||
					!wasm.EqualValType(stack[stackLen-1].typ, wasm.I32) || !stack[stackLen-1].knownI32 || stack[stackLen-1].i32 > 256 {
					return false
				}
				base = stackLen - 2
				constructorDepth = max(constructorDepth, stack[base].depth+1)
			case 7:
				if comp.Kind != wasm.CompArray || stackLen < 1 || !rootedScalarAggregateDefaultableStorage(comp.Array.Storage()) ||
					!wasm.EqualValType(stack[stackLen-1].typ, wasm.I32) || !stack[stackLen-1].knownI32 || stack[stackLen-1].i32 > 256 {
					return false
				}
				base = stackLen - 1
			case 8:
				count, countErr := r.U32()
				if comp.Kind != wasm.CompArray || countErr != nil || count > 256 || int(count) > stackLen {
					return false
				}
				base = stackLen - int(count)
				for i := base; i < stackLen; i++ {
					if !p.valueMatches(stack[i], comp.Array.Storage(), typeIndex) {
						return false
					}
					constructorDepth = max(constructorDepth, stack[i].depth+1)
				}
			default:
				return false
			}
			if constructorDepth > 16 {
				return false
			}
			stackLen = base
			allocations++
			value = rootedScalarAggregateStackValue{
				typ:         wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: typeIndex}), false)),
				constructed: true, typeIndex: typeIndex, depth: constructorDepth,
			}
		case 0x0b:
			if r.HasNext() || stackLen != 1 || stack[0].typ.Kind() != wasm.ValRef {
				return false
			}
			if stack[0].constructed {
				return p.constructedSubtype(stack[0].typeIndex, required)
			}
			return p.m.ReferenceTypeSubtype(stack[0].typ.Ref(), required)
		default:
			return false
		}
		if err != nil || !push(value) {
			return false
		}
	}
	return false
}

const rootedScalarAggregateParseWork = 8192

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

// Generic table initialization admits bounded aggregate constants composed of
// scalar/null literals and bounded nested reference constructors. The retained
// constant-expression evaluator supports more; this narrower source witness
// keeps instantiation ownership independently reviewable.
func rootedScalarAggregateElementExpression(expr wasm.Expr, ref wasm.RefType, m *wasm.Module) bool {
	return rootedScalarAggregateElementExpressionWithParser(expr, ref, newRootedScalarAggregateParser(m), m.ImportedGlobalCount()+len(m.Globals))
}

func rootedScalarAggregateElementExpressionWithParser(expr wasm.Expr, ref wasm.RefType, p *rootedScalarAggregateParser, globalLimit int) bool {
	return p.validateExpression(expr, ref, globalLimit)
}

func rootedScalarAggregateGlobal(global wasm.Global, p *rootedScalarAggregateParser, globalLimit int) bool {
	if global.Type.Type.Kind() != wasm.ValRef {
		return false
	}
	ref := global.Type.Type.Ref()
	heap := ref.Heap()
	if ref.Exact() {
		return false
	}
	switch heap.Kind() {
	case wasm.HeapAbs:
		if !ref.Nullable() {
			return false
		}
		switch heap.Abs() {
		case wasm.HeapAny, wasm.HeapEq, wasm.HeapI31, wasm.HeapStruct, wasm.HeapArray:
			return p.validateExpression(global.Init, ref, globalLimit)
		default:
			return false
		}
	case wasm.HeapTypeIndex:
		return !heap.Type().Rec && rootedScalarAggregateAt(p.m, heap.Type().Index) && p.validateExpression(global.Init, ref, globalLimit)
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
	kinds := make([]byte, 0, MaxFunctions)
	finals := make([]bool, 0, MaxFunctions)
	valueTypes := func(owner, groupBase, groupLimit uint32) bool {
		n, err := r.U32()
		if err != nil || n > 256 {
			return false
		}
		for range n {
			typ, err := r.Byte()
			if err != nil {
				return false
			}
			if typ >= 0x7b && typ <= 0x7f {
				continue
			}
			if rootedAbstractObjectHeap(wasm.AbsHeapType(typ)) || wasm.AbsHeapType(typ) == wasm.HeapI31 {
				continue
			}
			if typ != 0x63 && typ != 0x64 {
				return false
			}
			exact := false
			if marker, ok := r.Peek(); ok && marker == 0x62 {
				_, _ = r.Byte()
				exact = true
			}
			heap, err := r.S33()
			if err != nil {
				return false
			}
			if heap < 0 {
				if exact || !rootedAbstractObjectHeapS33(heap) && heap != -20 {
					return false
				}
				continue
			}
			if uint64(heap) >= uint64(groupBase) && uint64(heap) < uint64(groupLimit) {
				if exact {
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
	storageType := func(owner, groupBase, groupLimit uint32, references bool) bool {
		storage, err := r.Byte()
		if err != nil {
			return false
		}
		if storage == byte(wasm.PackI8) || storage == byte(wasm.PackI16) || storage >= 0x7c && storage <= 0x7f {
			return true
		}
		if references && (storage == 0x70 || storage == byte(wasm.HeapI31)) { // funcref or i31ref
			return true
		}
		if !references || storage != 0x63 && storage != 0x64 {
			return false
		}
		exact := false
		if marker, ok := r.Peek(); ok && marker == 0x62 {
			_, _ = r.Byte()
			exact = true
		}
		heap, err := r.S33()
		if err != nil {
			return false
		}
		if !exact && (heap == -16 || heap == -20) {
			return true
		}
		if heap < 0 {
			return false
		}
		if uint64(heap) >= uint64(groupBase) && uint64(heap) < uint64(groupLimit) {
			return !exact
		}
		return uint64(heap) < uint64(owner) && (kinds[heap] == 0x5f || kinds[heap] == 0x5e)
	}
	parseSubtype := func(owner, groupBase, groupLimit uint32, multi bool) bool {
		form, err := r.Byte()
		if err != nil {
			return false
		}
		compForm, final, prefixed, superKind := form, true, false, byte(0)
		if form == 0x4f || form == 0x50 {
			if multi {
				return false
			}
			prefixed, final = true, form == 0x4f
			supers, err := r.U32()
			if err != nil || supers > 1 {
				return false
			}
			if supers == 1 {
				super, err := r.S33()
				if err != nil || super < 0 || uint64(super) >= uint64(owner) || finals[super] || kinds[super] != 0x5f && kinds[super] != 0x5e {
					return false
				}
				superKind = kinds[super]
			}
			compForm, err = r.Byte()
			if err != nil {
				return false
			}
		}
		switch compForm {
		case 0x5f:
			fields, err := r.U32()
			if err != nil || fields == 0 || fields > 255 {
				return false
			}
			for range fields {
				if !storageType(owner, groupBase, groupLimit, true) {
					return false
				}
				mut, err := r.Byte()
				if err != nil || mut > 1 {
					return false
				}
			}
		case 0x60:
			if multi || prefixed || !valueTypes(owner, groupBase, groupLimit) || !valueTypes(owner, groupBase, groupLimit) {
				return false
			}
		case 0x5e:
			if !storageType(owner, groupBase, groupLimit, true) {
				return false
			}
			mut, err := r.Byte()
			if err != nil || mut > 1 {
				return false
			}
		default:
			return false
		}
		if superKind != 0 && compForm != superKind {
			return false
		}
		kinds = append(kinds, compForm)
		finals = append(finals, final)
		return true
	}
	for range count {
		groupBase := uint32(len(kinds))
		members := uint32(1)
		if marker, ok := r.Peek(); ok && marker == 0x4e {
			_, _ = r.Byte()
			members, err = r.U32()
			if err != nil || members < 2 || uint64(groupBase)+uint64(members) > MaxFunctions {
				return false
			}
		}
		groupLimit := groupBase + members
		for member := uint32(0); member < members; member++ {
			if !parseSubtype(groupBase+member, groupBase, groupLimit, members > 1) {
				return false
			}
		}
	}
	return !r.HasNext()
}

func rootedScalarHelperSlots(m *wasm.Module) int {
	slots := 4 // struct.set is the largest non-allocation helper.
	if m == nil {
		return slots
	}
	for _, group := range m.Types {
		for i := range group.SubTypes {
			if group.SubTypes[i].Comp.Kind == wasm.CompStruct {
				slots = max(slots, len(group.SubTypes[i].Comp.Fields)+1)
			}
		}
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
