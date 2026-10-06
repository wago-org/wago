//go:build linux && !tinygo

package riscv64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// Public construction invokes the already-lowered framed wrapper after state
// initialization. Private byte compilers do not own a start-execution lifecycle.
func admitDefinedStart(m *wasm.Module) error {
	if m.Start == nil {
		return nil
	}
	imports := 0
	for _, im := range m.Imports {
		if im.Type.Kind == wasm.ExternFunc {
			imports++
		}
	}
	index := uint64(*m.Start)
	if index < uint64(imports) || index >= uint64(imports+len(m.Code)) {
		return unsupported("start requires a bounded defined function")
	}
	sig, ok := m.LocalFuncType(int(index) - imports)
	if !ok || len(sig.Params) != 0 || len(sig.Results) != 0 {
		return unsupported("start requires a void signature")
	}
	return nil
}
