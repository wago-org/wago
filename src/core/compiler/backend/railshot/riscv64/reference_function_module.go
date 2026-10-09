//go:build linux && !tinygo

package riscv64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// This private decoded-module prerequisite proves that reference-bearing
// functions cannot escape through an export, descriptor, import or resource.
// It does not authorize production staging or general reference ownership.
// Every bound precedes frontend validation and derived carrier allocation.
func admitClosedReferenceFunctionModule(m *wasm.Module, vector bool) error {
	if m == nil || len(m.Imports) > MaxProductionImports || len(m.Code) == 0 || len(m.Code) > MaxFunctions || len(m.FuncTypes) != len(m.Code) || len(m.Types) > MaxFunctions || len(m.Exports) > MaxFunctions || len(m.Elements) > MaxOwnedElementSegments || len(m.Customs) > MaxFunctions {
		return unsupported("closed reference module directory bound")
	}
	counts, err := countClosedReferenceImports(m)
	if err != nil {
		return err
	}
	imported := counts.functions
	if m.UsesCompactImports || len(m.Tables) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || m.BranchHints != nil {
		return unsupported("closed reference module external state")
	}
	remaining := MaxModuleBytes
	if err := admitClosedReferenceNumericStateWithImports(m, vector, &remaining, counts); err != nil {
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
	types := internalReferenceTypes{decoded: m.Types, decodedSWAR: vector}
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
	for _, ex := range m.Exports {
		switch ex.Index.Kind {
		case wasm.ExternFunc:
			if !numericTarget(ex.Index.Index) {
				return unsupported("closed reference module numeric function exports only")
			}
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
	elements := 0
	for _, e := range m.Elements {
		if e.Mode.Kind != wasm.ElemDeclarative || e.Mode.Table != 0 || len(e.Mode.Offset.Instrs) != 0 || len(e.Mode.Offset.BodyBytes) != 0 || e.Kind.Kind != wasm.ElemFuncs || e.Kind.Ref != (wasm.RefType{}) || len(e.Kind.Exprs) != 0 || len(e.Kind.Funcs) > MaxOwnedTableEntries-elements || !consume(4*len(e.Kind.Funcs)) {
			return unsupported("closed reference module numeric declarative elements only")
		}
		elements += len(e.Kind.Funcs)
		for _, idx := range e.Kind.Funcs {
			if !numericTarget(uint32(idx)) {
				return unsupported("closed reference module descriptor escape")
			}
		}
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
			case 0x11, 0x13, 0x15:
				return unsupported("closed reference module excludes indirect and reference-tail calls")
			}
			var imm wasm.InstructionImmediate
			if err := classifier.ClassifyInto(&r, op, &imm); err != nil {
				return err
			}
			if op == 0xd2 && !numericTarget(imm.Index) {
				return unsupported("closed reference module ref.func escape")
			}
			if op == 0x14 {
				// Check even dead sites: private functions never become callable
				// descriptor targets, and the existing bridge transports numbers.
				if uint64(imm.Index) >= uint64(len(m.Types)) {
					return unsupported("closed reference numeric call_ref type index")
				}
				sig := &m.Types[imm.Index].SubTypes[0].Comp
				if vector {
					if err := swarCallSignature(sig); err != nil {
						return err
					}
				} else if err := integerNumericSignature(sig, true); err != nil {
					return err
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
