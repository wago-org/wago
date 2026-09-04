//go:build wago_guardpage && (linux || darwin || windows) && (amd64 || arm64)

package runtime

import "fmt"

func validateGuardedJobMemorySizes(initialBytes, maxBytes int) error {
	if err := validateJobMemorySizes(initialBytes, maxBytes); err != nil {
		return err
	}
	if uint64(initialBytes) > uint64(maxClassicLinMemBytes) || uint64(maxBytes) > uint64(maxClassicLinMemBytes) {
		return fmt.Errorf("runtime: guarded linear-memory size exceeds memory32 limit: initial %d maximum %d", initialBytes, maxBytes)
	}
	const wasmPageBytes = 1 << 16
	if initialBytes%wasmPageBytes != 0 || maxBytes%wasmPageBytes != 0 {
		return fmt.Errorf("runtime: guarded linear-memory size is not wasm-page aligned: initial %d maximum %d", initialBytes, maxBytes)
	}
	return nil
}
