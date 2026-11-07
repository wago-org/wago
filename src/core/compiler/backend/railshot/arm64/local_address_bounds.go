//go:build arm64

package arm64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// prepareLocalAddressBounds proves invariants over every assignment, rather
// than carrying assignment-version facts across control edges. Declared locals
// start at zero; parameters remain unknown. Only immediately adjacent constant,
// constant-mask, and masked-left-shift producers are admitted. Unknown writes
// invalidate a local for the entire function, including reads before the write.
// The independent bytecode/local limits bound the extra compilation work.
func (f *fn) prepareLocalAddressBounds(body []byte) {
	if !f.opt(optMemoryMinimumBounds) || f.nLocals <= f.nParams || f.nLocals > 64 || len(body) > 4096 {
		return
	}
	memory, ok := f.memoryType(0)
	if !ok || memory.Limits.Addr64 || memory.Limits.Min == 0 {
		return
	}
	var bounds [64]uint32
	var invalid uint64
	var aligned8 uint64
	for i, typ := range f.localType {
		if i < f.nParams || typ != mtI32 {
			invalid |= uint64(1) << uint(i)
		} else {
			aligned8 |= uint64(1) << uint(i) // declared locals start at zero
		}
	}
	type producer struct {
		op       byte
		bound    uint32
		known    bool
		aligned8 bool
	}
	var previous, beforePrevious producer
	r := wasm.NewReader(body)
	var imm wasm.InstructionImmediate
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return
		}
		current := producer{op: op}
		if op == 0x41 {
			copy := *r
			value, err := copy.I32()
			if err != nil {
				return
			}
			current.bound, current.known = uint32(value), true
			current.aligned8 = uint32(value)&7 == 0
		}
		if err := f.classifier.ClassifyInto(r, op, &imm); err != nil {
			return
		}
		switch op {
		case 0x71: // i32.and: the immediately preceding constant is the RHS.
			if previous.op == 0x41 {
				current.bound, current.known = previous.bound, true
				current.aligned8 = previous.bound&7 == 0 || beforePrevious.aligned8
			}
		case 0x74: // i32.shl: do not infer a wrapped upper bound.
			if previous.op == 0x41 {
				current.aligned8 = previous.bound&31 >= 3 || beforePrevious.aligned8
			}
			if previous.op == 0x41 && beforePrevious.known {
				maximum := uint64(beforePrevious.bound) << (previous.bound & 31)
				if maximum <= uint64(^uint32(0)) {
					current.bound, current.known = uint32(maximum), true
				}
			}
		case 0x6c: // multiplying by a multiple of eight clears the low three bits
			current.aligned8 = previous.op == 0x41 && (previous.aligned8 || beforePrevious.aligned8)
		case 0x6a, 0x6b, 0x72, 0x73: // a known aligned producer and an aligned constant
			current.aligned8 = previous.op == 0x41 && previous.aligned8 && beforePrevious.aligned8
		case 0x21, 0x22: // local.set, local.tee
			if int(imm.Index) >= f.nLocals {
				return
			}
			bit := uint64(1) << imm.Index
			if !previous.aligned8 {
				aligned8 &^= bit
			}
			if !previous.known {
				invalid |= bit
			} else if previous.bound > bounds[imm.Index] {
				bounds[imm.Index] = previous.bound
			}
		}
		beforePrevious, previous = previous, current
	}
	f.localAddressAligned8 = aligned8
	useful := false
	for i := 0; i < f.nLocals; i++ {
		if invalid&(uint64(1)<<uint(i)) != 0 {
			bounds[i] = ^uint32(0)
		}
		if bounds[i] != ^uint32(0) {
			useful = true
		}
	}
	if useful {
		f.localAddressBounds = append([]uint32(nil), bounds[:f.nLocals]...)
	}
}

// A successful check of base+extent proves the rounded extent as well when the
// base is 8-aligned: memory32 byte lengths are multiples of the 64 KiB Wasm page.
// This strengthens only the certificate; the original access/check is unchanged.
func (f *fn) alignedAddressExtent(kind uint8, index uint32, extent int32) int32 {
	if kind != 1 || index >= 64 || extent <= 0 || extent > 0x7ffffff8 ||
		f.localAddressAligned8&(uint64(1)<<index) == 0 {
		return extent
	}
	return (extent + 7) &^ 7
}

func (f *fn) localAddressMaximum(e *elem) (uint64, bool) {
	if e == nil || e.elemKind() != ekValue || e.st.typ != mtI32 || (e.st.kind != stLocalRef && e.st.kind != stLocalReg) {
		return 0, false
	}
	if f.rootLoopRangeLocal != 0 && len(f.ctrl) > 1 && f.ctrl[1].kind == cfLoop && uint(e.st.idx)+1 == uint(f.rootLoopRangeLocal) {
		return uint64(f.rootLoopRangeMaximum), true
	}
	if uint(e.st.idx) >= uint(len(f.localAddressBounds)) {
		return 0, false
	}
	return uint64(f.localAddressBounds[e.st.idx]), true
}
