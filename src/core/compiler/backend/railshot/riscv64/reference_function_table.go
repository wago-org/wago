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
