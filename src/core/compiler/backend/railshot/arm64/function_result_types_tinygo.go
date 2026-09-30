//go:build arm64 && tinygo

package arm64

import "github.com/wago-org/wago/src/core/compiler/wasm"

func lowerFunctionResultTypes(_ *scratch, vals []wasm.ValType) []machineType {
	return typesOfVals(vals)
}
