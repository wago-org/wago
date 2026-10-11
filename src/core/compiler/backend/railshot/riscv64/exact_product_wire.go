//go:build linux && !tinygo

package riscv64

import (
	"bytes"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// admitExactProductWireShape retains representation details that public
// decoding discards. Exact product classifiers independently prove decoded
// semantics; this gate only excludes custom/inert sections, explicit recursive
// wrappers, and sources outside the deliberately small checkpoint budget.
func admitExactProductWireShape(data []byte, wantIDs []byte) bool {
	if len(data) < 8 || len(data) > 512 || !bytes.Equal(data[:8], []byte{0, 'a', 's', 'm', 1, 0, 0, 0}) {
		return false
	}
	r := wasm.ReaderFrom(data[8:])
	sectionIndex := 0
	for r.HasNext() {
		id, err := r.Byte()
		if err != nil || id == 0 || wantIDs != nil && (sectionIndex >= len(wantIDs) || id != wantIDs[sectionIndex]) {
			return false
		}
		n, err := r.U32()
		if err != nil || uint64(n) > uint64(r.BytesLeft()) {
			return false
		}
		start := r.Offset()
		end := start + int(n)
		section := wasm.ReaderFrom(data[8+start : 8+end])
		if id != 8 {
			count, err := section.U32()
			if err != nil || count == 0 {
				return false
			}
		}
		if id == 1 {
			for _, b := range data[8+start+section.Offset() : 8+end] {
				if b == 0x4e { // explicit rec group, flattened by public decoding
					return false
				}
			}
		}
		if err = r.JumpTo(end); err != nil {
			return false
		}
		sectionIndex++
	}
	return sectionIndex != 0 && (wantIDs == nil || sectionIndex == len(wantIDs))
}
