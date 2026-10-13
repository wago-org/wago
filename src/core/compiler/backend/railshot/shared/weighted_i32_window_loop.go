package shared

import "github.com/wago-org/wago/src/core/compiler/wasm"

// WeightedI32WindowLoop describes a bounded unrolled wrapping reduction over a
// contiguous i32 window. Coefficient locals are invariant; pointer/count/hash
// updates and signed division remain observable. Backends must guard the entire
// runtime window range before reordering loads and retain the scalar source.
type WeightedI32WindowLoop struct {
	Coefficients                        [32]uint32
	Terms                               int
	Pointer, NextPointer, Counter, Hash uint32
	Bias, Multiplier                    int32
	Divisor, Offset                     uint32
	End                                 int
}

func InspectWeightedI32WindowLoop(source *wasm.Reader, m *wasm.Module) (WeightedI32WindowLoop, bool) {
	var p WeightedI32WindowLoop
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
	integer := func() (int32, bool) {
		if !byteIs(0x41) {
			return 0, false
		}
		v, e := r.I32()
		return v, e == nil
	}
	constant := func(want int32) bool { v, ok := integer(); return ok && v == want }
	load := func(first bool) bool {
		if !byteIs(0x28) {
			return false
		}
		a, e := r.U32()
		if e != nil || a > 2 {
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
	var ok bool
	if !byteIs(0x40) {
		return p, false
	}
	if p.Coefficients[0], ok = index(0x20); !ok {
		return p, false
	}
	if p.Pointer, ok = index(0x20); !ok {
		return p, false
	}
	top, valid := integer()
	if !valid || top < 12 || top > 124 || top&3 != 0 {
		return p, false
	}
	p.Terms = int(top/4) + 1
	if p.Terms%4 != 0 || !byteIs(0x6a) || !load(true) {
		return p, false
	}
	if p.Bias, ok = integer(); !ok || !byteIs(0x6a) || !byteIs(0x6c) {
		return p, false
	}
	for i := 1; i < p.Terms; i++ {
		if p.Coefficients[i], ok = index(0x20); !ok || !same(0x20, p.Pointer) {
			return p, false
		}
		offset := top - int32(i*4)
		if offset != 0 {
			if !constant(offset) || !byteIs(0x6a) {
				return p, false
			}
			if offset == 4 {
				if p.NextPointer, ok = index(0x22); !ok {
					return p, false
				}
			}
		}
		if !load(false) || !constant(p.Bias) || !byteIs(0x6a) || !byteIs(0x6c) {
			return p, false
		}
	}
	for i := 1; i < p.Terms; i++ {
		if !byteIs(0x6a) {
			return p, false
		}
	}
	divisor, valid := integer()
	if !valid || divisor <= 0 || uint32(divisor)&(uint32(divisor)-1) != 0 || !byteIs(0x6d) {
		return p, false
	}
	p.Divisor = uint32(divisor)
	if p.Hash, ok = index(0x20); !ok || !byteIs(0x73) {
		return p, false
	}
	if p.Multiplier, ok = integer(); !ok || !byteIs(0x6c) || !same(0x21, p.Hash) || !same(0x20, p.NextPointer) || !same(0x21, p.Pointer) {
		return p, false
	}
	if p.Counter, ok = index(0x20); !ok || !constant(-1) || !byteIs(0x6a) || !same(0x22, p.Counter) || !same(0x0d, 0) || !byteIs(0x0b) {
		return p, false
	}
	outputs := [4]uint32{p.Pointer, p.NextPointer, p.Counter, p.Hash}
	for i, x := range outputs {
		for _, y := range outputs[:i] {
			if x == y {
				return p, false
			}
		}
		for _, c := range p.Coefficients[:p.Terms] {
			if c == x {
				return p, false
			}
		}
	}
	mt, ok := m.MemoryType(0)
	if !ok || mt.Shared || mt.Limits.Addr64 {
		return p, false
	}
	p.End = r.Offset()
	return p, true
}
