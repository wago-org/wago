//go:build (linux || darwin) && arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestStoreAddressHintDirtyParameter(t *testing.T) {
	old := storeAddressHintEnabled
	defer func() { storeAddressHintEnabled = old }()
	m := modMem(t, 1, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, []byte{
		0x00,
		0x20, 0x00, 0x41, 0x2a, 0x36, 0x02, 0x00,
		0x20, 0x00, 0x41, 0x11, 0x36, 0x02, 0x04,
		0x20, 0x00, 0x28, 0x02, 0x00,
		0x0b,
	})
	for _, enabled := range []bool{false, true} {
		storeAddressHintEnabled = enabled
		for _, regABI := range []bool{false, true} {
			got, err := runArm64WrapperWithOptions(t, m, CompileOptions{Optimizations: map[string]bool{"value-facts": true, "reg-abi": regABI}}, uint64(1)<<32)
			if err != nil || got != 42 {
				t.Fatalf("hint=%t regABI=%t: got %d, %v; want 42", enabled, regABI, got, err)
			}
		}
	}
}
