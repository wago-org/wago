//go:build amd64

package amd64

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/abi"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

// Only one nontrapping index producer may move before the cast. No call,
// control edge, store, or arbitrary arithmetic can enter the fused region.
func (f *fn) tryFuseFinalCastArrayGet(typeIndex uint32, nullable bool, r *wasm.Reader) (bool, error) {
	if f.inlineDepth != 0 || f.localBase != 0 || len(f.customInstructions) != 0 || !f.gcArrayHelpers {
		return false, nil
	}
	st, ok := f.stagedArraySubtype(typeIndex)
	if !ok || !st.Final {
		return false, nil
	}
	storage := st.Comp.Array.Storage()
	scalar, supported := directGCScalarStorage(storage)
	collectorRef := nativeGCCollectorRefStorage(f.m, typeIndex, storage)
	if collectorRef {
		scalar, supported = directGCScalar{size: 4, typ: mtI64}, true
	}
	if !storage.Packed() && storage.Val() == wasm.V128 {
		scalar, supported = directGCScalar{size: 16, typ: mtV128}, true
	}
	if !supported {
		return false, nil
	}
	start := r.Offset()
	matched := false
	defer func() {
		if !matched {
			_ = r.JumpTo(start)
		}
	}()
	op, err := r.Byte()
	if err != nil {
		return false, nil
	}
	switch op {
	case 0x20:
		x, e := r.U32()
		if e != nil {
			return false, e
		}
		if int(x) >= len(f.localType) || f.localType[x] != mtI32 {
			return false, nil
		}
	case 0x41:
		if _, e := r.I32(); e != nil {
			return false, e
		}
	default:
		return false, nil
	}
	next, e := r.Byte()
	if e != nil || next != 0xfb {
		return false, nil
	}
	sub, e := r.U32()
	if e != nil {
		return false, e
	}
	if sub < 11 || sub > 13 || (sub == 11) == storage.Packed() {
		return false, nil
	}
	accessType, e := r.U32()
	if e != nil {
		return false, e
	}
	if accessType != typeIndex {
		return false, nil
	}
	end := r.Offset()
	// Use the ordinary local/constant emitter, including lazy local recovery.
	// The object is still on the stack, so local.get cannot select a loop latch.
	_ = r.JumpTo(start + 1)
	if err = f.emitPlain(r, op); err != nil {
		return false, err
	}
	_ = r.JumpTo(end)
	matched = true
	f.stats.peep("final-cast-array-get-fuse")
	return true, f.emitNativeFinalCastArrayGet(typeIndex, nullable, scalar, collectorRef, sub == 12)
}

func (f *fn) emitNativeFinalCastArrayGet(typeIndex uint32, nullable bool, scalar directGCScalar, collectorRef, signed bool) error {
	switch scalar.size {
	case 1, 2, 4, 8, 16:
	default:
		return fmt.Errorf("amd64: fused array width %d is invalid", scalar.size)
	}
	f.flush()
	indexValue := f.popValue()
	object := f.popValue()
	ref := f.materialize(object)
	if ref != RAX {
		f.a.MovReg64(RAX, ref)
	}
	f.release(ref)
	f.pinned = f.pinned.add(RAX)
	index := f.materialize(indexValue)
	// i32 index semantics discard dirty upper bits from a host result/local.
	f.a.MovRegReg32(RCX, index)
	f.release(index)
	f.pinned = f.pinned.remove(RAX)
	f.a.MovImm32(RDX, int32(typeIndex))
	flag := int32(0)
	if nullable {
		flag = 1
	}
	f.a.MovImm32(RSI, flag)
	if regallocCheckEnabled {
		f.checkCallClobber()
	}
	site := f.a.CallRel32()
	widthIndex := 0
	for width := scalar.size; width > 1; width >>= 1 {
		widthIndex++
	}
	f.sc.gcCastArrayGetSites[widthIndex] = append(f.sc.gcCastArrayGetSites[widthIndex], site)
	f.stats.call("gcnative")
	// The returned element address is consumed immediately, before any safepoint.
	if scalar.typ == mtV128 {
		x := f.allocFReg(0)
		f.mov128LoadDisp(x, RAX, 0)
		f.pushVReg(x)
	} else if scalar.typ.isFloat() {
		x := f.allocFReg(0)
		f.a.FLoadDisp(x, RAX, 0, scalar.typ == mtF64)
		f.pushFReg(x, scalar.typ)
	} else {
		f.a.LoadIdx(RAX, RAX, RSP, 0, scalar.size, signed, scalar.typ == mtI64 && !collectorRef)
		result := f.pushReg(RAX, scalar.typ)
		if collectorRef {
			f.setStackGCRoot(result, true)
		}
	}
	return nil
}

// EAX=untrusted compact ref, EDX=final local type, ECX=i32 index,
// ESI=nullable cast. Returns the checked element address in RAX. This leaf
// cannot allocate, collect, reenter, or expose its address to Wasm.
func (f *fn) emitNativeFinalCastArrayGetStub(width int) {
	a := f.a
	var castFailures, nullFailures, boundsFailures []int
	castFailure := func(cond Cond) { castFailures = append(castFailures, a.JccPlaceholder(cond)) }
	var preserve [3]bool
	for _, local := range f.pinnedLocals {
		if reg := f.locals[local].reg; reg >= R9 && reg <= R11 {
			preserve[reg-R9] = true
		}
	}
	for _, global := range f.globalPinIndices[:f.nGlobalPins] {
		state := f.globalReg[global]
		reg := globalRegValue(state)
		if reg >= R9 && reg <= R11 {
			preserve[reg-R9] = true
		}
	}
	for i, yes := range preserve {
		if yes {
			a.Push(R9 + Reg(i))
		}
	}
	f.rejectForeignAnyref(castFailure)
	a.TestSelf(RAX, false)
	nonNull := a.JccPlaceholder(condNE)
	a.TestSelf(RSI, false)
	castFailure(condE)
	nullFailures = append(nullFailures, a.JmpPlaceholder())
	a.PatchRel32(nonNull, a.Len())
	a.MovRegReg32(RSI, RCX)
	a.MovRegReg32(R10, RAX)
	a.AluRI(4, R10, 1, false)
	a.TestSelf(R10, false)
	castFailure(condNE)

	a.Load64(R8, RBX, -int32(abi.GCNativeViewPtrOffset))
	a.Load64(R10, R8, gc.NativeInstanceViewLocalTypesOffset)
	a.ImulRI(RDX, 4, true)
	a.LoadIdx(RDX, R10, RDX, 0, 4, false, false)

	a.Load64(R8, R8, gc.NativeInstanceViewCollectorOffset)
	a.ShiftImm(5, RAX, 1, false)
	a.AluRM(cmpRMcode, RAX, R8, gc.NativeViewHandleCountOffset, false)
	castFailure(condAE)
	a.Load64(R9, R8, gc.NativeViewHandlesOffset)
	a.ShiftImm(4, RAX, 4, true)
	a.Add64(R9, RAX)

	a.LoadIdx(R10, R9, RSP, gc.NativeHandleSpaceOffset, 1, false, false)
	a.TestSelf(R10, false)
	castFailure(condE)
	a.AluRI(cmpDigit, R10, gc.NativeSpaceCount-1, false)
	castFailure(condA)
	a.ImulRI(R10, gc.NativeViewSpaceStride, true)
	a.LeaDisp(R8, R8, gc.NativeViewSpacesOffset)
	a.Add64(R8, R10)
	a.Load64(R11, R8, gc.NativeSpaceBaseOffset)
	a.Load32(R8, R8, gc.NativeSpaceBytesOffset)
	a.Load32(RAX, R9, gc.NativeHandleOffsetOffset)
	a.Cmp32(RAX, R8)
	castFailure(condA)
	a.Sub32(R8, RAX)
	a.Load32(RCX, R9, gc.NativeHandleSizeOffset)
	a.Cmp32(RCX, R8)
	castFailure(condA)
	a.AluRI(cmpDigit, RCX, int32(gc.PayloadOffset), false)
	castFailure(condB)

	a.Add64(R11, RAX)
	a.Load32(RCX, R11, 0)
	a.Cmp32(RCX, RDX)
	castFailure(condNE)
	a.Load32(R10, R11, 8)
	a.Cmp32(RSI, R10)
	boundsFailures = append(boundsFailures, a.JccPlaceholder(condAE))
	a.ImulRI(RSI, int32(width), true)
	a.Load32(R10, R11, 4)
	a.MovReg64(R8, RSI)
	a.AluRI(0, R8, int32(gc.PayloadOffset)+int32(width), true)
	a.Load32(RDX, R9, gc.NativeHandleSizeOffset)
	a.Cmp64(R8, RDX)
	castFailure(condA)
	a.Cmp64(R8, R10)
	castFailure(condA)
	a.LeaDisp(RAX, R11, int32(gc.PayloadOffset))
	a.Add64(RAX, RSI)
	for i := len(preserve) - 1; i >= 0; i-- {
		if preserve[i] {
			a.Pop(R9 + Reg(i))
		}
	}
	a.Ret()
	// Trap tails commit dirty globals; restore their pins before reaching them.
	for _, failure := range []struct {
		sites []int
		code  uint32
	}{{castFailures, trapCastFailure}, {nullFailures, trapNullReference}, {boundsFailures, trapBuiltin}} {
		for _, site := range failure.sites {
			a.PatchRel32(site, a.Len())
		}
		for i := len(preserve) - 1; i >= 0; i-- {
			if preserve[i] {
				a.Pop(R9 + Reg(i))
			}
		}
		f.trapAlwaysUnknownPC(failure.code)
	}
}
