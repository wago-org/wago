package wago

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"os"
)

// decodeCompilerSource leaves body syntax to the validator on the successful
// compilation path. The ordinary decoder remains authoritative for malformed
// inputs, including rejection phase, offsets and ordering among decode errors.
func decodeCompilerSource(data []byte, features wasm.ValidationFeatures) (*wasm.Module, bool, error) {
	if os.Getenv("WAGO_FUSED_DECODE_VALIDATION") == "0" {
		m, err := wasm.DecodeModuleWithFeatures(data, features)
		return m, false, err
	}
	m, err := wasm.DecodeModuleForValidationWithFeatures(data, features)
	if err != nil {
		m, err = wasm.DecodeModuleWithFeatures(data, features)
		return m, false, err
	}
	return m, true, nil
}
