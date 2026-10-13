package shared

import "github.com/wago-org/wago/src/core/compiler/wasm"

// IndexMixStoreLoop stores consecutive mixed i32 indices, high word first in
// each pair. The mixer is ((seed+Add) XOR its unsigned FirstShift), multiplied,
// then XOR its unsigned LastShift and masked. Temporary retains the product.
// Bound is either a local index or an i32 constant. Emission must check a
// nonzero even modular distance and the complete, nonwrapping store span.
type IndexMixStoreLoop struct {
	Destination, Seed, Temporary, Bound uint32
	BoundConstant                       bool
	Add, Multiplier, Mask, Offset       uint32
	FirstShift, LastShift               uint8
	End                                 int
}

func InspectIndexMixStoreLoop(source *wasm.Reader, m *wasm.Module) (IndexMixStoreLoop, bool) {
	var p IndexMixStoreLoop
	if m == nil {
		return p, false
	}
	r := *source
	byteIs := func(want byte) bool { v, e := r.Byte(); return e == nil && v == want }
	index := func(op byte) (uint32, bool) {
		if !byteIs(op) {
			return 0, false
		}
		v, e := r.U32()
		return v, e == nil
	}
	same := func(op byte, want uint32) bool { v, ok := index(op); return ok && v == want }
	constant := func() (uint32, bool) {
		if !byteIs(0x41) {
			return 0, false
		}
		v, e := r.I32()
		return uint32(v), e == nil
	}
	constantIs := func(want uint32) bool { v, ok := constant(); return ok && v == want }
	if !byteIs(0x40) {
		return p, false
	}
	var ok bool
	if p.Destination, ok = index(0x20); !ok || !constantIs(4) || !byteIs(0x6a) {
		return p, false
	}
	if p.Seed, ok = index(0x20); !ok {
		return p, false
	}
	expression := func(first bool) bool {
		add, ok := constant()
		if !ok || !byteIs(0x6a) {
			return false
		}
		if first {
			p.Add = add - 1
		} else if add != p.Add {
			return false
		}
		tmp, ok := index(0x22)
		if !ok {
			return false
		}
		if first {
			p.Temporary = tmp
		}
		if tmp != p.Temporary {
			return false
		}
		shift, ok := constant()
		if !ok || !byteIs(0x76) || !same(0x20, tmp) || !byteIs(0x73) {
			return false
		}
		if first {
			p.FirstShift = uint8(shift & 31)
		}
		if uint8(shift&31) != p.FirstShift {
			return false
		}
		mul, ok := constant()
		if !ok || !byteIs(0x6c) || !same(0x22, tmp) {
			return false
		}
		if first {
			p.Multiplier = mul
		}
		if mul != p.Multiplier {
			return false
		}
		shift, ok = constant()
		if !ok || !byteIs(0x76) || !same(0x20, tmp) || !byteIs(0x73) {
			return false
		}
		if first {
			p.LastShift = uint8(shift & 31)
		}
		if uint8(shift&31) != p.LastShift {
			return false
		}
		mask, ok := constant()
		if !ok || !byteIs(0x71) {
			return false
		}
		if first {
			p.Mask = mask
		}
		return mask == p.Mask
	}
	store := func(first bool) bool {
		if !byteIs(0x36) {
			return false
		}
		align, e := r.U32()
		if e != nil || align > 2 {
			return false
		}
		off, e := r.U32()
		if e != nil {
			return false
		}
		if first {
			p.Offset = off
		}
		return off == p.Offset
	}
	if !expression(true) || !store(true) || !same(0x20, p.Destination) || !same(0x20, p.Seed) || !expression(false) || !store(false) {
		return p, false
	}
	if !same(0x20, p.Destination) || !constantIs(8) || !byteIs(0x6a) || !same(0x21, p.Destination) {
		return p, false
	}
	bound := func() bool {
		op, e := r.Byte()
		if e != nil {
			return false
		}
		switch op {
		case 0x20:
			p.Bound, e = r.U32()
			p.BoundConstant = false
		case 0x41:
			var v int32
			v, e = r.I32()
			p.Bound, p.BoundConstant = uint32(v), true
		default:
			return false
		}
		return e == nil
	}
	step := func() bool { return same(0x20, p.Seed) && constantIs(2) && byteIs(0x6a) && same(0x22, p.Seed) }
	trip := r
	if !(bound() && step() && byteIs(0x47)) {
		r = trip
		if !(step() && bound() && byteIs(0x47)) {
			return p, false
		}
	}
	if !same(0x0d, 0) || !byteIs(0x0b) {
		return p, false
	}
	if p.Destination == p.Seed || p.Temporary == p.Destination || p.Temporary == p.Seed ||
		!p.BoundConstant && (p.Bound == p.Destination || p.Bound == p.Seed || p.Bound == p.Temporary) {
		return p, false
	}
	mt, ok := m.MemoryType(0)
	if !ok || mt.Shared || mt.Limits.Addr64 {
		return p, false
	}
	p.End = r.Offset()
	return p, true
}
