//go:build linux && !tinygo

package riscv64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// Immutable local cells retain only this instance's canonical descriptors. The
// retained table profile owns their targets; mutable/imported reference globals
// and reference-valued signatures need separate lifecycle qualification.
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

// Native imports use importer-owned proxies on RV64. Public token identity for
// those proxies needs separate qualification; export only null or local targets.
func admitReferenceGlobalExport(m *wasm.Module, index uint32) error {
	imported := m.ImportedGlobalCount()
	if uint64(index) < uint64(imported) || uint64(index) >= uint64(imported+len(m.Globals)) {
		return unsupported("reference global export index")
	}
	global := m.Globals[int(index)-imported]
	if err := admitImmutableFunctionGlobal(global, m); err != nil {
		return err
	}
	value, err := tableElementExpression(global.Init)
	if err != nil {
		return err
	}
	if !value.Null && uint64(value.FuncIndex) < uint64(m.ImportedFuncCount()) {
		return unsupported("reference global export requires a local function target")
	}
	return nil
}
