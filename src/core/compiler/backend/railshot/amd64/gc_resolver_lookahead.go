//go:build amd64 && (!tinygo || !wago_lean)

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

// Choose the existing certificate only when a bounded scan finds its next
// usable read before another read of the new source. This is a replacement
// heuristic; all checks, root rules and effect invalidations still apply.
func (f *fn) retainGCResolvedAddress(reader *wasm.Reader, local int, hasLocal bool) bool {
	if f.inlineDepth != 0 || f.localBase != 0 || !gcResolveReuseEnabled || !hasLocal || !f.gcResolved.valid || f.gcResolved.local == local || len(f.customInstructions) != 0 {
		return false
	}
	r := *reader
	window, err := r.Bytes(min(64, r.BytesLeft()))
	if err != nil {
		return false
	}
	r = wasm.ReaderFrom(window)
	previous := -1
	for ops := 0; ops < 32; ops++ {
		op, err := r.Byte()
		if err != nil {
			return false
		}
		source := previous
		previous = -1
		switch op {
		case 0x20:
			var index uint32
			index, err = r.U32()
			previous = int(index)
		case 0x21, 0x22:
			var index uint32
			index, err = r.U32()
			if int(index) == local || int(index) == f.gcResolved.local {
				return false
			}
		case 0x41:
			_, err = r.I32()
		case 0x42:
			_, err = r.I64()
		case 0xfb:
			var sub, typ, field uint32
			sub, err = r.U32()
			if err != nil || source < 0 || (sub != 2 && sub != 3 && sub != 4) {
				return false
			}
			typ, err = r.U32()
			if err != nil {
				return false
			}
			field, err = r.U32()
			if err != nil {
				return false
			}
			if source == local {
				return false
			}
			if source == f.gcResolved.local {
				if typ != f.gcResolved.typeIndex {
					return false
				}
				layout, ok := f.gcTypeLayout(typ, wasm.CompStruct)
				if !ok || int(field) >= len(layout.FieldLayout) {
					return false
				}
				scalar, ok := directGCScalarStorage(layout.Type.Comp.Fields[field].Storage())
				return ok && uint64(gc.PayloadOffset)+uint64(layout.FieldLayout[field].Offset)+uint64(scalar.size) <= uint64(f.gcResolved.requiredBytes)
			}
		case 0x1a,
			0x45, 0x46, 0x47, 0x48, 0x49, 0x4a, 0x4b, 0x4c, 0x4d, 0x4e, 0x4f,
			0x50, 0x51, 0x52, 0x53, 0x54, 0x55, 0x56, 0x57, 0x58, 0x59, 0x5a,
			0x6a, 0x6b, 0x6c, 0x71, 0x72, 0x73, 0x74, 0x75, 0x76, 0x77, 0x78,
			0x7c, 0x7d, 0x7e, 0x83, 0x84, 0x85, 0x86, 0x87, 0x88, 0x89, 0x8a:
		default:
			return false
		}
		if err != nil {
			return false
		}
	}
	return false
}
