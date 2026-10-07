//go:build linux && !tinygo

package riscv64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// Immutable local cells retain only this instance's canonical descriptors. The
// retained table profile owns their targets; mutable/imported/exported reference
// globals and reference-valued signatures need separate lifecycle qualification.
func admitImmutableFunctionGlobal(global wasm.Global, m *wasm.Module) error {
	if global.Type.Mutable || !wasm.EqualValType(global.Type.Type, wasm.FuncRef) || len(m.Tables)+m.ImportedTableCount() != 1 {
		return unsupported("reference global requires immutable local funcref and retained table")
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
