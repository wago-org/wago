//go:build linux && riscv64 && !tinygo && !wago_precompiled

package wago

import (
	"fmt"
	railcore "github.com/wago-org/wago/src/core/compiler/backend/railshot"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

// newGCFrameRootPlan is the bounded RV64 exact-root producer for rooted scalar
// modules. Allocation helpers publish safepoint roots and Wasm/host calls
// publish return-PC caller-frame maps.
func newGCFrameRootPlan(m *wasm.Module, exactRoots, _ bool, diagnostic *string, analysis *wasm.ValidatedModuleAnalysis) *shared.GCModuleFrameRootPlan {
	return newGCFrameRootPlanOwned(m, exactRoots, diagnostic, analysis, railshotManagedExceptionReferences(m))
}

func newGCFrameRootPlanOwned(m *wasm.Module, exactRoots bool, diagnostic *string, analysis *wasm.ValidatedModuleAnalysis, ownedExceptions bool, custom ...map[uint32]railcore.CustomInstruction) *shared.GCModuleFrameRootPlan {
	var instructions map[uint32]railcore.CustomInstruction
	if len(custom) != 0 {
		instructions = custom[0]
	}
	customValues := false
	for _, ins := range instructions {
		customValues = customValues || ins.CustomOutput != nil
		for _, typ := range ins.CustomInputs {
			customValues = customValues || !typ.IsZero()
		}
	}
	if diagnostic != nil {
		*diagnostic = ""
	}
	if !exactRoots {
		return nil
	}
	reject := func(format string, args ...any) *shared.GCModuleFrameRootPlan {
		if diagnostic != nil {
			*diagnostic = fmt.Sprintf(format, args...)
		}
		return nil
	}
	if m == nil {
		return reject("generic GC module has no local function bodies")
	}
	functionEnvironments := ownedExceptions || wasm.ModuleFunctionReferenceCarriers(m)
	importedFunctions := m.ImportedFuncCount()
	importedGlobals := m.ImportedGlobalCount()
	importedMemories := m.ImportedMemCount()
	importedTables := m.ImportedTableCount()
	memoryCount := importedMemories + len(m.Memories)
	tableCount := importedTables + len(m.Tables)
	if importedFunctions+importedGlobals+importedMemories+importedTables+m.ImportedTagCount() != len(m.Imports) || importedFunctions > railshot.MaxHostFunctionImports || importedFunctions+len(m.Code) > railshot.MaxFunctions || len(m.Tables) > maxRV64ExecutionTables || tableCount > maxRV64ExecutionTables || memoryCount > rv64RootedMaxMemories || m.TagCount() > railshot.MaxFunctions || importedGlobals+len(m.Globals) > 256 || len(m.Elements) > 256 || len(m.Data) > 256 || len(m.StringRefs) != 0 {
		return reject("RV64 exact roots require a bounded scalar allocation module")
	}
	rootedImportValue := func(typ wasm.ValType) bool {
		if wasm.EqualValType(typ, wasm.I32) || wasm.EqualValType(typ, wasm.I64) || wasm.EqualValType(typ, wasm.F32) || wasm.EqualValType(typ, wasm.F64) || wasm.EqualValType(typ, wasm.V128) {
			return true
		}
		if typ.Kind() != wasm.ValRef {
			return false
		}
		heap := typ.Ref().Heap()
		if heap.Kind() == wasm.HeapAbs {
			if typ.Ref().Exact() {
				return false
			}
			switch heap.Abs() {
			case wasm.HeapFunc, wasm.HeapNoFunc:
				return functionEnvironments
			case wasm.HeapExn, wasm.HeapNoExn:
				return ownedExceptions
			case wasm.HeapNone, wasm.HeapAny, wasm.HeapEq, wasm.HeapI31, wasm.HeapStruct, wasm.HeapArray,
				wasm.HeapExtern, wasm.HeapNoExtern:
				return true
			default:
				return false
			}
		}
		if heap.Kind() != wasm.HeapTypeIndex || heap.Type().Rec {
			return false
		}
		target, ok := m.TypeSubType(heap.Type().Index)
		return ok && (target.Comp.Kind == wasm.CompStruct || target.Comp.Kind == wasm.CompArray || functionEnvironments && target.Comp.Kind == wasm.CompFunc)
	}
	if len(m.Imports) != 0 {
		functionIndex := uint32(0)
		for i := range m.Imports {
			im := &m.Imports[i]
			switch im.Type.Kind {
			case wasm.ExternFunc:
				_, inline := instructions[functionIndex]
				functionIndex++
				ft, ok := m.ImportFuncType(i)
				if !ok || len(ft.Params) > coreruntime.MaxHostArity || len(ft.Results) > coreruntime.MaxHostArity {
					return reject("RV64 exact roots reject function import %d", i)
				}
				if inline {
					continue
				} // The validated directory owns the full physical signature.
				for _, values := range [][]wasm.ValType{ft.Params, ft.Results} {
					for _, typ := range values {
						if !rootedImportValue(typ) {
							return reject("RV64 exact roots reject unsupported function import %d", i)
						}
					}
				}
			case wasm.ExternTag:
				// Immutable tag identities; payload roots are fixed below.
			case wasm.ExternGlobal:
				typ := im.Type.GlobalType().Type
				if !wasm.EqualValType(typ, wasm.I32) && !wasm.EqualValType(typ, wasm.I64) && !wasm.EqualValType(typ, wasm.F32) && !wasm.EqualValType(typ, wasm.F64) && !rv64RootedGlobalReference(m, typ) {
					return reject("RV64 exact roots reject non-scalar global import %d", i)
				}
			case wasm.ExternMem:
				memory := im.Type.MemType()
				if memory.Limits.HasMax && memory.Limits.Min > memory.Limits.Max {
					return reject("RV64 exact roots reject memory import %d", i)
				}
			case wasm.ExternTable:
				table := im.Type.TableType()
				elem := wasm.RefVal(table.Ref)
				if !wasm.FunctionReferenceType(m, elem) && !rv64RootedAggregateTableReference(m, elem) || table.Limits.Addr64 || !table.Limits.HasMax || table.Limits.Min > table.Limits.Max || table.Limits.Max > 256 {
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
		if memory.Shared || memory.Limits.HasMax && memory.Limits.Min > memory.Limits.Max {
			return reject("RV64 exact roots require bounded local memory32 %d", i)
		}
	}
	remainingElements := 256
	retainedElementSegments := len(m.Elements)
	for i := range m.Tables {
		table := &m.Tables[i]
		elem := wasm.RefVal(table.Type.Ref)
		rootedTable := rv64RootedAggregateTableReference(m, elem)
		if (!wasm.FunctionReferenceType(m, elem) && !rootedTable) || table.Type.Limits.Addr64 || table.Type.Limits.Min > 256 || rootedTable && !table.Type.Ref.Nullable() ||
			table.Type.Limits.HasMax && (table.Type.Limits.Min > table.Type.Limits.Max || table.Type.Limits.Max > 256) {
			return reject("RV64 exact roots require a bounded local function or aggregate-reference table32")
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
			if err != nil || !value.Null && (uint64(value.FuncIndex) >= uint64(importedFunctions+len(m.FuncTypes))) {
				return reject("RV64 exact roots reject table initializer target")
			}
		}
	}
	for i := range m.Elements {
		elem := &m.Elements[i]
		values := 0
		if elem.Mode.Kind == wasm.ElemActive {
			if uint64(elem.Mode.Table) >= uint64(tableCount) {
				return reject("RV64 exact roots reject active element table index %d", elem.Mode.Table)
			}
			table, ok := m.TableType(uint32(elem.Mode.Table))
			if !ok || !wasm.FunctionReferenceType(m, wasm.RefVal(table.Ref)) && !rv64RootedAggregateTableReference(m, wasm.RefVal(table.Ref)) {
				return reject("RV64 exact roots reject aggregate-reference table elements")
			}
			typed := elem.Kind.Kind == wasm.ElemTypedExprs
			if (typed && !m.ReferenceTypeSubtype(elem.Kind.Ref, table.Ref)) ||
				(!typed && !wasm.EqualValType(wasm.RefVal(table.Ref), wasm.FuncRef)) {
				return reject("RV64 exact roots reject active element/table type mismatch")
			}
		}
		switch elem.Kind.Kind {
		case wasm.ElemFuncs:
			if len(elem.Kind.Exprs) != 0 || elem.Kind.Ref != (wasm.RefType{}) {
				return reject("RV64 exact roots require canonical legacy function elements")
			}
			values = len(elem.Kind.Funcs)
			for _, function := range elem.Kind.Funcs {
				if uint64(function) >= uint64(importedFunctions+len(m.FuncTypes)) {
					return reject("RV64 exact roots reject element function %d", function)
				}
			}
		case wasm.ElemFuncExprs, wasm.ElemTypedExprs:
			aggregate := elem.Kind.Kind == wasm.ElemTypedExprs && rv64RootedAggregateTableReference(m, wasm.RefVal(elem.Kind.Ref))
			typedFunction := elem.Kind.Kind == wasm.ElemTypedExprs && wasm.FunctionReferenceType(m, wasm.RefVal(elem.Kind.Ref))
			if len(elem.Kind.Funcs) != 0 || elem.Kind.Kind == wasm.ElemFuncExprs && elem.Kind.Ref != (wasm.RefType{}) ||
				elem.Kind.Kind == wasm.ElemTypedExprs && !aggregate && !typedFunction && !wasm.EqualValType(wasm.RefVal(elem.Kind.Ref), wasm.FuncRef) {
				return reject("RV64 exact roots require exact nullable funcref expression elements")
			}
			values = len(elem.Kind.Exprs)
			for j, expr := range elem.Kind.Exprs {
				if typedFunction {
					_, ok := wasm.FunctionElementSubtype(m, expr, elem.Kind.Ref)
					if !ok {
						return reject("RV64 exact roots reject typed function element %d:%d", i, j)
					}
					continue
				}
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
				if err != nil || !value.Null && (uint64(value.FuncIndex) >= uint64(importedFunctions+len(m.FuncTypes))) {
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
		if export.Index.Kind == wasm.ExternTag && uint64(export.Index.Index) < uint64(m.TagCount()) {
			continue
		}
		if export.Index.Kind != wasm.ExternFunc {
			return reject("RV64 exact roots reject non-function export %d", i)
		}
		if uint64(export.Index.Index) >= uint64(importedFunctions+len(m.Code)) {
			return reject("RV64 exact roots reject unavailable function export %d", i)
		}
	}
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	ehMaps, err := railshot.BuildExceptionRootMapsOwned(m, ownedExceptions)
	if err != nil {
		return reject("exception root maps: %v", err)
	}
	if len(m.Code) == 0 {
		return shared.NewGCModuleFrameRootPlan(0)
	}
	modulePlan, err := gcFramePrepareRV64RootPlan(m, &classifier, analysis, ownedExceptions, instructions)
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
		for i := range ehMaps {
			if ehMaps[i].LocalFunction == uint32(function) && !plan.SetFixedOffsets(gcFrameFixedOffsets(&ehMaps[i])) {
				return reject("function %d exception root layout", function)
			}
		}
		usesSWAR, err := railshot.FunctionUsesSWAR(m, function)
		usesSWAR = usesSWAR || customValues
		if err != nil {
			return reject("function %d SWAR root layout: %v", function, err)
		}
		slot, local := 0, uint32(0)
		add := func(t wasm.ValType) bool {
			functionRef := functionEnvironments && wasm.FunctionReferenceType(m, t)
			externRef := wasm.ExternReferenceType(t)
			if collectorFrameRefType(m, t) || ownedExceptions && t.Kind() == wasm.ValRef && (t.Ref().Heap() == wasm.AbsHeap(wasm.HeapExn) || t.Ref().Heap() == wasm.AbsHeap(wasm.HeapNoExn)) || functionRef || externRef {
				if len(plan.Locals) == shared.GCFrameTrackedLocalLimit {
					return false
				}
				offset := uint32(shared.RV64CallFrameHeaderBytes + slot*8)
				if functionRef {
					offset |= shared.GCFrameFuncRefRootFlag
				} else if externRef {
					offset |= shared.GCFrameExternRefRootFlag
				}
				plan.Locals = append(plan.Locals, shared.GCFrameLocal{Index: local, Offset: offset})
			}
			// SWAR keeps every logical local in one padded 16-byte home.
			if usesSWAR || wasm.EqualValType(t, wasm.V128) {
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
		liveMasks, err := gcFrameAllLiveMasksArenaWithClassifier(m.Code[function].BodyBytes, len(plan.Locals), &classifier, m)
		if err != nil {
			return reject("function %d exact local liveness: %v", function, err)
		}
		if len(instructions) != 0 {
			inline, _, err := rv64CustomInlineRootBoundaries(m, m.Code[function].BodyBytes, &classifier, instructions)
			if err != nil || inline > liveMasks.callN {
				return reject("function %d inline native call mask count", function)
			}
			// Every site has the same conservative all-local mask here. Removing
			// inline sites cannot change any surviving site's local liveness.
			liveMasks.callN -= inline
			liveMasks.words = liveMasks.words[:(liveMasks.allocationN+liveMasks.callN)*liveMasks.wordsPerSite]
		}
		if ownedExceptions {
			count, scanErr := railshot.ExceptionOwnedCopyCount(m, function)
			if scanErr != nil {
				return reject("function %d exception copies: %v", function, scanErr)
			}
			if count != 0 {
				next := newGCFrameLiveMasks(liveMasks.allocationN+count, liveMasks.callN, liveMasks.wordsPerSite)
				mask := make([]uint64, liveMasks.wordsPerSite)
				for i := range mask {
					mask[i] = ^uint64(0)
				}
				if n := len(plan.Locals) % 64; n != 0 {
					mask[len(mask)-1] = (uint64(1) << n) - 1
				} else if len(plan.Locals) == 0 {
					mask[0] = 0
				}
				for site := 0; site < next.allocationN+next.callN; site++ {
					copy(next.site(site), mask)
				}
				liveMasks = next
			}
		}
		plan.Locals, liveMasks, _, err = gcFrameCompactLiveLocalsArena(plan.Locals, liveMasks)
		if err != nil {
			return reject("function %d exact local liveness: %v", function, err)
		}
		if liveMasks.allocationN == 0 && liveMasks.callN == 0 && !rv64RootedBodyHasTailBridge(m, m.Code[function].BodyBytes, &classifier) {
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

func rv64RootedBodyHasTailBridge(m *wasm.Module, body []byte, classifier *wasm.ModuleInstructionClassifier) bool {
	if m == nil || classifier == nil {
		return false
	}
	imports := uint64(m.ImportedFuncCount())
	r := wasm.NewReader(body)
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return false
		}
		var imm wasm.InstructionImmediate
		if err = classifier.ClassifyInto(r, op, &imm); err != nil {
			return false
		}
		switch imm.Kind {
		case wasm.InstrReturnCall:
			if uint64(imm.Index) < imports {
				sig, ok := m.FuncSignature(imm.Index)
				if ok && rv64RootedTailSignature(m, sig) {
					return true
				}
			}
		case wasm.InstrReturnCallIndirect:
			table, ok := m.TableType(imm.Index2)
			if !ok || !wasm.FunctionReferenceType(m, wasm.RefVal(table.Ref)) {
				continue
			}
			sig, ok := m.ResolvedTypeFunc(imm.Index)
			if ok && rv64RootedTailSignature(m, sig) {
				return true
			}
		case wasm.InstrReturnCallRef:
			sig, ok := m.ResolvedTypeFunc(imm.Index)
			if ok && rv64RootedTailSignature(m, sig) {
				return true
			}
		}
	}
	return false
}

func rv64RootedTailSignature(m *wasm.Module, sig *wasm.CompType) bool {
	if sig == nil || sig.Kind != wasm.CompFunc || len(sig.Params) > 256 || len(sig.Results) > 256 {
		return false
	}
	for _, values := range [][]wasm.ValType{sig.Params, sig.Results} {
		for _, typ := range values {
			if !wasm.EqualValType(typ, wasm.I32) && !wasm.EqualValType(typ, wasm.I64) && !wasm.EqualValType(typ, wasm.F32) && !wasm.EqualValType(typ, wasm.F64) && !wasm.EqualValType(typ, wasm.V128) && typ.Kind() != wasm.ValRef {
				return false
			}
		}
	}
	return true
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
	if ref.Exact() {
		return false
	}
	switch heap.Kind() {
	case wasm.HeapAbs:
		switch heap.Abs() {
		case wasm.HeapExn, wasm.HeapNoExn:
			return railshot.ManagedExceptionReferences(m)
		case wasm.HeapAny, wasm.HeapEq, wasm.HeapI31, wasm.HeapStruct, wasm.HeapArray, wasm.HeapExtern, wasm.HeapNoExtern:
			return true
		default:
			return false
		}
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

func rv64RootedGlobalReference(m *wasm.Module, typ wasm.ValType) bool {
	if rv64RootedAggregateTableReference(m, typ) {
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

func (p *rv64RootedAggregateParser) valueMatches(value rv64RootedAggregateStackValue, storage wasm.StorageType, owner uint32) bool {
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
		case 0x6a, 0x6b, 0x6c, 0x7c, 0x7d, 0x7e:
			typ := wasm.I32
			if op >= 0x7c {
				typ = wasm.I64
			}
			if stackLen < 2 || !wasm.EqualValType(stack[stackLen-2].typ, typ) || !wasm.EqualValType(stack[stackLen-1].typ, typ) {
				return false
			}
			left, right := stack[stackLen-2], stack[stackLen-1]
			stackLen -= 2
			value.typ = typ
			if wasm.EqualValType(typ, wasm.I32) && left.knownI32 && right.knownI32 {
				value.knownI32 = true
				switch op {
				case 0x6a:
					value.i32 = left.i32 + right.i32
				case 0x6b:
					value.i32 = left.i32 - right.i32
				case 0x6c:
					value.i32 = left.i32 * right.i32
				}
			}
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
			if sub == 26 || sub == 27 {
				if stackLen < 1 || stack[stackLen-1].typ.Kind() != wasm.ValRef {
					return false
				}
				input := stack[stackLen-1]
				want, output := wasm.HeapExtern, wasm.HeapAny
				if sub == 27 {
					want, output = wasm.HeapAny, wasm.HeapExtern
				}
				if !p.valueMatches(input, wasm.StorageVal(wasm.RefVal(wasm.Ref(true, wasm.AbsHeap(want), false))), 0) {
					return false
				}
				stackLen--
				value.typ = wasm.RefVal(wasm.Ref(input.typ.Ref().Nullable(), wasm.AbsHeap(output), false))
				break
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
					if !rv64RootedAggregateDefaultableStorage(comp.Fields[i].Storage()) {
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

// Opaque nulls carry no collector object. Mixed exception declarations can keep
// them in state without treating function descriptors or extern tokens as roots.
func rv64OwnedOpaqueNullGlobal(m *wasm.Module, global *wasm.Global) bool {
	if m == nil || global == nil || !railshot.ManagedExceptionReferences(m) || global.Type.Type.Kind() != wasm.ValRef || len(global.Init.Instrs) != 0 {
		return false
	}
	ref := global.Type.Type.Ref()
	if !ref.Nullable() || ref.Exact() {
		return false
	}
	heap := ref.Heap()
	switch heap.Kind() {
	case wasm.HeapAbs:
		if heap.Abs() != wasm.HeapFunc && heap.Abs() != wasm.HeapNoFunc && heap.Abs() != wasm.HeapExtern && heap.Abs() != wasm.HeapNoExtern {
			return false
		}
	case wasm.HeapTypeIndex:
		if heap.Type().Rec {
			return false
		}
		comp, ok := m.TypeFunc(heap.Type().Index)
		if !ok || comp == nil {
			return false
		}
	default:
		return false
	}
	r := wasm.ReaderFrom(global.Init.BodyBytes)
	op, err := r.Byte()
	if err != nil || op != 0xd0 {
		return false
	}
	literal, err := r.S33()
	if err != nil {
		return false
	}
	var actual wasm.HeapType
	if literal >= 0 {
		actual = wasm.IndexedHeap(wasm.TypeIdx{Index: uint32(literal)})
	} else {
		actual = wasm.AbsHeap(wasm.AbsHeapType(byte(literal & 0x7f)))
	}
	end, err := r.Byte()
	return err == nil && end == 0x0b && !r.HasNext() && m.ReferenceTypeSubtype(wasm.Ref(true, actual, false), ref)
}

func rv64RootedScalarGlobal(m *wasm.Module, global *wasm.Global, limit int, p *rv64RootedAggregateParser) bool {
	if global == nil || len(global.Init.Instrs) != 0 {
		return false
	}
	if global.Type.Type.Kind() == wasm.ValRef {
		if railshot.AdmitPrimitiveReferenceGlobalInitializer(*global, m, limit) {
			return true
		}
		if rv64OwnedOpaqueNullGlobal(m, global) {
			return true
		}
		ref := global.Type.Type.Ref()
		heap := ref.Heap()
		if ref.Exact() || !p.validateExpression(global.Init, ref, limit) {
			return false
		}
		switch heap.Kind() {
		case wasm.HeapAbs:
			switch heap.Abs() {
			case wasm.HeapNone:
				return ref.Nullable() && railshot.ManagedExceptionReferences(m)
			case wasm.HeapExn, wasm.HeapNoExn:
				return ref.Nullable() && railshot.ManagedExceptionReferences(m)
			case wasm.HeapAny, wasm.HeapEq, wasm.HeapI31, wasm.HeapStruct, wasm.HeapArray, wasm.HeapExtern, wasm.HeapNoExtern:
				return ref.Nullable() || heap.Abs() == wasm.HeapI31
			default:
				return false
			}
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

func gcFramePrepareRV64RootPlan(m *wasm.Module, classifier *wasm.ModuleInstructionClassifier, analysis *wasm.ValidatedModuleAnalysis, ownedExceptions bool, custom ...map[uint32]railcore.CustomInstruction) (*shared.GCModuleFrameRootPlan, error) {
	var instructions map[uint32]railcore.CustomInstruction
	if len(custom) != 0 {
		instructions = custom[0]
	}
	if !ownedExceptions && len(instructions) == 0 {
		return gcFramePrepareModuleRootPlan(m, classifier, analysis)
	}
	plan := shared.NewGCModuleFrameRootPlan(len(m.Code))
	count := 0
	for function := range m.Code {
		copies := 0
		var err error
		if ownedExceptions {
			copies, err = railshot.ExceptionOwnedCopyCount(m, function)
		}
		if err != nil {
			return nil, err
		}
		needs := gcFrameBodyNeedsRootPlanWithClassifier(m, m.Code[function].BodyBytes, classifier)
		if len(instructions) != 0 {
			inline, boundary, err := rv64CustomInlineRootBoundaries(m, m.Code[function].BodyBytes, classifier, instructions)
			if err != nil {
				return nil, err
			}
			if inline != 0 && !boundary {
				needs = false
			}
		}
		if copies != 0 || needs {
			if !plan.MarkFunction(function) {
				return nil, fmt.Errorf("exception root function %d", function)
			}
			count++
		}
	}
	if !plan.ReserveFunctions(count) {
		return nil, fmt.Errorf("exception root capacity")
	}
	return plan, nil
}

func rv64CustomInlineRootBoundaries(m *wasm.Module, body []byte, classifier *wasm.ModuleInstructionClassifier, instructions map[uint32]railcore.CustomInstruction) (inline int, boundary bool, err error) {
	r := wasm.NewReader(body)
	for r.HasNext() {
		op, e := r.Byte()
		if e != nil {
			return 0, false, e
		}
		var imm wasm.InstructionImmediate
		if e = classifier.ClassifyInto(r, op, &imm); e != nil {
			return 0, false, e
		}
		if op == 0xfb {
			switch imm.Subopcode {
			case 0, 1, 6, 7, 8, 9, 10:
				boundary = true
			}
			if wasm.OpaqueStorageMutation(m, imm.Kind, imm.Index, imm.Index2) {
				boundary = true
			}
		}
		switch imm.Kind {
		case wasm.InstrCall:
			if _, ok := instructions[imm.Index]; ok {
				inline++
			} else {
				boundary = true
			}
		case wasm.InstrCallIndirect, wasm.InstrCallRef, wasm.InstrReturnCall, wasm.InstrReturnCallIndirect, wasm.InstrReturnCallRef:
			boundary = true
		}
	}
	return inline, boundary, nil
}
