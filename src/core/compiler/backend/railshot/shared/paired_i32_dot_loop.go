package shared

import "github.com/wago-org/wago/src/core/compiler/wasm"

// PairedI32DotLoop describes a wrapping i32 dot reduction over two adjacent
// four-byte elements per iteration. Each input has an arbitrary wrapping bias.
// The original counter advances eight bytes until a positive constant bound.
// Backends must guard finite iteration, address wrap and the entire load extent,
// and retain the original scalar loop when those runtime guards fail.
type PairedI32DotLoop struct {
	Left, Right, LeftAddress, RightAddress uint32
	Counter, Accumulator                   uint32
	LeftBias, RightBias                    int32
	Bound, Offset                          uint32
	End                                    int
}

// InspectPairedI32DotLoop is a bounded source proof, independent of workload
// identity. It accepts only ordinary memory32 loads and wrapping arithmetic;
// calls, stores, shared memory and escaping control cannot enter the cover.
func InspectPairedI32DotLoop(source *wasm.Reader, m *wasm.Module) (PairedI32DotLoop, bool) {
	var p PairedI32DotLoop
	if m == nil {
		return p, false
	}
	var ok bool
	r := *source
	byteIs := func(want byte) bool { got, err := r.Byte(); return err == nil && got == want }
	index := func(op byte) (uint32, bool) {
		if !byteIs(op) {
			return 0, false
		}
		v, err := r.U32()
		return v, err == nil
	}
	same := func(op byte, want uint32) bool { got, ok := index(op); return ok && got == want }
	constant := func(want int32) bool {
		if !byteIs(0x41) {
			return false
		}
		v, err := r.I32()
		return err == nil && v == want
	}
	load := func(first bool) bool {
		if !byteIs(0x28) {
			return false
		}
		// Most loops reject the syntax before reaching a load. Only candidates
		// pay the module memory lookup, which may inspect imported memories.
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
	bias := func() (int32, bool) {
		if !byteIs(0x41) {
			return 0, false
		}
		v, err := r.I32()
		return v, err == nil && byteIs(0x6a)
	}
	if !byteIs(0x40) {
		return p, false
	}
	if p.Left, ok = index(0x20); !ok {
		return p, false
	}
	if p.Counter, ok = index(0x20); !ok || !byteIs(0x6a) {
		return p, false
	}
	if p.LeftAddress, ok = index(0x22); !ok || !constant(4) || !byteIs(0x6a) || !load(true) {
		return p, false
	}
	if p.LeftBias, ok = bias(); !ok {
		return p, false
	}
	if p.Right, ok = index(0x20); !ok || !same(0x20, p.Counter) || !byteIs(0x6a) {
		return p, false
	}
	if p.RightAddress, ok = index(0x22); !ok || !constant(4) || !byteIs(0x6a) || !load(false) {
		return p, false
	}
	if p.RightBias, ok = bias(); !ok || !byteIs(0x6c) || !same(0x20, p.LeftAddress) || !load(false) {
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
	if p.Accumulator, ok = index(0x20); !ok || !byteIs(0x6a) || !byteIs(0x6a) || !same(0x21, p.Accumulator) || !same(0x20, p.Counter) || !constant(8) || !byteIs(0x6a) || !same(0x22, p.Counter) || !byteIs(0x41) {
		return p, false
	}
	bound, err := r.I32()
	if err != nil || bound <= 0 || bound&7 != 0 || !byteIs(0x47) || !same(0x0d, 0) || !byteIs(0x0b) {
		return p, false
	}
	// Bases may alias each other, but writes must never modify an input or
	// another output. The two address temporaries remain observable after exit.
	outputs := [4]uint32{p.Counter, p.Accumulator, p.LeftAddress, p.RightAddress}
	for i, x := range outputs {
		if x == p.Left || x == p.Right {
			return p, false
		}
		for _, y := range outputs[:i] {
			if x == y {
				return p, false
			}
		}
	}
	p.Bound, p.End = uint32(bound), r.Offset()
	return p, true
}
