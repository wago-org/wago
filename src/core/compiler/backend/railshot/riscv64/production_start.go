//go:build linux && !tinygo

package riscv64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// Public construction invokes a lowered framed wrapper or normalized host
// binding after state initialization. Private byte compilers do not own a start
// execution lifecycle.
func admitProductionStart(m *wasm.Module) error {
	if m.Start == nil {
		return nil
	}
	imports := 0
	var importedSig *wasm.CompType
	for entry, im := range m.Imports {
		if im.Type.Kind == wasm.ExternFunc {
			if uint64(imports) == uint64(*m.Start) {
				var ok bool
				importedSig, ok = m.ImportFuncType(entry)
				if !ok {
					return unsupported("start requires a bounded imported signature")
				}
			}
			imports++
		}
	}
	index := uint64(*m.Start)
	if index < uint64(imports) {
		if importedSig == nil || len(importedSig.Params) != 0 || len(importedSig.Results) != 0 {
			return unsupported("imported start requires a void host signature")
		}
		return nil
	}
	if index >= uint64(imports+len(m.Code)) {
		return unsupported("start requires a bounded defined function")
	}
	sig, ok := m.LocalFuncType(int(index) - imports)
	if !ok || len(sig.Params) != 0 || len(sig.Results) != 0 {
		return unsupported("start requires a void signature")
	}
	return nil
}
