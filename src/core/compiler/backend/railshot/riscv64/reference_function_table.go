//go:build linux && !tinygo

package riscv64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// This allocation-free prerequisite bounds the new owned table representation
// before validation or copying. The enclosing proof checks every numeric target
// after it has classified the complete physical function namespace.
func admitClosedReferenceOwnedTableState(m *wasm.Module, remaining *int) error {
	if len(m.Tables) == 0 {
		return nil // Preserve the already qualified table-free byte budget.
	}
	if len(m.Tables) != 1 {
		return unsupported("closed reference single own table required")
	}
	consume := func(n int) bool {
		if n > *remaining {
			return false
		}
		*remaining -= n
		return true
	}
	table := &m.Tables[0]
	t := table.Type
	if t.Limits.Addr64 || !wasm.EqualValType(wasm.RefVal(t.Ref), wasm.FuncRef) || t.Limits.Min > MaxOwnedTableEntries || t.Limits.Max > MaxOwnedTableEntries || t.Limits.HasMax && t.Limits.Max < t.Limits.Min || !t.Limits.HasMax && t.Limits.Max != 0 || !consume(21) {
		return unsupported("closed reference bounded nullable own table32 required")
	}
	if table.Init != nil {
		if _, err := tableElementExpression(*table.Init); err != nil {
			return err
		}
		if !consume(len(table.Init.BodyBytes)) {
			return unsupported("closed reference table initializer byte bound")
		}
	}
	for _, e := range m.Elements {
		if e.Mode.Kind == wasm.ElemActive {
			if len(e.Mode.Offset.Instrs) != 0 || len(e.Mode.Offset.BodyBytes) > 7 {
				return unsupported("closed reference table offset literal bound")
			}
			if _, err := plainConst(e.Mode.Offset, false); err != nil {
				return err
			}
		}
		if !consume(16 + len(e.Mode.Offset.BodyBytes)) {
			return unsupported("closed reference element metadata byte bound")
		}
	}
	return nil
}

// All original payloads, including unused declarative segments, are proved
// before validation discards them. Expression elements have no global or foreign
// owner ingress: only exact nullable funcref literals may name numeric slots.
func admitClosedReferenceElements(m *wasm.Module, remaining *int, numericTarget func(uint32) bool) error {
	if len(m.Elements) > MaxOwnedElementSegments {
		return unsupported("closed reference element directory bound")
	}
	consume := func(n int) bool {
		if n > *remaining {
			return false
		}
		*remaining -= n
		return true
	}
	values := MaxOwnedTableEntries
	for _, e := range m.Elements {
		mode := e.Mode.Kind
		modeOK := mode == wasm.ElemDeclarative || mode == wasm.ElemPassive || len(m.Tables) != 0 && mode == wasm.ElemActive
		if !modeOK || e.Mode.Table != 0 || len(e.Mode.Offset.Instrs) != 0 || mode != wasm.ElemActive && len(e.Mode.Offset.BodyBytes) != 0 {
			return unsupported("closed reference exact confined element mode")
		}
		tableFreePassive := len(m.Tables) == 0 && mode == wasm.ElemPassive
		if tableFreePassive && (e.Kind.Kind != wasm.ElemFuncs || len(e.Kind.Funcs) == 0) {
			return unsupported("closed reference table-free passive declaration requires a legacy target")
		}
		if tableFreePassive && !consume(16) {
			return unsupported("closed reference passive element metadata byte bound")
		}
		switch e.Kind.Kind {
		case wasm.ElemFuncs:
			if e.Kind.Ref != (wasm.RefType{}) || len(e.Kind.Exprs) != 0 || len(e.Kind.Funcs) > values || !consume(4*len(e.Kind.Funcs)) {
				return unsupported("closed reference canonical legacy elements bound")
			}
			values -= len(e.Kind.Funcs)
			for _, index := range e.Kind.Funcs {
				if !numericTarget(uint32(index)) {
					return unsupported("closed reference element descriptor escape")
				}
			}
		case wasm.ElemFuncExprs, wasm.ElemTypedExprs:
			if len(e.Kind.Funcs) != 0 || e.Kind.Kind == wasm.ElemFuncExprs && e.Kind.Ref != (wasm.RefType{}) || e.Kind.Kind == wasm.ElemTypedExprs && !wasm.EqualValType(wasm.RefVal(e.Kind.Ref), wasm.FuncRef) || len(e.Kind.Exprs) > values {
				return unsupported("closed reference exact nullable funcref expression elements")
			}
			values -= len(e.Kind.Exprs)
			// The table proof already charges all original segment headers. A
			// table-free declarative expression retains its exact type witness.
			if len(m.Tables) == 0 && !consume(16) || e.Kind.Kind == wasm.ElemTypedExprs && !consume(1) {
				return unsupported("closed reference expression element metadata byte bound")
			}
			for _, expr := range e.Kind.Exprs {
				value, err := tableElementExpression(expr)
				if err != nil {
					return err
				}
				if !consume(len(expr.BodyBytes)) {
					return unsupported("closed reference expression element byte bound")
				}
				if !value.Null && !numericTarget(value.FuncIndex) {
					return unsupported("closed reference expression element descriptor escape")
				}
			}
		default:
			return unsupported("closed reference element kind")
		}
	}
	return nil
}
