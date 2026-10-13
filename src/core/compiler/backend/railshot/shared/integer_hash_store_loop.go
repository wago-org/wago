package shared

import "github.com/wago-org/wago/src/core/compiler/wasm"

// IntegerHashStoreLoop maps consecutive i32 seeds to ordinary memory32 words.
// Each pair computes ((seed XOR Xor)*Multiplier XOR its logical right shift)
// AND Mask. The temporary contains the multiplied value, not the stored result.
// Runtime emission must guard finite iteration, address wrap and the full store
// extent, retaining the original scalar loop and partial writes on guard failure.
type IntegerHashStoreLoop struct {
	Destination, Seed, Temporary         uint32
	Xor, Multiplier, Mask, Bound, Offset uint32
	Shift                                uint8
	End                                  int
}

func InspectIntegerHashStoreLoop(source *wasm.Reader, m *wasm.Module) (IntegerHashStoreLoop, bool) {
	var p IntegerHashStoreLoop
	if m == nil {
		return p, false
	}
	r := *source
	byteIs := func(want byte) bool { got, err := r.Byte(); return err == nil && got == want }
	index := func(op byte) (uint32, bool) {
		if !byteIs(op) {
			return 0, false
		}
		x, err := r.U32()
		return x, err == nil
	}
	same := func(op byte, want uint32) bool { x, ok := index(op); return ok && x == want }
	constant := func() (uint32, bool) {
		if !byteIs(0x41) {
			return 0, false
		}
		x, err := r.I32()
		return uint32(x), err == nil
	}
	constantIs := func(want uint32) bool { x, ok := constant(); return ok && x == want }
	var ok bool
	if !byteIs(0x40) {
		return p, false
	}
	if p.Destination, ok = index(0x20); !ok {
		return p, false
	}
	if p.Seed, ok = index(0x20); !ok {
		return p, false
	}
	expression := func(first bool) bool {
		x, ok := constant()
		if !ok || !byteIs(0x73) {
			return false
		}
		if first {
			p.Xor = x
		}
		if x != p.Xor {
			return false
		}
		x, ok = constant()
		if !ok || !byteIs(0x6c) {
			return false
		}
		if first {
			p.Multiplier = x
		}
		if x != p.Multiplier {
			return false
		}
		temp, ok := index(0x22)
		if !ok {
			return false
		}
		if first {
			p.Temporary = temp
		}
		if temp != p.Temporary {
			return false
		}
		x, ok = constant()
		if !ok || !byteIs(0x76) || !same(0x20, p.Temporary) || !byteIs(0x73) {
			return false
		}
		if first {
			p.Shift = uint8(x & 31)
		}
		if uint8(x&31) != p.Shift {
			return false
		}
		x, ok = constant()
		if !ok || !byteIs(0x71) {
			return false
		}
		if first {
			p.Mask = x
		}
		return x == p.Mask
	}
	store := func(first bool) bool {
		if !byteIs(0x36) {
			return false
		}
		if first {
			mt, ok := m.MemoryType(0)
			if !ok || mt.Shared || mt.Limits.Addr64 {
				return false
			}
		}
		align, err := r.U32()
		if err != nil || align > 2 {
			return false
		}
		off, err := r.U32()
		if err != nil {
			return false
		}
		if first {
			p.Offset = off
		}
		return off == p.Offset
	}
	if !expression(true) || !store(true) || !same(0x20, p.Destination) || !constantIs(4) || !byteIs(0x6a) || !same(0x20, p.Seed) || !constantIs(1) || !byteIs(0x6a) || !expression(false) || !store(false) {
		return p, false
	}
	if !same(0x20, p.Destination) || !constantIs(8) || !byteIs(0x6a) || !same(0x21, p.Destination) || !same(0x20, p.Seed) || !constantIs(2) || !byteIs(0x6a) || !same(0x22, p.Seed) {
		return p, false
	}
	if p.Bound, ok = constant(); !ok || p.Bound < 2 || !byteIs(0x47) || !same(0x0d, 0) || !byteIs(0x0b) {
		return p, false
	}
	if p.Destination == p.Seed || p.Temporary == p.Destination || p.Temporary == p.Seed {
		return p, false
	}
	p.End = r.Offset()
	return p, true
}
