package shared

import "github.com/wago-org/wago/src/core/compiler/wasm"

// ShiftedI32DotLoop is a pairwise wrapping reduction whose left pointer advances
// eight bytes and whose counter decrements two toward an invariant runtime limit.
// The right stream is offset by an invariant wrapping i32 shift. Backends must
// guard a finite suffix and the full load extent before replacing the source.
type ShiftedI32DotLoop struct {
	Pointer, Shift, Limit, Counter, Accumulator, RightAddress uint32
	LeftBias, RightBias                                       int32
	Offset                                                    uint32
	End                                                       int
}

func InspectShiftedI32DotLoop(source *wasm.Reader, m *wasm.Module) (ShiftedI32DotLoop, bool) {
	var p ShiftedI32DotLoop
	if m == nil {
		return p, false
	}
	r := *source
	byteIs := func(want byte) bool { got, e := r.Byte(); return e == nil && got == want }
	index := func(op byte) (uint32, bool) {
		if !byteIs(op) {
			return 0, false
		}
		x, e := r.U32()
		return x, e == nil
	}
	same := func(op byte, want uint32) bool { got, ok := index(op); return ok && got == want }
	constant := func(want int32) bool {
		if !byteIs(0x41) {
			return false
		}
		v, e := r.I32()
		return e == nil && v == want
	}
	load := func(first bool) bool {
		if !byteIs(0x28) {
			return false
		}
		a, e := r.U32()
		if e != nil || a > 2 {
			return false
		}
		o, e := r.U32()
		if e != nil {
			return false
		}
		if first {
			p.Offset = o
		}
		return o == p.Offset
	}
	bias := func() (int32, bool) {
		if !byteIs(0x41) {
			return 0, false
		}
		v, e := r.I32()
		return v, e == nil && byteIs(0x6a)
	}
	var ok bool
	if !byteIs(0x40) {
		return p, false
	}
	if p.Pointer, ok = index(0x20); !ok || !load(true) {
		return p, false
	}
	if p.LeftBias, ok = bias(); !ok || !same(0x20, p.Pointer) {
		return p, false
	}
	if p.Shift, ok = index(0x20); !ok || !byteIs(0x6a) {
		return p, false
	}
	if p.RightAddress, ok = index(0x22); !ok || !constant(4) || !byteIs(0x6a) || !load(false) {
		return p, false
	}
	if p.RightBias, ok = bias(); !ok || !byteIs(0x6c) || !same(0x20, p.Pointer) || !constant(-4) || !byteIs(0x6a) || !load(false) {
		return p, false
	}
	b, valid := bias()
	if !valid || b != p.LeftBias || !same(0x20, p.RightAddress) || !load(false) {
		return p, false
	}
	b, valid = bias()
	if !valid || b != p.RightBias || !byteIs(0x6c) {
		return p, false
	}
	if p.Accumulator, ok = index(0x20); !ok || !byteIs(0x6a) || !byteIs(0x6a) || !same(0x21, p.Accumulator) || !same(0x20, p.Pointer) || !constant(8) || !byteIs(0x6a) || !same(0x21, p.Pointer) {
		return p, false
	}
	if p.Limit, ok = index(0x20); !ok {
		return p, false
	}
	if p.Counter, ok = index(0x20); !ok || !constant(-2) || !byteIs(0x6a) || !same(0x22, p.Counter) || !byteIs(0x47) || !same(0x0d, 0) || !byteIs(0x0b) {
		return p, false
	}
	outputs := [4]uint32{p.Pointer, p.Counter, p.Accumulator, p.RightAddress}
	for i, x := range outputs {
		if x == p.Shift || x == p.Limit {
			return p, false
		}
		for _, y := range outputs[:i] {
			if x == y {
				return p, false
			}
		}
	}
	// Only syntactic candidates pay the memory lookup, including imported memory.
	mt, ok := m.MemoryType(0)
	if !ok || mt.Shared || mt.Limits.Addr64 {
		return p, false
	}
	p.End = r.Offset()
	return p, true
}
