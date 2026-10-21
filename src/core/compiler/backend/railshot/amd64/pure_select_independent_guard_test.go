//go:build linux && amd64 && wago_guardpage

package amd64

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestPureSelectIndependentGuardedTrapOrder(t *testing.T) {
	saved := pureSelectEnabled
	defer func() { pureSelectEnabled = saved }()
	for _, cheapLoad := range []bool{false, true} {
		body := []byte{0, 0x41, 0, 0x41, 29, 0x36, 2, 0}
		if cheapLoad {
			body = append(body, 0x41, 7)
		} else {
			body = append(body, 0x20, 0, 0x28, 2, 0)
		}
		body = append(body, 0x41, 17, 0x6c, 0x41, 3, 0x73, 0x41, 19, 0x6c)
		if cheapLoad {
			body = append(body, 0x20, 0, 0x28, 2, 0)
		} else {
			body = append(body, 0x41, 7)
		}
		body = append(body, 0x41, 1, 0x41, 0, 0x6e, 0x1b, 0x0b)
		m := modMem(t, 1, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
		for _, enabled := range []bool{false, true} {
			pureSelectEnabled = enabled
			// This helper uses NewJobMemoryGuarded and CallGuarded, so the
			// inaccessible address is inside the registered guard reservation.
			if err := callStoreValueGuarded(t, m, 65536); err == nil || !strings.Contains(err.Error(), "out of bounds") {
				t.Fatalf("cheapLoad=%v enabled=%v: trap=%v, want earlier memory trap", cheapLoad, enabled, err)
			}
		}
	}
}
