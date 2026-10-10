//go:build linux && !tinygo

package riscv64

import "github.com/wago-org/wago/src/core/compiler/wasm"

const MaxIntegerInitializerBytes = 128
const MaxIntegerInitializerValues = 8

// ValidateIntegerInitializer checks the public integer constant-expression
// profile without evaluating values or allocating an operand stack. resolve
// supplies only immutable global types available in the initializer context. Source and artifact admission
// share this parser before the generic constant evaluator can fold or execute.
func ValidateIntegerInitializer(body []byte, expected wasm.ValType, resolve func(uint32) (wasm.ValType, bool)) error {
	if !scalarType(expected, false) || len(body) == 0 || len(body) > MaxIntegerInitializerBytes {
		return unsupported("bounded integer initializer required")
	}
	var stack [MaxIntegerInitializerValues]wasm.ValType
	depth := 0
	r := wasm.ReaderFrom(body)
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return err
		}
		var typ wasm.ValType
		switch op {
		case 0x0b:
			if r.HasNext() || depth != 1 || !wasm.EqualValType(stack[0], expected) {
				return unsupported("integer initializer result/end")
			}
			return nil
		case 0x41:
			_, err = r.I32()
			typ = wasm.I32
		case 0x42:
			_, err = r.I64()
			typ = wasm.I64
		case 0x23:
			if resolve == nil {
				return unsupported("integer initializer has no available globals")
			}
			var index uint32
			index, err = r.U32()
			if err == nil {
				var ok bool
				typ, ok = resolve(index)
				if !ok || !scalarType(typ, false) {
					return unsupported("integer initializer requires an available immutable integer")
				}
			}
		case 0x6a, 0x6b, 0x6c, 0x7c, 0x7d, 0x7e:
			typ = wasm.I32
			if op >= 0x7c {
				typ = wasm.I64
			}
			if depth < 2 || !wasm.EqualValType(stack[depth-1], typ) || !wasm.EqualValType(stack[depth-2], typ) {
				return unsupported("integer initializer operand type/underflow")
			}
			depth--
			continue
		default:
			return unsupported("integer initializer opcode")
		}
		if err != nil {
			return err
		}
		if depth == len(stack) {
			return unsupported("integer initializer operand bound")
		}
		stack[depth] = typ
		depth++
	}
	return unsupported("integer initializer missing end")
}

func admitModuleIntegerInitializer(expr wasm.Expr, expected wasm.ValType, m *wasm.Module, limit int) error {
	if limit < 0 || limit > m.ImportedGlobalCount()+len(m.Globals) {
		return unsupported("initializer global scope")
	}
	if len(expr.Instrs) != 0 {
		return unsupported("integer initializer must be byte-backed")
	}
	return ValidateIntegerInitializer(expr.BodyBytes, expected, func(index uint32) (wasm.ValType, bool) {
		if uint64(index) >= uint64(limit) {
			return wasm.ValType{}, false
		}
		typ, ok := m.GlobalTypeByIndex(index)
		return typ.Type, ok && !typ.Mutable
	})
}

// AdmitPublicInitializers runs on the original decoded expressions before
// frontend folding. Literal folding must not erase source resource limits.
func AdmitPublicInitializers(m *wasm.Module) error {
	if m == nil || len(m.Imports) > MaxProductionImports || len(m.Memories) > MaxProductionMemories || m.MemCount() > MaxProductionMemories || len(m.Tables)+m.ImportedTableCount() > 1 && !isCore3RefIsNullExecution(m) && !admitStateOnlyTwoTable32(m, true, true) && !admitOwnedTwoTable32Size(m, true, true) && !admitOwnedTwoTable32GetSet(m, true, true) && !admitOwnedTwoTable32Grow(m, true, true) && !admitOwnedTwoTable32Fill(m, true, true) && !admitOwnedTwoTable32Copy(m, true, true) && !admitOwnedTwoTable32CrossCopy(m, true, true) && !admitOwnedTwoTable32InitDrop(m, true, true) && !admitOwnedTwoTable32Indirect(m, true, true) && !admitOwnedTwoTable64Size(m, true, true) && !admitMixedTable64Size(m, true, true) && !admitMixedTable64GetSet(m, true, true) && !admitMixedTable64Grow(m, true, true) && !admitMixedTable64Fill(m, true, true) && !admitMixedTable64Copy(m, true, true) && !admitOwnedTwoTable64GetSet(m, true, true) && !admitOwnedTwoTable64Grow(m, true, true) && !admitOwnedTwoTable64Fill(m, true, true) && !admitOwnedTwoTable64Copy(m, true, true) && !admitOwnedTwoTable64CrossCopy(m, true, true) && !admitOwnedTwoTable64InitDrop(m, true, true) && !admitOwnedTwoTable64Indirect(m, true, true) || m.ImportedGlobalCount()+len(m.Globals) > 256 || len(m.Data) > 256 || len(m.Elements) > MaxOwnedElementSegments {
		return unsupported("public initializer count bound")
	}
	for _, table := range m.Tables {
		if table.Init != nil {
			global, err := admitTableInitializer(*table.Init, m)
			if err != nil {
				return err
			}
			if global {
				if len(m.Elements) >= MaxOwnedElementSegments || table.Type.Limits.Min > MaxOwnedTableEntries {
					return unsupported("table global initializer aggregate bound")
				}
				remaining := uint64(MaxOwnedTableEntries) - table.Type.Limits.Min
				for _, element := range m.Elements {
					entries := uint64(len(element.Kind.Funcs) + len(element.Kind.Exprs))
					if entries > remaining {
						return unsupported("table global initializer aggregate bound")
					}
					remaining -= entries
				}
			}
		}
	}
	localMutableReferences, err := admitLocalMutableFunctionGlobals(m)
	if err != nil {
		return err
	}
	hasReferenceGlobal := false
	for _, im := range m.Imports {
		if im.Type.Kind == wasm.ExternGlobal && wasm.EqualValType(im.Type.GlobalType().Type, wasm.FuncRef) {
			if !admitImportedFunctionGlobal(m, im.Type.GlobalType()) {
				return unsupported("reference global import requires immutable funcref and retained table")
			}
			hasReferenceGlobal = true
		}
	}
	for i, global := range m.Globals {
		if wasm.EqualValType(global.Type.Type, wasm.FuncRef) {
			hasReferenceGlobal = true
			if err := admitLocalFunctionGlobal(global, m, m.ImportedGlobalCount()+i, localMutableReferences); err != nil {
				return err
			}
			continue
		}
		if scalarType(global.Type.Type, false) {
			if err := admitModuleIntegerInitializer(global.Init, global.Type.Type, m, m.ImportedGlobalCount()+i); err != nil {
				return err
			}
		} else if len(global.Init.BodyBytes) > 0 && global.Init.BodyBytes[0] == 0x23 {
			if err := admitGlobalExpression(global.Init, m, global.Type.Type, m.ImportedGlobalCount()+i); err != nil {
				return err
			}
		}
	}
	if hasReferenceGlobal {
		if len(m.Exports) > MaxFunctions {
			return unsupported("reference-global export count bound")
		}
		for _, ex := range m.Exports {
			if ex.Index.Kind == wasm.ExternGlobal {
				if typ, ok := m.GlobalTypeByIndex(ex.Index.Index); ok && wasm.EqualValType(typ.Type, wasm.FuncRef) {
					if err := admitReferenceGlobalExport(m, ex.Index.Index); err != nil {
						return err
					}
				}
			}
		}
	}
	for _, data := range m.Data {
		if data.Mode.Kind == wasm.DataActive {
			memory, ok := m.MemoryType(uint32(data.Mode.Mem))
			if !ok {
				return unsupported("active data memory index")
			}
			if err := admitMemoryType(memory, true, true); err != nil {
				return err
			}
			if err := admitModuleIntegerInitializer(data.Mode.Offset, wasm.MemoryAddrType(memory), m, m.ImportedGlobalCount()+len(m.Globals)); err != nil {
				return err
			}
		}
	}
	for _, element := range m.Elements {
		if element.Mode.Kind == wasm.ElemActive {
			offsetType := wasm.I32
			if admitOwnedTable64ActiveElement(m, true, true) {
				offsetType = wasm.I64
			}
			if err := admitModuleIntegerInitializer(element.Mode.Offset, offsetType, m, m.ImportedGlobalCount()+len(m.Globals)); err != nil {
				return err
			}
		}
	}
	return nil
}

func admittedPublicScalarConst(expr wasm.Expr, typ wasm.ValType, production, public bool, imports []globalSpec) (uint64, error) {
	value, err := admittedScalarConst(expr, typ, production, imports)
	if err == nil || !public || !scalarType(typ, false) {
		return value, err
	}
	if len(expr.Instrs) != 0 {
		return 0, unsupported("integer initializer must be byte-backed")
	}
	err = ValidateIntegerInitializer(expr.BodyBytes, typ, func(index uint32) (wasm.ValType, bool) {
		if uint64(index) >= uint64(len(imports)) {
			return wasm.ValType{}, false
		}
		g := imports[index]
		return g.typ, !g.mutable
	})
	// Runtime metadata carries the original program; this value is typing only.
	return 0, err
}
