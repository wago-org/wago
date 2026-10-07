//go:build linux && !tinygo

package riscv64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// Public immutable cells use retained canonical descriptors. Imported cells
// require the retained-reference profile and frozen Runtime descriptor-owner roots.
func admitImmutableFunctionGlobal(global wasm.Global, m *wasm.Module, limit int) error {
	if global.Type.Mutable || !wasm.EqualValType(global.Type.Type, wasm.FuncRef) || len(m.Tables)+m.ImportedTableCount() > 1 {
		return unsupported("reference global requires immutable local funcref and retained references")
	}
	if len(global.Init.BodyBytes) > 0 && global.Init.BodyBytes[0] == 0x23 {
		if err := admitGlobalExpression(global.Init, m, wasm.FuncRef, limit); err != nil {
			return err
		}
		return nil
	}
	value, err := tableElementExpression(global.Init)
	if err != nil {
		return err
	}
	if !value.Null && uint64(value.FuncIndex) >= uint64(m.ImportedFuncCount()+len(m.Code)) {
		return unsupported("reference global function index")
	}
	return nil
}

// Export chains are validated before folding. Imported-function targets need
// same-store native binding validation at public instantiation.
func admitReferenceGlobalExport(m *wasm.Module, index uint32) error {
	imported := m.ImportedGlobalCount()
	// Each copy reduces the global index. The public profile has at most 256
	// globals; no recursion or allocation is needed to find the literal target.
	for remaining := 256; remaining > 0; remaining-- {
		if uint64(index) < uint64(imported) {
			typ, ok := m.GlobalTypeByIndex(index)
			if !ok || !admitImportedFunctionGlobal(m, typ) {
				return unsupported("reference global import requires immutable funcref and retained references")
			}
			return nil
		}
		if uint64(index) >= uint64(imported+len(m.Globals)) {
			return unsupported("reference global export index")
		}
		global := m.Globals[int(index)-imported]
		if err := admitImmutableFunctionGlobal(global, m, int(index)); err != nil {
			return err
		}
		if global.Init.BodyBytes[0] == 0x23 {
			r := wasm.ReaderFrom(global.Init.BodyBytes[1:])
			index, _ = r.U32()
			continue
		}
		return nil
	}
	return unsupported("reference global initializer chain bound")
}

func admitImportedFunctionGlobal(m *wasm.Module, typ wasm.GlobalType) bool {
	return !typ.Mutable && wasm.EqualValType(typ.Type, wasm.FuncRef) && len(m.Tables)+m.ImportedTableCount() <= 1
}
