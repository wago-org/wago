//go:build amd64 && tinygo

package amd64

import "github.com/wago-org/wago/src/core/compiler/wasm"

func lowerFunctionResultTypes(_ *scratch, vals []wasm.ValType) []machineType {
	return typesOfVals(vals)
}
