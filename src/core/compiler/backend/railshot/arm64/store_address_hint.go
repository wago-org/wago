//go:build arm64

package arm64

import (
	"os"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

var storeAddressHintEnabled = os.Getenv("WAGO_ARM64_NO_STORE_ADDRESS_HINT") != "1"

// storeAddressAhead asks whether the local.get immediately preceding r is the
// address beneath one bounded, straight-line scalar value expression. This is
// a cost hint only; the ordinary compiler still decodes and emits every op.
func storeAddressAhead(r wasm.Reader) bool {
	start, depth := r.Offset(), 0
	for n := 0; n < 32 && r.Offset()-start < 256; n++ {
		op, err := r.Byte()
		if err != nil {
			return false
		}
		switch {
		case op >= 0x36 && op <= 0x3e:
			return depth == 1
		case op == 0x20 || op == 0x23 || op >= 0x41 && op <= 0x44:
			if wasm.SkipInstructionImmediate(&r, op) != nil {
				return false
			}
			depth++
		case op == 0x22:
			if depth < 1 || wasm.SkipInstructionImmediate(&r, op) != nil {
				return false
			}
		case op == 0x45 || op == 0x50 || op >= 0x67 && op <= 0x69 || op >= 0x79 && op <= 0x7b:
			if depth < 1 {
				return false
			}
		case op >= 0x46 && op <= 0x4f || op >= 0x51 && op <= 0x5a || op >= 0x6a && op <= 0x78 || op >= 0x7c && op <= 0x8a:
			if depth < 2 {
				return false
			}
			depth--
		default:
			return false
		}
	}
	return false
}
