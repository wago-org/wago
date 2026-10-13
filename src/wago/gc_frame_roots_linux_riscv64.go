//go:build linux && riscv64 && !tinygo && !wago_precompiled

package wago

import (
	"fmt"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

// newGCFrameRootPlan is the bounded RV64 exact-root producer for rooted scalar
// modules. Allocation helpers publish safepoint roots and Wasm/host calls
// publish return-PC caller-frame maps.
func newGCFrameRootPlan(m *wasm.Module, exactRoots, genericExecution bool, diagnostic *string, analysis *wasm.ValidatedModuleAnalysis) *shared.GCModuleFrameRootPlan {
	if diagnostic != nil {
		*diagnostic = ""
	}
	if !exactRoots {
		return nil
	}
	if !genericExecution {
		// Exact RV64 products retain their existing persistent-root proofs until
		// their product witnesses are deliberately migrated to frame maps.
		return nil
	}
	reject := func(format string, args ...any) *shared.GCModuleFrameRootPlan {
		if diagnostic != nil {
			*diagnostic = fmt.Sprintf(format, args...)
		}
		return nil
	}
	if m == nil || len(m.Code) == 0 {
		return reject("generic GC module has no local function bodies")
	}
	importedFunctions := m.ImportedFuncCount()
	importedGlobals := m.ImportedGlobalCount()
	importedMemories := m.ImportedMemCount()
	importedTables := m.ImportedTableCount()
	memoryCount := importedMemories + len(m.Memories)
	tableCount := importedTables + len(m.Tables)
	if importedFunctions+importedGlobals+importedMemories+importedTables != len(m.Imports) || importedFunctions > railshot.MaxHostFunctionImports || importedFunctions+len(m.Code) > railshot.MaxFunctions || len(m.Tables) > 2 || tableCount > 2 || memoryCount > rv64RootedMaxMemories || len(m.Tags) != 0 || importedGlobals+len(m.Globals) > 256 || len(m.Elements) > 256 || len(m.Data) > 256 || len(m.StringRefs) != 0 {
		return reject("RV64 exact roots require a bounded scalar allocation module")
	}
	rootedImportValue := func(typ wasm.ValType) bool {
		if wasm.EqualValType(typ, wasm.I32) || wasm.EqualValType(typ, wasm.I64) || wasm.EqualValType(typ, wasm.F32) || wasm.EqualValType(typ, wasm.F64) {
			return true
		}
		if typ.Kind() != wasm.ValRef || typ.Ref().Exact() {
			return false
		}
		heap := typ.Ref().Heap()
		if heap.Kind() == wasm.HeapAbs {
			return heap.Abs() == wasm.HeapI31
		}
		if heap.Kind() != wasm.HeapTypeIndex || heap.Type().Rec {
			return false
		}
		target, ok := m.TypeSubType(heap.Type().Index)
		return ok && (target.Comp.Kind == wasm.CompStruct || target.Comp.Kind == wasm.CompArray)
	}
	if len(m.Imports) != 0 {
		for i := range m.Imports {
			im := &m.Imports[i]
			switch im.Type.Kind {
			case wasm.ExternFunc:
				ft, ok := m.ImportFuncType(i)
				if !ok || len(ft.Params) > coreruntime.MaxHostArity || len(ft.Results) > coreruntime.MaxHostArity {
					return reject("RV64 exact roots reject function import %d", i)
				}
				for _, values := range [][]wasm.ValType{ft.Params, ft.Results} {
					for _, typ := range values {
						if !rootedImportValue(typ) {
							return reject("RV64 exact roots reject unsupported function import %d", i)
						}
					}
				}
			case wasm.ExternGlobal:
				typ := im.Type.GlobalType().Type
				if !wasm.EqualValType(typ, wasm.I32) && !wasm.EqualValType(typ, wasm.I64) && !wasm.EqualValType(typ, wasm.F32) && !wasm.EqualValType(typ, wasm.F64) {
					return reject("RV64 exact roots reject non-scalar global import %d", i)
				}
			case wasm.ExternMem:
				memory := im.Type.MemType()
				if memory.Shared || memory.Limits.Addr64 || !memory.Limits.HasMax || memory.Limits.Min > memory.Limits.Max || memory.Limits.Max > 256 {
					return reject("RV64 exact roots reject memory import %d", i)
				}
			case wasm.ExternTable:
				table := im.Type.TableType()
				if !wasm.EqualValType(wasm.RefVal(table.Ref), wasm.FuncRef) || table.Limits.Addr64 || !table.Limits.HasMax || table.Limits.Min > table.Limits.Max || table.Limits.Max > 256 {
					return reject("RV64 exact roots reject table import %d", i)
				}
			default:
				return reject("RV64 exact roots reject resource import %d", i)
			}
		}
	}
	if m.DataCount != nil && (uint64(*m.DataCount) != uint64(len(m.Data)) || *m.DataCount > 256) {
		return reject("RV64 exact roots require an exact bounded data count")
	}
	if m.Start != nil {
		sig, ok := m.FuncSignature(uint32(*m.Start))
		if !ok || sig == nil || len(sig.Params) != 0 || len(sig.Results) != 0 {
			return reject("RV64 exact roots require a bounded void start")
		}
	}
	aggregateParser := newRV64RootedAggregateParser(m)
	for i := range m.Globals {
		if !rv64RootedScalarGlobal(m, &m.Globals[i], importedGlobals+i, aggregateParser) {
			return reject("RV64 exact roots require a bounded scalar or aggregate constant global %d", i)
		}
	}
	for i := range m.Memories {
		memory := m.Memories[i]
		if memory.Shared || memory.Limits.Addr64 || !memory.Limits.HasMax || memory.Limits.Min > memory.Limits.Max || memory.Limits.Max > 256 {
			return reject("RV64 exact roots require bounded local memory32 %d", i)
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
	for i := range m.Tables {
		table := &m.Tables[i]
		elem := wasm.RefVal(table.Type.Ref)
		rootedTable := rv64RootedAggregateTableReference(m, elem)
		if (!wasm.EqualValType(elem, wasm.FuncRef) && !rootedTable) || table.Type.Limits.Addr64 || !table.Type.Limits.HasMax ||
			table.Type.Limits.Min > table.Type.Limits.Max || table.Type.Limits.Max > 256 {
			return reject("RV64 exact roots require a bounded local function or aggregate-reference table32")
		}
		if rootedTable && publicTables[importedTables+i] {
			return reject("RV64 exact roots reject public aggregate-reference table %d", i)
		}
		if table.Init != nil {
			if rootedTable {
				if !rv64RootedAggregateElement(*table.Init, table.Type.Ref, aggregateParser, importedGlobals+len(m.Globals)) {
					return reject("RV64 exact roots reject aggregate-reference table initializer")
				}
				if !rv64RootedAggregateNullElement(*table.Init, table.Type.Ref) {
					retainedElementSegments++
					if table.Type.Limits.Min == 0 || retainedElementSegments > 256 || table.Type.Limits.Min > uint64(remainingElements) {
						return reject("RV64 exact roots reject aggregate-reference table initializer retained bound")
					}
					remainingElements -= int(table.Type.Limits.Min)
				}
				continue
			}
			expr := *table.Init
			if len(expr.Instrs) != 0 || len(expr.BodyBytes) < 3 || len(expr.BodyBytes) > 7 || expr.BodyBytes[0] != 0xd0 && expr.BodyBytes[0] != 0xd2 {
				return reject("RV64 exact roots reject table initializer expression")
			}
			if expr.BodyBytes[0] == 0xd0 {
				r := wasm.ReaderFrom(expr.BodyBytes[1:])
				heap, err := r.S33()
				if err != nil || heap != -16 {
					return reject("RV64 exact roots require a funcref table initializer")
				}
			}
			value, err := wasm.ParseFuncrefElementExpr(expr)
			public := publicTables[importedTables+i]
			if err != nil || !value.Null && ((!public && uint64(value.FuncIndex) < uint64(importedFunctions)) || uint64(value.FuncIndex) >= uint64(importedFunctions+len(m.FuncTypes))) {
				return reject("RV64 exact roots reject table initializer target")
			}
		}
	}
	for i := range m.Elements {
		elem := &m.Elements[i]
		if tableCount == 0 {
			return reject("RV64 exact roots require bounded local function elements")
		}
		values := 0
		allowImported := !hasPrivateTable
		if elem.Mode.Kind == wasm.ElemActive {
			if uint64(elem.Mode.Table) >= uint64(tableCount) {
				return reject("RV64 exact roots reject active element table index %d", elem.Mode.Table)
			}
			table, ok := m.TableType(uint32(elem.Mode.Table))
			if !ok || !wasm.EqualValType(wasm.RefVal(table.Ref), wasm.FuncRef) && !rv64RootedAggregateTableReference(m, wasm.RefVal(table.Ref)) {
				return reject("RV64 exact roots reject aggregate-reference table elements")
			}
			typed := elem.Kind.Kind == wasm.ElemTypedExprs
			if (typed && !wasm.EqualValType(wasm.RefVal(elem.Kind.Ref), wasm.RefVal(table.Ref))) ||
				(!typed && !wasm.EqualValType(wasm.RefVal(table.Ref), wasm.FuncRef)) {
				return reject("RV64 exact roots reject active element/table type mismatch")
			}
			allowImported = publicTables[elem.Mode.Table]
		}
		switch elem.Kind.Kind {
		case wasm.ElemFuncs:
			if len(elem.Kind.Exprs) != 0 || elem.Kind.Ref != (wasm.RefType{}) {
				return reject("RV64 exact roots require canonical legacy function elements")
			}
			values = len(elem.Kind.Funcs)
			for _, function := range elem.Kind.Funcs {
				if (!allowImported && uint64(function) < uint64(importedFunctions)) || uint64(function) >= uint64(importedFunctions+len(m.FuncTypes)) {
					return reject("RV64 exact roots reject element function %d", function)
				}
			}
		case wasm.ElemFuncExprs, wasm.ElemTypedExprs:
			aggregate := elem.Kind.Kind == wasm.ElemTypedExprs && rv64RootedAggregateTableReference(m, wasm.RefVal(elem.Kind.Ref))
			if len(elem.Kind.Funcs) != 0 || elem.Kind.Kind == wasm.ElemFuncExprs && elem.Kind.Ref != (wasm.RefType{}) ||
				elem.Kind.Kind == wasm.ElemTypedExprs && !aggregate && !wasm.EqualValType(wasm.RefVal(elem.Kind.Ref), wasm.FuncRef) {
				return reject("RV64 exact roots require exact nullable funcref expression elements")
			}
			values = len(elem.Kind.Exprs)
			for j, expr := range elem.Kind.Exprs {
				if aggregate {
					if !rv64RootedAggregateElement(expr, elem.Kind.Ref, aggregateParser, importedGlobals+len(m.Globals)) {
						return reject("RV64 exact roots reject aggregate element %d:%d", i, j)
					}
					continue
				}
				if len(expr.Instrs) != 0 || len(expr.BodyBytes) < 3 || len(expr.BodyBytes) > 7 || expr.BodyBytes[0] != 0xd0 && expr.BodyBytes[0] != 0xd2 {
					return reject("RV64 exact roots reject element expression %d:%d", i, j)
				}
				if expr.BodyBytes[0] == 0xd0 {
					r := wasm.ReaderFrom(expr.BodyBytes[1:])
					heap, err := r.S33()
					if err != nil || heap != -16 {
						return reject("RV64 exact roots reject non-funcref null element %d:%d", i, j)
					}
				}
				value, err := wasm.ParseFuncrefElementExpr(expr)
				if err != nil || !value.Null && ((!allowImported && uint64(value.FuncIndex) < uint64(importedFunctions)) || uint64(value.FuncIndex) >= uint64(importedFunctions+len(m.FuncTypes))) {
					return reject("RV64 exact roots reject element expression %d:%d", i, j)
				}
			}
		default:
			return reject("RV64 exact roots reject unknown function element kind")
		}
		if values > remainingElements {
			return reject("RV64 exact roots require bounded function element payloads")
		}
		switch elem.Mode.Kind {
		case wasm.ElemActive:
			if uint64(elem.Mode.Table) >= uint64(tableCount) || len(elem.Mode.Offset.Instrs) != 0 || !rv64RootedI32Const(elem.Mode.Offset.BodyBytes) {
				return reject("RV64 exact roots require bounded literal active function elements")
			}
		case wasm.ElemPassive, wasm.ElemDeclarative:
			if elem.Mode.Table != 0 || len(elem.Mode.Offset.Instrs) != 0 || len(elem.Mode.Offset.BodyBytes) != 0 {
				return reject("RV64 exact roots require canonical inactive function elements")
			}
		default:
			return reject("RV64 exact roots reject unknown function element modes")
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
					return reject("RV64 exact roots reject malformed function %d", i)
				}
				var imm wasm.InstructionImmediate
				if err = classifier.ClassifyInto(r, op, &imm); err != nil {
					return reject("RV64 exact roots reject malformed function %d", i)
				}
				if imm.Kind == wasm.InstrRefFunc && uint64(imm.Index) < uint64(importedFunctions) {
					return reject("RV64 exact roots reject imported ref.func in private table module")
				}
			}
		}
	}
	remainingData := railshot.MaxModuleBytes
	for i := range m.Data {
		data := &m.Data[i]
		if len(data.Init) > remainingData {
			return reject("RV64 exact roots require bounded data payloads")
		}
		switch data.Mode.Kind {
		case wasm.DataActive:
			if uint64(data.Mode.Mem) >= uint64(memoryCount) || len(data.Mode.Offset.Instrs) != 0 || !rv64RootedI32Const(data.Mode.Offset.BodyBytes) {
				return reject("RV64 exact roots require literal active memory32 data")
			}
		case wasm.DataPassive:
			if data.Mode.Mem != 0 || len(data.Mode.Offset.Instrs) != 0 || len(data.Mode.Offset.BodyBytes) != 0 {
				return reject("RV64 exact roots require canonical passive data")
			}
		default:
			return reject("RV64 exact roots reject unknown data modes")
		}
		remainingData -= len(data.Init)
	}
	for i := range m.Exports {
		export := &m.Exports[i]
		if export.Index.Kind == wasm.ExternMem && uint64(export.Index.Index) < uint64(memoryCount) {
			continue
		}
		if export.Index.Kind == wasm.ExternGlobal && uint64(export.Index.Index) < uint64(importedGlobals+len(m.Globals)) {
			continue
		}
		if export.Index.Kind == wasm.ExternTable && uint64(export.Index.Index) < uint64(tableCount) {
			continue
		}
		if export.Index.Kind != wasm.ExternFunc {
			return reject("RV64 exact roots reject non-function export %d", i)
		}
		if uint64(export.Index.Index) < uint64(importedFunctions) || uint64(export.Index.Index) >= uint64(importedFunctions+len(m.Code)) {
			return reject("RV64 exact roots reject imported or unavailable function export %d", i)
		}
	}
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	modulePlan, err := gcFramePrepareModuleRootPlan(m, &classifier, analysis)
	if err != nil {
		return reject("%v", err)
	}
	var safepointBase uint32
	for function := range m.Code {
		if !modulePlan.FunctionPending(function) {
			continue
		}
		ft, ok := m.LocalFuncType(function)
		if !ok {
			return reject("function %d has no validated signature", function)
		}
		plan, ok := modulePlan.BeginFunction(function)
		if !ok {
			return reject("function %d root plan ownership is invalid", function)
		}
		*plan = shared.GCFrameRootPlan{Candidate: true, Exact: true, SafepointBase: safepointBase}
		slot, local := 0, uint32(0)
		add := func(t wasm.ValType) bool {
			if collectorFrameRefType(m, t) {
				if len(plan.Locals) == shared.GCFrameTrackedLocalLimit {
					return false
				}
				plan.Locals = append(plan.Locals, shared.GCFrameLocal{Index: local, Offset: uint32(shared.RV64CallFrameHeaderBytes + slot*8)})
			}
			if wasm.EqualValType(t, wasm.V128) {
				slot += 2
			} else {
				slot++
			}
			local++
			return true
		}
		for _, t := range ft.Params {
			if !add(t) {
				return reject("function %d exceeds %d tracked collector locals", function, shared.GCFrameTrackedLocalLimit)
			}
		}
		for _, run := range m.Code[function].Locals.Runs {
			for i := uint32(0); i < run.Count; i++ {
				if !add(run.Type) {
					return reject("function %d exceeds %d tracked collector locals", function, shared.GCFrameTrackedLocalLimit)
				}
			}
		}
		plan.Conservative = true
		liveMasks, err := gcFrameAllLiveMasksArenaWithClassifier(m.Code[function].BodyBytes, len(plan.Locals), &classifier)
		if err != nil {
			return reject("function %d exact local liveness: %v", function, err)
		}
		plan.Locals, liveMasks, _, err = gcFrameCompactLiveLocalsArena(plan.Locals, liveMasks)
		if err != nil {
			return reject("function %d exact local liveness: %v", function, err)
		}
		if liveMasks.allocationN == 0 && liveMasks.callN == 0 {
			return reject("function %d is collecting without an RV64 root boundary", function)
		}
		if uint64(safepointBase)+uint64(liveMasks.allocationN) > uint64(shared.GCSafepointIDMax) {
			return reject("function %d exceeds the dense safepoint ID bound", function)
		}
		if !plan.SetLiveMasks(liveMasks.words, liveMasks.allocationN, liveMasks.callN) {
			return reject("function %d has malformed exact local liveness masks", function)
		}
		safepointBase += uint32(liveMasks.allocationN)
	}
	return modulePlan
}

func rv64RootedI32Const(body []byte) bool {
	r := wasm.ReaderFrom(body)
	op, err := r.Byte()
	if err != nil || op != 0x41 {
		return false
	}
	if _, err = r.I32(); err != nil {
		return false
	}
	end, err := r.Byte()
	return err == nil && end == 0x0b && !r.HasNext()
}

func rv64RootedAggregateTableReference(m *wasm.Module, typ wasm.ValType) bool {
	if m == nil || typ.Kind() != wasm.ValRef {
		return false
	}
	ref := typ.Ref()
	heap := ref.Heap()
	if !ref.Nullable() || ref.Exact() {
		return false
	}
	switch heap.Kind() {
	case wasm.HeapAbs:
		return heap.Abs() == wasm.HeapI31
	case wasm.HeapTypeIndex:
		if heap.Type().Rec {
			return false
		}
		comp := rv64RootedAggregateType(m, heap.Type().Index)
		return comp != nil && (comp.Kind == wasm.CompStruct || comp.Kind == wasm.CompArray)
	default:
		return false
	}
}

func rv64RootedAggregateNullElement(expr wasm.Expr, ref wasm.RefType) bool {
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

func rv64RootedAggregateType(m *wasm.Module, index uint32) *wasm.CompType {
	if m == nil {
		return nil
	}
	sub, ok := m.TypeSubType(index)
	if !ok {
		return nil
	}
	return &sub.Comp
}

func rv64RootedFlatTypeCount(m *wasm.Module) int {
	if m == nil {
		return 0
	}
	total := 0
	for i := range m.Types {
		total += len(m.Types[i].SubTypes)
	}
	return total
}

type rv64RootedAggregateParser struct {
	m           *wasm.Module
	valid       bool
	entry, exit []uint32
}

func newRV64RootedAggregateParser(m *wasm.Module) *rv64RootedAggregateParser {
	p := &rv64RootedAggregateParser{m: m}
	flatCount := rv64RootedFlatTypeCount(m)
	if m == nil || flatCount == 0 || flatCount > railshot.MaxFunctions {
		return p
	}
	children := make([][]uint32, flatCount)
	roots := make([]uint32, 0, flatCount)
	flat := uint32(0)
	for i := range m.Types {
		for member := range m.Types[i].SubTypes {
			sub := &m.Types[i].SubTypes[member]
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

func (p *rv64RootedAggregateParser) constructedSubtype(typeIndex uint32, required wasm.RefType) bool {
	if p == nil || !p.valid || required.Heap().Kind() != wasm.HeapTypeIndex || required.Heap().Type().Rec {
		return false
	}
	target := required.Heap().Type().Index
	if uint64(typeIndex) >= uint64(len(p.entry)) || uint64(target) >= uint64(len(p.entry)) {
		return false
	}
	actualComp, targetComp := rv64RootedAggregateType(p.m, typeIndex), rv64RootedAggregateType(p.m, target)
	if actualComp == nil || targetComp == nil || actualComp.Kind != targetComp.Kind || actualComp.Kind != wasm.CompStruct && actualComp.Kind != wasm.CompArray {
		return false
	}
	if required.Exact() {
		return typeIndex == target
	}
	return p.entry[target] <= p.entry[typeIndex] && p.entry[typeIndex] < p.exit[target]
}

type rv64RootedAggregateStackValue struct {
	typ         wasm.ValType
	constructed bool
	typeIndex   uint32
	depth       int
	knownI32    bool
	i32         uint32
}

func (p *rv64RootedAggregateParser) valueMatches(value rv64RootedAggregateStackValue, storage wasm.StorageType) bool {
	want := storage.Val()
	if storage.Packed() {
		want = wasm.I32
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

func (p *rv64RootedAggregateParser) validateExpression(expr wasm.Expr, required wasm.RefType, globalLimit int) bool {
	if p == nil || !p.valid || globalLimit < 0 || globalLimit > p.m.ImportedGlobalCount()+len(p.m.Globals) ||
		len(expr.Instrs) != 0 || len(expr.BodyBytes) < 3 || len(expr.BodyBytes) > 4096 {
		return false
	}
	var stack [512]rv64RootedAggregateStackValue
	stackLen, allocations, work := 0, 0, 0
	push := func(value rv64RootedAggregateStackValue) bool {
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
		if work > rv64RootedAggregateParseWork {
			return false
		}
		op, err := r.Byte()
		if err != nil {
			return false
		}
		value := rv64RootedAggregateStackValue{}
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
			comp := rv64RootedAggregateType(p.m, typeIndex)
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
					if !p.valueMatches(stack[base+i], comp.Fields[i].Storage()) {
						return false
					}
					constructorDepth = max(constructorDepth, stack[base+i].depth+1)
				}
			case 1:
				if comp.Kind != wasm.CompStruct {
					return false
				}
				for i := range comp.Fields {
					if !rv64RootedAggregateDefaultableStorage(comp.Fields[i].Storage()) {
						return false
					}
				}
			case 6:
				if comp.Kind != wasm.CompArray || stackLen < 2 || !p.valueMatches(stack[stackLen-2], comp.Array.Storage()) ||
					!wasm.EqualValType(stack[stackLen-1].typ, wasm.I32) || !stack[stackLen-1].knownI32 || stack[stackLen-1].i32 > 256 {
					return false
				}
				base = stackLen - 2
				constructorDepth = max(constructorDepth, stack[base].depth+1)
			case 7:
				if comp.Kind != wasm.CompArray || stackLen < 1 || !rv64RootedAggregateDefaultableStorage(comp.Array.Storage()) ||
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
					if !p.valueMatches(stack[i], comp.Array.Storage()) {
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
			value = rv64RootedAggregateStackValue{
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

func rv64RootedAggregateSupportedStorage(storage wasm.StorageType) bool {
	if storage.Packed() {
		return true
	}
	want := storage.Val()
	return wasm.EqualValType(want, wasm.I32) || wasm.EqualValType(want, wasm.I64) || wasm.EqualValType(want, wasm.F32) || wasm.EqualValType(want, wasm.F64) || wasm.EqualValType(want, wasm.V128) || want.Kind() == wasm.ValRef
}

func rv64RootedAggregateDefaultableStorage(storage wasm.StorageType) bool {
	return rv64RootedAggregateSupportedStorage(storage) && (storage.Packed() || storage.Val().Kind() != wasm.ValRef || storage.Val().Ref().IsDefaultable())
}

func rv64RootedAggregateElement(expr wasm.Expr, ref wasm.RefType, p *rv64RootedAggregateParser, globalLimit int) bool {
	return p.validateExpression(expr, ref, globalLimit)
}

const rv64RootedAggregateParseWork = 8192

func rv64RootedScalarGlobal(m *wasm.Module, global *wasm.Global, limit int, p *rv64RootedAggregateParser) bool {
	if global == nil || len(global.Init.Instrs) != 0 {
		return false
	}
	if global.Type.Type.Kind() == wasm.ValRef {
		ref := global.Type.Type.Ref()
		heap := ref.Heap()
		if ref.Exact() || !p.validateExpression(global.Init, ref, limit) {
			return false
		}
		switch heap.Kind() {
		case wasm.HeapAbs:
			return heap.Abs() == wasm.HeapI31
		case wasm.HeapTypeIndex:
			if heap.Type().Rec {
				return false
			}
			comp := rv64RootedAggregateType(m, heap.Type().Index)
			return comp != nil && (comp.Kind == wasm.CompStruct || comp.Kind == wasm.CompArray)
		default:
			return false
		}
	}
	if wasm.EqualValType(global.Type.Type, wasm.I32) || wasm.EqualValType(global.Type.Type, wasm.I64) {
		return railshot.ValidateIntegerInitializer(global.Init.BodyBytes, global.Type.Type, func(index uint32) (wasm.ValType, bool) {
			if uint64(index) >= uint64(limit) {
				return wasm.ValType{}, false
			}
			typ, ok := m.GlobalTypeByIndex(index)
			return typ.Type, ok && !typ.Mutable
		}) == nil
	}
	r := wasm.ReaderFrom(global.Init.BodyBytes)
	op, err := r.Byte()
	if err != nil {
		return false
	}
	if op == 0x23 {
		index, err := r.U32()
		if err != nil || uint64(index) >= uint64(limit) {
			return false
		}
		end, err := r.Byte()
		if err != nil || end != 0x0b || r.HasNext() {
			return false
		}
		typ, ok := m.GlobalTypeByIndex(index)
		return ok && !typ.Mutable && wasm.EqualValType(typ.Type, global.Type.Type)
	}
	switch {
	case wasm.EqualValType(global.Type.Type, wasm.I32) && op == 0x41:
		_, err = r.I32()
	case wasm.EqualValType(global.Type.Type, wasm.I64) && op == 0x42:
		_, err = r.I64()
	case wasm.EqualValType(global.Type.Type, wasm.F32) && op == 0x43:
		_, err = r.LEU32()
	case wasm.EqualValType(global.Type.Type, wasm.F64) && op == 0x44:
		_, err = r.LEU64()
	default:
		return false
	}
	if err != nil {
		return false
	}
	end, err := r.Byte()
	return err == nil && end == 0x0b && !r.HasNext()
}
