package wago

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// maxStagedEHTagParams bounds the payload words in the staged tag directory; it
// matches the backends' exception record payload capacity.
const maxStagedEHTagParams = 8

// isRefNullConstExpr reports whether body is exactly `ref.null <heaptype> end`.
func isRefNullConstExpr(body []byte) bool {
	r := wasm.NewReader(body)
	if op, err := r.Byte(); err != nil || op != 0xd0 {
		return false
	}
	if _, err := r.S33(); err != nil {
		return false
	}
	end, err := r.Byte()
	return err == nil && end == 0x0b && r.BytesLeft() == 0
}
