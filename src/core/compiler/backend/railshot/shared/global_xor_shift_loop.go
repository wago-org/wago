package shared

import "github.com/wago-org/wago/src/core/compiler/wasm"

// GlobalXorShiftLoop adds low32(g XOR (g >> Shift)) after each g += Step.
// The do-while uses a zero-based unsigned i32 counter. Only one defined mutable
// i64 global is accessed; calls, memory, fences and escaping branches reject.
type GlobalXorShiftLoop struct {
	Counter, Accumulator, Bound, Global uint32
	Step                                uint64
	Shift                               uint8
	End                                 int
}

// InspectGlobalXorShiftLoop has a fixed grammar and leaves source unchanged.
// The backend independently proves counter initialization and native eligibility.
func InspectGlobalXorShiftLoop(source *wasm.Reader, m *wasm.Module) (GlobalXorShiftLoop, bool) {
	var p GlobalXorShiftLoop
	if m == nil || m.GlobalCount() == 0 {
		return p, false
	}
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
	var ok bool
	if !byteIs(0x40) {
		return p, false
	}
	if p.Accumulator, ok = index(0x20); !ok || !byteIs(0x02) || !byteIs(0x7f) {
		return p, false
	}
	if p.Global, ok = index(0x23); !ok || p.Global < uint32(m.ImportedGlobalCount()) {
		return p, false
	}
	gt, valid := m.GlobalTypeByIndex(p.Global)
	if !valid || !gt.Mutable || !wasm.EqualValType(wasm.GlobalValueType(gt), wasm.I64) || !byteIs(0x42) {
		return p, false
	}
	step, err := r.I64()
	if err != nil || !byteIs(0x7c) || !same(0x24, p.Global) || !same(0x23, p.Global) || !byteIs(0xa7) || !same(0x23, p.Global) || !byteIs(0x42) {
		return p, false
	}
	shift, err := r.I64()
	if err != nil || !byteIs(0x88) || !byteIs(0xa7) || !byteIs(0x73) || !byteIs(0x0b) || !byteIs(0x6a) || !same(0x21, p.Accumulator) {
		return p, false
	}
	if p.Counter, ok = index(0x20); !ok || !byteIs(0x41) {
		return p, false
	}
	one, err := r.I32()
	if err != nil || one != 1 || !byteIs(0x6a) || !same(0x21, p.Counter) || !same(0x20, p.Counter) {
		return p, false
	}
	if p.Bound, ok = index(0x20); !ok || !byteIs(0x49) || !same(0x0d, 0) || !byteIs(0x0b) || p.Counter == p.Accumulator || p.Counter == p.Bound || p.Accumulator == p.Bound {
		return p, false
	}
	p.Step, p.Shift, p.End = uint64(step), uint8(uint64(shift)&63), r.Offset()
	return p, true
}
