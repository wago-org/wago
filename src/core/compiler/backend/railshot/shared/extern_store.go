package shared

import "github.com/wago-org/wago/src/core/compiler/wasm"

// ExternStoreMayCall accounts for receipt publication helpers in modules that
// need no collector of their own. Opaque words are not compact frame roots.
func ExternStoreMayCall(m *wasm.Module, kind wasm.InstrKind, index uint32) bool {
	if m == nil {
		return false
	}
	switch kind {
	case wasm.InstrGlobalSet:
		g, ok := m.GlobalTypeByIndex(index)
		return ok && wasm.EqualValType(wasm.GlobalValueType(g), wasm.ExternRef)
	case wasm.InstrTableSet, wasm.InstrTableFill, wasm.InstrTableCopy, wasm.InstrTableGrow, wasm.InstrTableInit:
		for i := 0; i < m.TableCount(); i++ {
			if table, ok := m.TableType(uint32(i)); ok && wasm.EqualValType(wasm.RefVal(table.Ref), wasm.ExternRef) {
				return true
			}
		}
	}
	return false
}
