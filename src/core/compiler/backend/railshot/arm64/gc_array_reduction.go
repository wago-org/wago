//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

// gcArrayReduction proves the complete unsigned induction sum of immediately
// consumed uniform arrays. The masked index is in bounds, identities cannot
// escape, and allocation is the only remaining effect.
func (f *fn) gcArrayReduction(code []byte) (uint32, uint32, bool) {
	if !f.gcArrayHelpers || len(f.customInstructions) != 0 || f.interruptible || f.localBase != 0 || f.nParams != 1 || len(f.ft.Results) != 1 || f.ft.Results[0] != wasm.I32 || f.ft.Params[0] != wasm.I32 {
		return 0, 0, false
	}
	if profileEnabled && f.stats != nil && f.stats.RecordSources {
		return 0, 0, false
	}
	for _, typ := range f.localType {
		if typ != mtI32 {
			return 0, 0, false
		}
	}
	r := wasm.ReaderFrom(code)
	b := func(want byte) bool { v, err := r.Byte(); return err == nil && v == want }
	u := func(want uint32) bool { v, err := r.U32(); return err == nil && v == want }
	local := func() (uint32, bool) {
		if !b(0x20) {
			return 0, false
		}
		v, err := r.U32()
		return v, err == nil && int(v) < len(f.localType)
	}
	if !b(0x03) || !b(0x40) {
		return 0, 0, false
	}
	acc, ok := local()
	if !ok || acc == 0 {
		return 0, 0, false
	}
	counter, ok := local()
	if !ok || counter == 0 || counter == acc || !b(0x41) {
		return 0, 0, false
	}
	length, err := r.I32()
	if err != nil || length <= 0 || length > 64 || length&(length-1) != 0 || !b(0xfb) || !u(6) {
		return 0, 0, false
	}
	source, err := r.U32()
	if err != nil {
		return 0, 0, false
	}
	typ, ok := f.stagedArraySubtype(source)
	if !ok || !typ.Final {
		return 0, 0, false
	}
	field := typ.Comp.Array
	if field.Storage().Packed() || field.Storage().Val() != wasm.I32 {
		return 0, 0, false
	}
	index, ok := local()
	if !ok || index != counter || !b(0x41) {
		return 0, 0, false
	}
	mask, err := r.I32()
	if err != nil || mask != length-1 || !b(0x71) || !b(0xfb) || !u(11) || !u(source) {
		return 0, 0, false
	}
	if !b(0x6a) || !b(0x21) || !u(acc) {
		return 0, 0, false
	}
	x, ok := local()
	if !ok || x != counter || !b(0x41) {
		return 0, 0, false
	}
	step, err := r.I32()
	if err != nil || step != 1 || !b(0x6a) || !b(0x21) || !u(counter) {
		return 0, 0, false
	}
	x, ok = local()
	if !ok || x != counter {
		return 0, 0, false
	}
	x, ok = local()
	if !ok || x != 0 || !b(0x49) || !b(0x0d) || !u(0) || !b(0x0b) {
		return 0, 0, false
	}
	x, ok = local()
	if !ok || x != acc || !b(0x0b) {
		return 0, 0, false
	}
	if _, more := r.Peek(); more {
		return 0, 0, false
	}
	return source, uint32(length), true
}

func (f *fn) emitGCArrayReduction(typeIndex, length uint32) error {
	f.gcReducedBounded = f.opt(optPreparedBoundedEntry) && len(f.localType) <= 128
	f.sc.directPrepared, f.sc.directPreparedLight = false, false
	if reg, _, ok := f.pinReg(0); ok {
		f.recoverLocal(0)
		f.pushValue(storage{kind: stLocalReg, typ: mtI32, reg: reg, idx: 0})
	} else {
		f.pushValue(storage{kind: stLocalRef, typ: mtI32, idx: 0})
	}
	count := f.materialize(f.popValue())
	f.cmpImm(count, 0, false)
	f.a.Csinc32(count, count, ZR, condNE)
	f.pushReg(count, mtI32)
	// Two raw slots: count and an exact pair of u32 type index / array length.
	f.pushValue(storage{kind: stConst, typ: mtI64, cval: int64(uint64(typeIndex)<<32 | uint64(length))})
	if layout, ok := f.gcTypeLayout(typeIndex, wasm.CompArray); ok && gc.NativeDeadBatchSupported && layout.Type.Final && layout.PointerFree && layout.ElemLayout.DirectAccess && layout.ElemLayout.Size == 4 {
		f.nativeDeadArrayBatchBytes = gc.Align8(gc.PayloadOffset + length*4)
		if f.gcReducedBounded && f.opt(optRegABI) && f.opt(optPreparedDirectEntry) {
			f.gcDeadBatchLayout = uint64(f.nativeDeadArrayBatchBytes) | uint64(8)<<32
		}
	}
	if err := f.callGCStructHelper(gcArrayReserveDeadBatch, []wasm.ValType{wasm.I32, wasm.I64}, nil); err != nil {
		return err
	}
	if reg, _, ok := f.pinReg(0); ok {
		f.recoverLocal(0)
		f.pushValue(storage{kind: stLocalReg, typ: mtI32, reg: reg, idx: 0})
	} else {
		f.pushValue(storage{kind: stLocalRef, typ: mtI32, idx: 0})
	}
	result := f.materialize(f.popValue())
	f.a.MovReg32(result, result)
	previous := f.allocReg(maskOf(result))
	f.a.SubImm64(previous, result, 1)
	f.a.Mul64(result, result, previous)
	f.release(previous)
	f.a.LsrImm(result, result, 1, false)
	f.a.MovReg32(result, result)
	f.pushReg(result, mtI32)
	f.stats.peep("gc-array-induction-reduction")
	end := wasm.ReaderFrom([]byte{0x0b})
	return f.bodyLoop(&end, 0)
}
