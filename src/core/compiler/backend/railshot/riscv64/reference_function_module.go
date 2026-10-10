//go:build linux && !tinygo

package riscv64

import (
	"bytes"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// admitPublicReferenceCallBoundary is the first public owner-scoped call_ref
// surface. It is deliberately one table-free/reference-global-free recipe:
// a numeric target, a token-producing wrapper, and a wrapper that consumes one
// nullable exact target reference before an ordinary call_ref. The wrapper may
// return either the numeric target result or, after dropping it, the same
// authenticated target as a nullable funcref. The compiler witness, public
// token authentication, and invocation owner graph are all required; no other
// public dynamic-reference shape inherits this exception.
func admitPublicReferenceCallBoundary(m *wasm.Module) bool {
	if m == nil || len(m.Imports) != 0 || len(m.Types) != 3 || len(m.FuncTypes) != 3 || len(m.Code) != 3 || len(m.Tables) != 0 || len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || len(m.Data) != 0 || m.DataCount != nil || len(m.Elements) != 1 || m.Start != nil || len(m.Exports) != 3 || len(m.Customs) != 0 || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	for i := range m.Types {
		if len(m.Types[i].SubTypes) != 1 {
			return false
		}
		sub := m.Types[i].SubTypes[0]
		if !sub.Final || sub.HasPrefix || len(sub.Supers) != 0 || sub.Metadata.Describes.Present() || sub.Metadata.Descriptor.Present() || sub.Comp.Kind != wasm.CompFunc || len(sub.Comp.Fields) != 0 || sub.Comp.Array != (wasm.FieldType{}) {
			return false
		}
	}
	target := m.Types[0].SubTypes[0].Comp
	producer := m.Types[1].SubTypes[0].Comp
	caller := m.Types[2].SubTypes[0].Comp
	indexedTarget := wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{}), false))
	if len(target.Params) != 0 || len(target.Results) != 1 || !wasm.EqualValType(target.Results[0], wasm.I32) || len(producer.Params) != 0 || len(producer.Results) != 1 || !wasm.EqualValType(producer.Results[0], wasm.FuncRef) || len(caller.Params) != 1 || !wasm.EqualValType(caller.Params[0], indexedTarget) || len(caller.Results) != 1 {
		return false
	}
	numericResult := wasm.EqualValType(caller.Results[0], wasm.I32)
	referenceResult := wasm.EqualValType(caller.Results[0], wasm.FuncRef)
	if !numericResult && !referenceResult {
		return false
	}
	for i := range m.FuncTypes {
		if m.FuncTypes[i].Rec || m.FuncTypes[i].Index != uint32(i) || len(m.Code[i].Locals.Runs) != 0 || len(m.Code[i].Body.Instrs) != 0 {
			return false
		}
	}
	e := m.Elements[0]
	if e.Mode.Kind != wasm.ElemDeclarative || e.Mode.Table != 0 || len(e.Mode.Offset.BodyBytes) != 0 || len(e.Mode.Offset.Instrs) != 0 || e.Kind.Kind != wasm.ElemFuncs || e.Kind.Ref != (wasm.RefType{}) || len(e.Kind.Funcs) != 1 || e.Kind.Funcs[0] != 0 || len(e.Kind.Exprs) != 0 {
		return false
	}
	callerBody := []byte{0x20, 0, 0x14, 0, 0x0b}
	if referenceResult {
		callerBody = []byte{0x20, 0, 0x14, 0, 0x1a, 0x20, 0, 0x0b}
	}
	if len(m.Code[0].BodyBytes) != 3 || m.Code[0].BodyBytes[0] != 0x41 || m.Code[0].BodyBytes[1]&0x80 != 0 || m.Code[0].BodyBytes[2] != 0x0b || !bytes.Equal(m.Code[1].BodyBytes, []byte{0xd2, 0, 0x0b}) || !bytes.Equal(m.Code[2].BodyBytes, callerBody) {
		return false
	}
	for i, name := range []string{"target", "get", "call"} {
		export := m.Exports[i]
		if export.Name != name || export.Index.Kind != wasm.ExternFunc || export.Index.Index != uint32(i) {
			return false
		}
	}
	return true
}

// This private decoded-module prerequisite proves that reference-bearing
// functions cannot escape through a descriptor, import or resource. A direct
// function export is allowed only through the public wrapper boundary, which
// authenticates opaque tokens and never makes that function a ref.func target.
// It does not authorize production staging or general reference ownership.
// Every bound precedes frontend validation and derived carrier allocation.
func admitClosedReferenceFunctionModule(m *wasm.Module, vector bool) error {
	if m == nil || len(m.Imports) > MaxProductionImports || len(m.Code) == 0 || len(m.Code) > MaxFunctions || len(m.FuncTypes) != len(m.Code) || len(m.Types) > MaxFunctions || len(m.Exports) > MaxFunctions || len(m.Elements) > MaxOwnedElementSegments || len(m.Customs) > MaxFunctions {
		return unsupported("closed reference module directory bound")
	}
	publicReferenceCallBoundary := !vector && admitPublicReferenceCallBoundary(m)
	counts, err := countClosedReferenceImports(m)
	if err != nil {
		return err
	}
	imported := counts.functions
	if m.UsesCompactImports || len(m.Tables) > 1 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || m.BranchHints != nil {
		return unsupported("closed reference module external state")
	}
	remaining := MaxModuleBytes
	if err := admitClosedReferenceNumericStateWithImports(m, vector, &remaining, counts); err != nil {
		return err
	}
	if err := admitClosedReferenceOwnedTableState(m, &remaining); err != nil {
		return err
	}
	consume := func(n int) bool {
		if n > remaining {
			return false
		}
		remaining -= n
		return true
	}
	if err := chargeDeclaredTypeBytes(m, &remaining); err != nil {
		return err
	}
	types := internalReferenceTypes{decoded: m.Types, decodedSWAR: vector, declarationExtern: !vector && admitCore3BrOnNullDeclarations(m)}
	limit := 256
	if vector {
		limit = maxSWARLocals
	}
	for _, group := range m.Types {
		if len(group.SubTypes) != 1 {
			return unsupported("closed reference module recursive group")
		}
		sub := &group.SubTypes[0]
		if !sub.Final || sub.HasPrefix || len(sub.Supers) != 0 || sub.Metadata.Describes.Present() || sub.Metadata.Descriptor.Present() || len(sub.Comp.Fields) != 0 || sub.Comp.Array != (wasm.FieldType{}) {
			return unsupported("closed reference module plain function type")
		}
		if _, _, err := admitDeclaredControlSignature(&sub.Comp, types, true, true, vector); err != nil {
			return err
		}
	}
	var references [MaxFunctions]bool
	// The imported prefix is numeric. Internal reference carriers never cross
	// an imported signature, including an unused import or unreachable call.
	for _, im := range m.Imports {
		if im.Type.Kind != wasm.ExternFunc {
			continue // Numeric resource shape was checked with owned state.
		}
		// Include the separator in the retained "module.name" binding.
		index := im.Type.FuncType()
		if im.Type != wasm.NewFuncExternType(index) || index.Rec || uint64(index.Index) >= uint64(len(m.Types)) || !consume(6) {
			return unsupported("closed reference imported function binding")
		}
		sig := &m.Types[index.Index].SubTypes[0].Comp
		if vector {
			if err := swarImportSignature(sig); err != nil {
				return err
			}
		} else if err := integerNumericSignature(sig, true); err != nil {
			return err
		}
	}
	var importSignatures [MaxHostFunctionImports]*wasm.CompType
	importedOrdinal := 0
	for _, im := range m.Imports {
		if im.Type.Kind == wasm.ExternFunc {
			importSignatures[importedOrdinal] = &m.Types[im.Type.FuncType().Index].SubTypes[0].Comp
			importedOrdinal++
		}
	}
	hasReferenceFunction := false
	for i, index := range m.FuncTypes {
		if index.Rec || uint64(index.Index) >= uint64(len(m.Types)) {
			return unsupported("closed reference module function binding")
		}
		ft := &m.Types[index.Index].SubTypes[0].Comp
		reference, _, err := admitDeclaredControlSignature(ft, types, true, true, vector)
		if err != nil {
			return err
		}
		references[imported+i] = reference
		hasReferenceFunction = hasReferenceFunction || reference
		fn := &m.Code[i]
		if len(fn.BodyBytes) == 0 || len(fn.BodyBytes) > 64<<10 || len(fn.Body.Instrs) != 0 || len(fn.Body.BodyBytes) != 0 || len(fn.Locals.Runs) > limit || fn.LocalDeclBytes > MaxModuleBytes || !consume(len(fn.BodyBytes)) {
			return unsupported("closed reference module byte-backed function bound")
		}
		locals := len(ft.Params)
		// LocalDeclBytes is a source offset, not authority for hand-built
		// run storage. Charge at least the derived encoding, even zero runs.
		localBytes := 1
		if len(fn.Locals.Runs) >= 128 {
			localBytes++
		}
		for _, run := range fn.Locals.Runs {
			if uint64(run.Count) > uint64(limit-locals) {
				return unsupported("closed reference module local bound")
			}
			locals += int(run.Count)
			localBytes += 2 // count and numeric value type
			if run.Count >= 128 {
				localBytes++
			}
			if run.Type.Kind() == wasm.ValRef {
				localBytes += 5
			}
			if !vector && scalarType(run.Type, true) || vector && swarCarrierType(run.Type) {
				continue
			}
			if !internalFunctionReferenceWithTypes(run.Type, true, types) {
				return unsupported("closed reference module local carrier")
			}
		}
		if !consume(max(localBytes, int(fn.LocalDeclBytes))) {
			return unsupported("closed reference module local declaration byte bound")
		}
	}
	if !hasReferenceFunction {
		return unsupported("closed reference module requires internal reference functions")
	}
	numericTarget := func(index uint32) bool { return uint64(index) < uint64(imported+len(m.Code)) && !references[index] }
	if len(m.Tables) != 0 && m.Tables[0].Init != nil {
		value, err := tableElementExpression(*m.Tables[0].Init)
		if err != nil {
			return err
		}
		if !value.Null && !numericTarget(value.FuncIndex) {
			return unsupported("closed reference table initializer descriptor escape")
		}
	}
	for _, global := range m.Globals {
		if wasm.EqualValType(global.Type.Type, wasm.FuncRef) {
			value, err := tableElementExpression(global.Init)
			if err != nil {
				return err
			}
			if !value.Null && !numericTarget(value.FuncIndex) {
				return unsupported("closed reference cell initializer descriptor escape")
			}
		}
	}
	if m.Start != nil {
		index := uint64(*m.Start)
		if index >= uint64(imported+len(m.Code)) || !numericTarget(uint32(index)) || !consume(5) {
			return unsupported("closed reference start requires a bounded numeric function")
		}
		var ft *wasm.CompType
		if index < uint64(imported) {
			ft = importSignatures[index]
		} else {
			ft = &m.Types[m.FuncTypes[int(index)-imported].Index].SubTypes[0].Comp
		}
		if len(ft.Params) != 0 || len(ft.Results) != 0 {
			return unsupported("closed reference start requires a void signature")
		}
	}
	referenceExports := false
	for _, ex := range m.Exports {
		switch ex.Index.Kind {
		case wasm.ExternFunc:
			if uint64(ex.Index.Index) >= uint64(imported+len(m.Code)) {
				return unsupported("closed reference module function export index")
			}
			referenceExports = referenceExports || references[ex.Index.Index]
		case wasm.ExternMem:
			if uint64(ex.Index.Index) >= uint64(counts.memories+len(m.Memories)) {
				return unsupported("closed reference module owned memory export index")
			}
		case wasm.ExternGlobal:
			// Reference cells are private even though their values name numeric slots.
			if uint64(ex.Index.Index) >= uint64(counts.globals+len(m.Globals)) || int(ex.Index.Index) >= counts.globals && m.Globals[int(ex.Index.Index)-counts.globals].Type.Type.Kind() == wasm.ValRef {
				return unsupported("closed reference module owned numeric global export index")
			}
		default:
			return unsupported("closed reference module numeric exports only")
		}
	}
	if referenceExports {
		if len(m.Tables) != 0 {
			return unsupported("public reference wrapper requires table-free ephemeral transport")
		}
		for _, global := range m.Globals {
			if global.Type.Type.Kind() == wasm.ValRef {
				return unsupported("public reference wrapper requires reference-global-free ephemeral transport")
			}
		}
	}
	if err := admitClosedReferenceElements(m, &remaining, numericTarget); err != nil {
		return err
	}
	for _, custom := range m.Customs {
		if custom.Name == "metadata.code.branch_hint" {
			return unsupported("closed reference module branch metadata")
		}
	}
	if err := admitSWARDecodedMetadataBudget(m, &remaining); err != nil {
		return err
	}
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	for function, fn := range m.Code {
		r := wasm.ReaderFrom(fn.BodyBytes)
		budget, depth := 0, 0
		for r.HasNext() {
			op, err := r.Byte()
			if err != nil {
				return err
			}
			budget++
			switch op {
			case 0x02, 0x03, 0x04:
				depth++
				if depth > maxControlNesting {
					return unsupported("closed reference module control depth")
				}
			case 0x0b:
				depth--
			case 0x1c, 0x0e:
				probe := r
				count, err := probe.U32()
				if err != nil {
					return err
				}
				if op == 0x1c && count != 1 || op == 0x0e && count > maxTableEntries {
					return unsupported("closed reference module immediate count")
				}
				budget += int(count)
			}
			if budget > integerBudget {
				return unsupported("closed reference module instruction/immediate budget")
			}
			switch op {
			case 0x1f:
				return unsupported("closed reference module exception control is unsupported")
			}
			var imm wasm.InstructionImmediate
			if err := classifier.ClassifyInto(&r, op, &imm); err != nil {
				return err
			}
			if op == 0x24 && uint64(imm.Index) >= uint64(counts.globals) {
				owned := uint64(imm.Index) - uint64(counts.globals)
				if owned >= uint64(len(m.Globals)) || !m.Globals[owned].Type.Mutable {
					return unsupported("closed reference immutable owned global set")
				}
			}
			if op == 0xd2 && !numericTarget(imm.Index) {
				return unsupported("closed reference module ref.func escape")
			}
			if referenceExports && (op == 0x14 || op == 0x15) && !publicReferenceCallBoundary {
				return unsupported("public reference wrapper dynamic reference call requires owner scoping")
			}
			if (op == 0x11 || op == 0x13) && (len(m.Tables) != 1 || imm.Index2 != 0) {
				return unsupported("closed reference indirect call requires own table zero")
			}
			if (op == 0x25 || op == 0x26 || imm.Kind == wasm.InstrTableSize) && (len(m.Tables) != 1 || imm.Index != 0) {
				return unsupported("closed reference table operation requires own table zero")
			}
			if op == 0xfc {
				switch imm.Kind {
				case wasm.InstrTableInit, wasm.InstrElemDrop:
					if imm.Kind == wasm.InstrTableInit && len(m.Tables) != 1 || uint64(imm.Index) >= uint64(len(m.Elements)) || imm.Index2 != 0 {
						return unsupported("closed reference element operation requires exact ordinal and init own table")
					}
				case wasm.InstrTableCopy, wasm.InstrTableGrow, wasm.InstrTableFill:
					if len(m.Tables) != 1 || imm.Index != 0 || imm.Index2 != 0 {
						return unsupported("closed reference bulk operation requires own table zero")
					}
					if imm.Kind == wasm.InstrTableGrow && !m.Tables[0].Type.Limits.HasMax && m.Tables[0].Type.Limits.Min > MaxOwnedTableEntries/2 {
						return unsupported("closed reference no-max growth reservation bound")
					}
				}
			}
			if op == 0x14 || op == 0x15 || op == 0x11 || op == 0x13 {
				// Check even dead sites: private functions never become callable
				// descriptor targets, and the existing bridge transports numbers.
				if uint64(imm.Index) >= uint64(len(m.Types)) {
					return unsupported("closed reference numeric descriptor call type index")
				}
				sig := &m.Types[imm.Index].SubTypes[0].Comp
				if vector {
					if err := swarCallSignature(sig); err != nil {
						return err
					}
				} else if err := integerNumericSignature(sig, true); err != nil {
					return err
				}
				if op == 0x15 || op == 0x13 {
					source := &m.Types[m.FuncTypes[function].Index].SubTypes[0].Comp
					if err := admitReferenceTailResults(source, sig); err != nil {
						return err
					}
				}
			}
			if op == 0x12 && uint64(imm.Index) < uint64(imported) {
				// This includes unreachable tails. A private source may have reference
				// parameters/locals, but a numeric imported target must match its results.
				source := &m.Types[m.FuncTypes[function].Index].SubTypes[0].Comp
				if err := admitReferenceTailResults(source, importSignatures[imm.Index]); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
