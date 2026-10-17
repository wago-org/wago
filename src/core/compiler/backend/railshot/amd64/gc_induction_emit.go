//go:build amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	x64 "github.com/wago-org/wago/src/core/encoder/amd64"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

func (f *fn) pushGCInductionCount() {
	if r, _, ok := f.pinReg(0); ok {
		f.recoverLocal(0)
		f.pushValue(storage{kind: stLocalReg, typ: mtI32, reg: r, idx: 0})
	} else {
		f.pushValue(storage{kind: stLocalRef, typ: mtI32, idx: 0})
	}
}

// Allocation is the sole remaining effect of these whole-function proofs.
// Do-while inputs of zero still charge one allocation. The helper retains the
// ordinary collector policies and can yield to Go for unbounded batch work.
func (f *fn) emitGCInductionReservation(helper uint32, descriptor uint64, descriptorType wasm.ValType) error {
	f.gcReducedBounded = f.opt(optPreparedBoundedEntry) && len(f.localType) <= 128
	f.sc.directPrepared = false
	f.pushGCInductionCount()
	count := f.materialize(f.popValue())
	f.a.MovRegReg32(count, count)
	one := f.allocReg(maskOf(count))
	f.a.MovImm32(one, 1)
	f.a.AluRI(7, count, 0, false)
	f.a.Cmovcc(x64.CondE, count, one, false)
	f.release(one)
	f.pushReg(count, mtI32)
	f.pushValue(storage{kind: stConst, typ: mtOf(descriptorType), cval: int64(descriptor)})
	return f.callGCStructHelper(helper, []wasm.ValType{wasm.I32, descriptorType}, nil)
}

func (f *fn) emitGCInductionSum(i31 bool) error {
	f.pushGCInductionCount()
	result := f.materialize(f.popValue())
	f.a.MovRegReg32(result, result)
	correction := Reg(0)
	if i31 {
		correction = f.allocReg(maskOf(result))
		f.a.MovRegReg32(correction, result)
		f.a.ShiftImm(4, correction, 31, false)
		f.a.AluRR(0x21, correction, result, false)
	}
	avoid := maskOf(result)
	if i31 {
		avoid |= maskOf(correction)
	}
	previous := f.allocReg(avoid)
	f.a.MovReg64(previous, result)
	f.a.AluRI(5, previous, 1, true)
	f.a.IMul(result, previous, true)
	f.release(previous)
	f.a.ShiftImm(5, result, 1, true)
	if i31 {
		f.a.AluRR(0x31, result, correction, false)
		f.release(correction)
	} else {
		f.a.MovRegReg32(result, result)
	}
	f.pushReg(result, mtI32)
	end := wasm.ReaderFrom([]byte{0x0b})
	return f.bodyLoop(&end, 0)
}

func (f *fn) emitGCI31Reduction() error {
	f.gcReducedBounded = f.opt(optPreparedBoundedEntry) && len(f.localType) <= 128
	f.gcI31Induction = f.gcReducedBounded && f.opt(optRegABI) && f.opt(optPreparedDirectEntry)
	f.stats.peep("gc-i31-induction-reduction")
	return f.emitGCInductionSum(true)
}
func (f *fn) emitGCStructReduction(typeIndex uint32) error {
	if err := f.emitGCInductionReservation(gcStructReserveDeadBatch, uint64(typeIndex), wasm.I32); err != nil {
		return err
	}
	if layout, ok := f.gcTypeLayout(typeIndex, wasm.CompStruct); f.gcReducedBounded && f.opt(optRegABI) && f.opt(optPreparedDirectEntry) && ok && layout.NativeAlloc && layout.PointerFree && layout.ObjectSize <= 4095 {
		f.gcDeadBatchLayout = uint64(layout.ObjectSize) | uint64(max(layout.Align, 8))<<32
	}
	f.stats.peep("gc-struct-induction-reduction")
	return f.emitGCInductionSum(false)
}
func (f *fn) emitGCArrayReduction(typeIndex, length uint32) error {
	if err := f.emitGCInductionReservation(gcArrayReserveDeadBatch, uint64(typeIndex)<<32|uint64(length), wasm.I64); err != nil {
		return err
	}
	if layout, ok := f.gcTypeLayout(typeIndex, wasm.CompArray); ok && gc.NativeDeadBatchSupported && layout.Type.Final && layout.PointerFree && layout.ElemLayout.DirectAccess && layout.ElemLayout.Size == 4 && f.gcReducedBounded && f.opt(optRegABI) && f.opt(optPreparedDirectEntry) {
		f.gcDeadBatchLayout = uint64(gc.Align8(gc.PayloadOffset+length*4)) | uint64(8)<<32
	}
	f.stats.peep("gc-array-induction-reduction")
	return f.emitGCInductionSum(false)
}
