package wago

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func hasStructuralHeapTypes(m *wasm.Module) bool {
	if m == nil {
		return false
	}
	for gi := range m.Types {
		for si := range m.Types[gi].SubTypes {
			kind := m.Types[gi].SubTypes[si].Comp.Kind
			if kind == wasm.CompStruct || kind == wasm.CompArray {
				return true
			}
		}
	}
	return false
}
