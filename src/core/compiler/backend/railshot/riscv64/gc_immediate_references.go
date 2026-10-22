//go:build linux && !tinygo

package riscv64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// Immediate-reference modules cannot create or receive a collector object.
// Their any/eq storage contains only null and i31 values. Extern conversions
// remain internal identity operations: no extern carrier crosses a boundary.
func admitRV64ImmediateReferenceModule(m *wasm.Module) bool {
	if m == nil || len(m.Code) == 0 || len(m.Code) > MaxFunctions || len(m.Types) == 0 || len(m.Types) > MaxFunctions || len(m.Globals) > 256 || len(m.Tables) > 2 || len(m.Elements) > MaxOwnedElementSegments || len(m.Tags) != 0 || len(m.StringRefs) != 0 {
		return false
	}
	seenReference := false
	carrier := func(typ wasm.ValType) bool {
		return scalarType(typ, true) || typ.Kind() == wasm.ValRef && !typ.Ref().Exact() && typ.Ref().Heap().Kind() == wasm.HeapAbs && typ.Ref().Heap().Abs() == wasm.HeapI31
	}
	for _, group := range m.Types {
		for _, sub := range group.SubTypes {
			if !sub.Final || len(sub.Supers) != 0 || sub.Metadata.Describes.Present() || sub.Metadata.Descriptor.Present() || sub.Comp.Kind != wasm.CompFunc {
				return false
			}
			for _, list := range [][]wasm.ValType{sub.Comp.Params, sub.Comp.Results} {
				for _, typ := range list {
					seenReference = seenReference || immediateCollectorType(typ)
					if !carrier(typ) {
						return false
					}
				}
			}
		}
	}
	for _, im := range m.Imports {
		switch im.Type.Kind {
		case wasm.ExternFunc:
			index := im.Type.FuncType()
			ft, ok := m.TypeFunc(index.Index)
			if index.Rec || !ok {
				return false
			}
			for _, list := range [][]wasm.ValType{ft.Params, ft.Results} {
				for _, typ := range list {
					if !scalarType(typ, true) {
						return false
					}
				}
			}
		case wasm.ExternGlobal:
			if !scalarType(im.Type.GlobalType().Type, true) {
				return false
			}
		case wasm.ExternMem:
		default:
			return false
		}
	}
	for _, global := range m.Globals {
		seenReference = seenReference || immediateCollectorType(global.Type.Type)
		if global.Type.Type.Kind() == wasm.ValRef && (!immediateCollectorType(global.Type.Type) || !immediateReferenceInitializer(global.Init, m)) {
			return false
		}
	}
	for _, table := range m.Tables {
		seenReference = true
		if !immediateCollectorType(wasm.RefVal(table.Type.Ref)) || table.Type.Limits.Addr64 || table.Type.Limits.Min > MaxOwnedTableEntries || table.Type.Limits.HasMax && table.Type.Limits.Max > MaxOwnedTableEntries || table.Init != nil && !immediateReferenceInitializer(*table.Init, m) {
			return false
		}
	}
	for _, elem := range m.Elements {
		seenReference = true
		if elem.Kind.Kind != wasm.ElemTypedExprs || !immediateCollectorType(wasm.RefVal(elem.Kind.Ref)) || len(elem.Kind.Funcs) != 0 {
			return false
		}
		for _, expr := range elem.Kind.Exprs {
			if !immediateReferenceInitializer(expr, m) {
				return false
			}
		}
	}
	for _, ex := range m.Exports {
		// A foreign writer could put an object in an any/eq table or global.
		if ex.Index.Kind == wasm.ExternTable || ex.Index.Kind == wasm.ExternGlobal {
			var typ wasm.ValType
			if ex.Index.Kind == wasm.ExternTable {
				table, ok := m.TableType(ex.Index.Index)
				if !ok {
					return false
				}
				typ = wasm.RefVal(table.Ref)
			} else {
				global, ok := m.GlobalTypeByIndex(ex.Index.Index)
				if !ok {
					return false
				}
				typ = global.Type
			}
			if typ.Kind() == wasm.ValRef && !wasm.EqualValType(typ, wasm.I31Ref) {
				return false
			}
		}
	}
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	for _, fn := range m.Code {
		r := wasm.NewReader(fn.BodyBytes)
		for r.HasNext() {
			op, err := r.Byte()
			if err != nil {
				return false
			}
			var nullHeap int64
			if op == 0xd0 {
				probe := *r
				heap, err := probe.S33()
				if err != nil {
					return false
				}
				nullHeap = heap
			}
			var imm wasm.InstructionImmediate
			if classifier.ClassifyInto(r, op, &imm) != nil {
				return false
			}
			seenReference = seenReference || op == 0xfb
			if op == 0xd0 {
				if nullHeap >= 0 {
					return false
				}
				heap := wasm.AbsHeapType(byte(nullHeap & 0x7f))
				collectorNull := rootedAbstractObjectHeap(heap) || heap == wasm.HeapI31
				if !collectorNull && heap != wasm.HeapExtern && heap != wasm.HeapNoExtern {
					return false
				}
				seenReference = seenReference || collectorNull
			}
			if op == 0xfb {
				switch imm.Kind {
				case wasm.InstrRefI31, wasm.InstrI31GetS, wasm.InstrI31GetU, wasm.InstrAnyConvertExtern, wasm.InstrExternConvertAny:
				case wasm.InstrRefTest, wasm.InstrRefCast:
					if !imm.HasHeapType || imm.HeapType.Kind() != wasm.HeapAbs || !rootedAbstractObjectHeap(imm.HeapType.Abs()) && imm.HeapType.Abs() != wasm.HeapI31 {
						return false
					}
				default:
					return false
				}
			}
			switch imm.Kind {
			case wasm.InstrRefFunc, wasm.InstrCallRef, wasm.InstrReturnCallRef, wasm.InstrCallIndirect, wasm.InstrReturnCallIndirect:
				return false
			}
		}
	}
	return seenReference
}

func immediateCollectorType(typ wasm.ValType) bool {
	if typ.Kind() != wasm.ValRef || typ.Ref().Exact() || typ.Ref().Heap().Kind() != wasm.HeapAbs {
		return false
	}
	heap := typ.Ref().Heap().Abs()
	return heap == wasm.HeapI31 || rootedAbstractObjectHeap(heap)
}

// Typed Wasm validation checks the final value and every global index. This
// inventory additionally proves that the expression cannot allocate or convert
// an opaque host value. The compiler separately bounds expression work.
func immediateReferenceInitializer(expr wasm.Expr, m *wasm.Module) bool {
	if len(expr.Instrs) != 0 || len(expr.BodyBytes) > 8192 {
		return false
	}
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	r := wasm.NewReader(expr.BodyBytes)
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return false
		}
		if op == 0x0b {
			return !r.HasNext()
		}
		var imm wasm.InstructionImmediate
		if classifier.ClassifyInto(r, op, &imm) != nil {
			return false
		}
		switch imm.Kind {
		case wasm.InstrI32Const, wasm.InstrI64Const, wasm.InstrI32Add, wasm.InstrI32Sub, wasm.InstrI32Mul, wasm.InstrI64Add, wasm.InstrI64Sub, wasm.InstrI64Mul, wasm.InstrGlobalGet, wasm.InstrRefNull, wasm.InstrRefI31:
		default:
			return false
		}
	}
	return true
}
