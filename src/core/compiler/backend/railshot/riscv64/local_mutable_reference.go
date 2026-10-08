//go:build linux && !tinygo

package riscv64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// Mutable local reference cells have no foreign descriptor ingress: no table,
// imported reference global, reference signature or exported mutable cell.
// Every nonnull value therefore names this instance's canonical directory.
func admitLocalMutableFunctionGlobals(m *wasm.Module) (bool, error) {
	has := false
	for _, g := range m.Globals {
		has = has || g.Type.Mutable && wasm.EqualValType(g.Type.Type, wasm.FuncRef)
	}
	if !has {
		return false, nil
	}
	if len(m.Tables) != 0 || m.ImportedTableCount() != 0 || len(m.Types) > MaxFunctions || len(m.Exports) > MaxFunctions {
		return false, unsupported("local mutable references require bounded table-free private cells")
	}
	for _, im := range m.Imports {
		if im.Type.Kind == wasm.ExternGlobal && !swarCarrierType(im.Type.GlobalType().Type) {
			return false, unsupported("local mutable references exclude imported reference globals")
		}
	}
	for _, group := range m.Types {
		if len(group.SubTypes) != 1 {
			return false, unsupported("local mutable reference type group")
		}
		sig := &group.SubTypes[0].Comp
		if sig.Kind != wasm.CompFunc || len(sig.Params) > 256 || len(sig.Results) > 256 {
			return false, unsupported("local mutable reference signature shape")
		}
		for _, list := range [][]wasm.ValType{sig.Params, sig.Results} {
			for _, typ := range list {
				if !swarCarrierType(typ) {
					return false, unsupported("local mutable references exclude reference signatures")
				}
			}
		}
	}
	for _, ex := range m.Exports {
		if ex.Index.Kind != wasm.ExternGlobal {
			continue
		}
		typ, ok := m.GlobalTypeByIndex(ex.Index.Index)
		if !ok {
			return false, unsupported("local mutable reference export index")
		}
		if typ.Mutable && wasm.EqualValType(typ.Type, wasm.FuncRef) {
			return false, unsupported("mutable reference global export is pending")
		}
	}
	return true, nil
}

func admitLocalFunctionGlobal(g wasm.Global, m *wasm.Module, limit int, mutable bool) error {
	if g.Type.Mutable {
		if !mutable {
			return unsupported("mutable reference global requires closed local profile")
		}
		// Reuse only the initializer rules. Export admission still calls the
		// immutable helper directly, so this does not permit mutable aliases.
		g.Type.Mutable = false
	}
	return admitImmutableFunctionGlobal(g, m, limit)
}
