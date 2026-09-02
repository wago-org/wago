//go:build !tinygo

package amd64

import "github.com/wago-org/wago/src/core/compiler/wasm"

func lowerFunctionResultTypes(sc *scratch, vals []wasm.ValType) []machineType {
	var types []machineType
	if len(vals) <= len(sc.functionResultTypeArena) {
		types = sc.functionResultTypeArena[:len(vals)]
	} else {
		types = make([]machineType, len(vals))
	}
	for i, val := range vals {
		types[i] = mtOf(val)
	}
	return types
}
