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
	if m == nil || len(m.Imports) > MaxProductionImports || m.ImportedGlobalCount()+len(m.Globals) > 256 || len(m.Data) > 256 || len(m.Elements) > MaxOwnedElementSegments {
		return unsupported("public initializer count bound")
	}
	for i, global := range m.Globals {
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
	for _, data := range m.Data {
		if data.Mode.Kind == wasm.DataActive {
			if err := admitModuleIntegerInitializer(data.Mode.Offset, wasm.I32, m, m.ImportedGlobalCount()+len(m.Globals)); err != nil {
				return err
			}
		}
	}
	for _, element := range m.Elements {
		if element.Mode.Kind == wasm.ElemActive {
			if err := admitModuleIntegerInitializer(element.Mode.Offset, wasm.I32, m, m.ImportedGlobalCount()+len(m.Globals)); err != nil {
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
