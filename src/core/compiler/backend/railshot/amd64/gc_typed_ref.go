// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

import (
	"math"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	x64 "github.com/wago-org/wago/src/core/encoder/amd64"
	"github.com/wago-org/wago/src/core/runtime/abi"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

// Caller save (8), return address (8), flags (8), and four scratch saves (32).
const gcTypedMintStackBytes = 56

// The struct allocation path has just published this handle and initialized its
// header. Use that exact type without resolving the same object a second time.
// R8 is the collector, RSI is the handle index, and RAX is its compact ref.
// RCX, RDX and R10 are scratch at this publication point.
func emitFreshNativeStructTypeWord(a *x64.Asm, typeIndex, objectSize uint32) {
	if !gcDense16CastsEnabled {
		return
	}
	var raw [8]int
	n := 0
	fallback := func(cond Cond) {
		raw[n] = a.JccPlaceholder(cond)
		n++
	}
	a.AluRM(cmpRMcode, RSI, R8, gc.NativeViewHandleCountOffset, false)
	fallback(condAE)
	// RCX still holds the nursery offset used to publish the new handle.
	a.AluRI(0, RCX, int32(objectSize), false)
	a.AluRM(cmpRMcode, RCX, R8, int32(gc.NativeViewSpacesOffset+gc.NativeSpaceNursery*gc.NativeViewSpaceStride+gc.NativeSpaceBytesOffset), false)
	fallback(condA)
	a.Load32(RDX, R8, gc.NativeViewTypeRefEpochOffset)
	a.TestSelf(RDX, false)
	fallback(condE)
	a.AluRI(cmpDigit, RDX, int32(gc.NativeTypedRefMaxEpoch), false)
	fallback(condA)
	a.Load64(R10, RBX, -int32(abi.GCNativeViewPtrOffset))
	a.Load64(R10, R10, gc.NativeInstanceViewLocalTypesOffset)
	a.Load32(RCX, R10, int32(typeIndex*4))
	a.AluRI(cmpDigit, RCX, int32(gc.NativeTypeWordIDMask), false)
	fallback(condA)
	a.AluRM(cmpRMcode, RCX, R8, gc.NativeViewTypeWordCountOffset, false)
	fallback(condAE)
	a.Load64(R10, R8, gc.NativeViewTypeWordsOffset)
	a.TestSelf(R10, true)
	fallback(condE)
	a.LeaScaled(R10, R10, RCX, 2, 0)
	a.Load32(R10, R10, 0)
	a.TestImm(R10, gc.NativeTypeWordFastValid, false)
	fallback(condE)
	a.ShiftImm(4, RCX, 32, true)
	a.AluRR(0x09, RAX, RCX, true)
	a.ShiftImm(4, RDX, 1, false)
	a.AluRI(1, RDX, 1, false)
	a.ShiftImm(4, RDX, 48, true)
	a.AluRR(0x09, RAX, RDX, true)
	for _, site := range raw[:n] {
		a.PatchRel32(site, a.Len())
	}
}

func (f *fn) emitNativeMintReference(ref Reg) {
	if !gcDense16CastsEnabled {
		return
	}
	f.gcTypedMintCalls = true
	if ref != RAX {
		f.a.Push(RAX)
		f.a.MovReg64(RAX, ref)
	}
	site := f.a.CallPreservingRel32()
	f.relocs = append(f.relocs, f.newGCStubCallReloc(site, gcSharedStubMintTypedRef))
	if ref != RAX {
		f.a.MovReg64(ref, RAX)
		f.a.Pop(RAX)
	}
	f.stats.call("gcnative-type-mint")
}

// This read-only leaf cannot allocate, collect or trap. Bad shapes preserve
// reference identity, so eager minting cannot move a checked access's trap.
// Four GP saves and PUSHFQ preserve every caller value and pending condition.
func emitModuleGCMintTypedRef(a *x64.Asm) {
	a.EmitBytes([]byte{0x9c}) // PUSHFQ
	for _, reg := range [...]Reg{R8, R9, RCX, RDX} {
		a.Push(reg)
	}
	var unchanged []int
	skipIf := func(cond Cond) { unchanged = append(unchanged, a.JccPlaceholder(cond)) }
	a.MovReg64(RCX, RAX)
	a.ShiftImm(5, RCX, 32, true)
	a.TestSelf(RCX, true)
	raw := a.JccPlaceholder(condE)
	a.TestImm(RCX, 1<<16, false)
	skipIf(condE) // opaque words do not carry the reserved known marker
	a.TestSelf(RAX, false)
	skipIf(condE)
	a.TestImm(RAX, 1, false)
	skipIf(condNE)
	a.ShiftImm(5, RCX, 17, false)
	a.Load64(R8, RBX, -int32(abi.GCNativeViewPtrOffset))
	a.Load64(R8, R8, gc.NativeInstanceViewCollectorOffset)
	a.AluRM(cmpRMcode, RCX, R8, gc.NativeViewTypeRefEpochOffset, false)
	skipIf(condE) // a current carrier already has the required evidence
	a.MovRegReg32(RAX, RAX)
	a.PatchRel32(raw, a.Len())
	a.TestSelf(RAX, false)
	skipIf(condE)
	a.TestImm(RAX, 1, false)
	skipIf(condNE)
	a.Load64(R8, RBX, -int32(abi.GCNativeViewPtrOffset))
	a.Load64(R8, R8, gc.NativeInstanceViewCollectorOffset)
	a.CmpImm32Mem(R8, gc.NativeViewTypeRefEpochOffset, 0)
	skipIf(condE) // disabled epochs are never enabled again
	a.MovRegReg32(RCX, RAX)
	a.ShiftImm(5, RCX, 1, false)
	a.AluRM(cmpRMcode, RCX, R8, gc.NativeViewHandleCountOffset, false)
	skipIf(condAE)
	a.Load64(R9, R8, gc.NativeViewHandlesOffset)
	a.ShiftImm(4, RCX, 4, true)
	a.Add64(R9, RCX)
	a.LoadIdx(RDX, R9, RSP, gc.NativeHandleSpaceOffset, 1, false, false)
	a.TestSelf(RDX, false)
	skipIf(condE)
	a.AluRI(cmpDigit, RDX, gc.NativeSpaceCount-1, false)
	skipIf(condA)
	a.ShiftImm(4, RDX, 4, false)
	a.Add64(RDX, R8)
	a.Load64(R8, RDX, gc.NativeViewSpacesOffset+gc.NativeSpaceBaseOffset)
	a.TestSelf(R8, true)
	skipIf(condE)
	a.Load32(RDX, RDX, gc.NativeViewSpacesOffset+gc.NativeSpaceBytesOffset)
	a.Load32(RCX, R9, gc.NativeHandleOffsetOffset)
	a.Cmp32(RCX, RDX)
	skipIf(condA)
	a.Sub32(RDX, RCX)
	a.Load32(R9, R9, gc.NativeHandleSizeOffset)
	a.Cmp32(R9, RDX)
	skipIf(condA)
	a.AluRI(cmpDigit, R9, int32(gc.HeaderSize), false)
	skipIf(condB)
	a.Add64(R8, RCX)
	a.Load32(RCX, R8, 0)
	a.AluRI(cmpDigit, RCX, 0xffff, false)
	skipIf(condA)
	a.Load64(R8, RBX, -int32(abi.GCNativeViewPtrOffset))
	a.Load64(R8, R8, gc.NativeInstanceViewCollectorOffset)
	a.AluRM(cmpRMcode, RCX, R8, gc.NativeViewTypeWordCountOffset, false)
	skipIf(condAE)
	a.Load64(R9, R8, gc.NativeViewTypeWordsOffset)
	a.TestSelf(R9, true)
	skipIf(condE)
	a.LeaScaled(R9, R9, RCX, 2, 0)
	a.Load32(R9, R9, 0)
	a.TestImm(R9, gc.NativeTypeWordFastValid, false)
	skipIf(condE)
	a.Load32(RDX, R8, gc.NativeViewTypeRefEpochOffset)
	a.TestSelf(RDX, false)
	skipIf(condE)
	a.AluRI(cmpDigit, RDX, int32(gc.NativeTypedRefMaxEpoch), false)
	skipIf(condA)
	a.ShiftImm(4, RCX, 32, true)
	a.AluRR(0x09, RAX, RCX, true)
	a.ShiftImm(4, RDX, 1, false)
	a.AluRI(1, RDX, 1, false)
	a.ShiftImm(4, RDX, 48, true)
	a.AluRR(0x09, RAX, RDX, true)
	for _, site := range unchanged {
		a.PatchRel32(site, a.Len())
	}
	for _, reg := range [...]Reg{RDX, RCX, R9, R8} {
		a.Pop(reg)
	}
	a.EmitBytes([]byte{0x9d}) // POPFQ
	a.Ret()
}

func emitGCNormalizeNativeRef(a *x64.Asm, ref, scratch Reg) {
	if !gcDense16CastsEnabled {
		return
	}
	a.MovReg64(scratch, ref)
	a.ShiftImm(5, scratch, 48, true)
	a.TestImm(scratch, 1, false)
	unknown := a.JccPlaceholder(condE)
	a.TestSelf(ref, false)
	null := a.JccPlaceholder(condE)
	a.TestImm(ref, 1, false)
	i31 := a.JccPlaceholder(condNE)
	a.MovRegReg32(ref, ref)
	for _, site := range [...]int{unknown, null, i31} {
		a.PatchRel32(site, a.Len())
	}
}

// emitNativeTypedCast consumes RAX's internal object carrier. Known values
// belong to the active collector and remain rooted by the native frame plan.
// A current epoch permits pure type checks without physical object access.
// Unknown values retain the existing checked leaf. Stale evidence is removed
// before that leaf rechecks the current handle and object header.
// Only the existing cast scratch registers R8, RCX, RDX and RDI are changed.
// The returned success branch must be patched past the checked cold path.
type gcTypedCheckDone struct{ success, mismatch int }

func (d gcTypedCheckDone) patch(a *x64.Asm) {
	a.PatchRel32(d.success, a.Len())
	if d.mismatch >= 0 {
		a.PatchRel32(d.mismatch, a.Len())
	}
}

func (f *fn) emitNativeTypedCast(typeIndex uint32, exact, test bool) gcTypedCheckDone {
	return f.emitNativeTypedCheck(typeIndex, exact, test, 0)
}

func (f *fn) emitNativeTypedCheck(typeIndex uint32, exact, test bool, abstractMask uint32) gcTypedCheckDone {
	a := f.a
	a.MovReg64(RCX, RAX)
	a.ShiftImm(5, RCX, 48, true)
	a.TestImm(RCX, 1, false)
	unknown := a.JccPlaceholder(condE)

	failIf := func(cond Cond) {
		f.sc.gcTrustedCastFailSites = append(f.sc.gcTrustedCastFailSites, a.JccPlaceholder(cond))
	}
	var mismatches [3]int
	nMismatch := 0
	mismatchIf := func(cond Cond) {
		if !test {
			failIf(cond)
			return
		}
		mismatches[nMismatch] = a.JccPlaceholder(cond)
		nMismatch++
	}
	a.TestSelf(RAX, false)
	failIf(condE)
	a.TestImm(RAX, 1, false)
	failIf(condNE)
	a.ShiftImm(5, RCX, 1, false) // the fifteen-bit carrier epoch
	a.Load64(R8, RBX, -int32(abi.GCNativeViewPtrOffset))
	a.Load64(R8, R8, gc.NativeInstanceViewCollectorOffset)
	a.AluRM(cmpRMcode, RCX, R8, gc.NativeViewTypeRefEpochOffset, false)
	stale := a.JccPlaceholder(condNE)
	a.TestSelf(RCX, false)
	disabled := a.JccPlaceholder(condE)

	a.MovReg64(RDI, RAX)
	a.ShiftImm(5, RDI, 32, true)
	a.AluRI(4, RDI, 0xffff, false)
	badIndex, unsupported := -1, -1

	equal := -1
	if abstractMask != 0 {
		a.AluRM(cmpRMcode, RDI, R8, gc.NativeViewTypeWordCountOffset, false)
		badIndex = a.JccPlaceholder(condAE)
		a.Load64(RDX, R8, gc.NativeViewTypeWordsOffset)
		a.LeaScaled(RCX, RDX, RDI, 2, 0)
		a.Load32(RCX, RCX, 0)
		a.TestImm(RCX, gc.NativeTypeWordFastValid, false)
		unsupported = a.JccPlaceholder(condE)

		a.TestImm(RCX, abstractMask, false)
		mismatchIf(condE)
	} else {
		// A current carrier was minted only from an admitted descriptor. Its dense
		// identity is immutable within this epoch, so equality needs no row load.
		a.Load64(RDX, RBX, -int32(abi.GCNativeViewPtrOffset))
		a.Load64(RDX, RDX, gc.NativeInstanceViewLocalTypesOffset)
		if uint64(typeIndex)*4 <= math.MaxInt32 {
			a.Load32(RDX, RDX, int32(typeIndex*4))
		} else {
			a.MovImm64(RCX, uint64(typeIndex)*4)
			a.LoadIdx(RDX, RDX, RCX, 0, 4, false, false)
		}
		a.Cmp32(RDI, RDX)
		if exact {
			mismatchIf(condNE)
		} else {
			equal = a.JccPlaceholder(condE)
			a.AluRM(cmpRMcode, RDI, R8, gc.NativeViewSubtypeIntervalCountOffset, false)
			failIf(condAE)
			a.AluRM(cmpRMcode, RDX, R8, gc.NativeViewSubtypeIntervalCountOffset, false)
			failIf(condAE)
			a.Load64(R8, R8, gc.NativeViewSubtypeIntervalsOffset)
			a.LeaScaled(RCX, R8, RDI, 3, 0)
			a.Load64(RCX, RCX, 0)
			a.LeaScaled(RDX, R8, RDX, 3, 0)
			a.Load64(RDX, RDX, 0)
			a.MovReg64(RDI, RCX)
			a.ShiftImm(5, RDI, 32, true)
			a.MovReg64(R8, RDX)
			a.ShiftImm(5, R8, 32, true)
			a.Cmp32(RDI, R8)
			mismatchIf(condB)
			a.Cmp32(RCX, RDX)
			mismatchIf(condA)
		}
	}
	if equal >= 0 {
		a.PatchRel32(equal, a.Len())
	}
	if test {
		a.MovImm32(RAX, 1)
	}
	a.XorSelf32(RDX)
	done := gcTypedCheckDone{success: a.JmpPlaceholder(), mismatch: -1}
	if nMismatch != 0 {
		for _, site := range mismatches[:nMismatch] {
			a.PatchRel32(site, a.Len())
		}
		a.XorSelf32(RAX)
		done.mismatch = a.JmpPlaceholder()
	}
	for _, site := range [...]int{stale, disabled, badIndex, unsupported} {
		if site >= 0 {
			a.PatchRel32(site, a.Len())
		}
	}
	a.MovRegReg32(RAX, RAX)
	a.PatchRel32(unknown, a.Len())
	return done
}

func nativeAbstractTypeMask(heap int64) uint32 {
	switch heap {
	case -18:
		return gc.NativeTypeWordAny
	case -19:
		return gc.NativeTypeWordEq
	case -21:
		return gc.NativeTypeWordStruct
	case -22:
		return gc.NativeTypeWordArray
	default:
		return 0
	}
}

func (f *fn) emitNativeAbstractCheck(heap int64, nullable, test bool) error {
	object := f.popValue()
	f.flush()
	var inlineLocals [16]locState
	savedLocals := inlineLocals[:]
	if len(f.pinnedLocals) > len(inlineLocals) {
		pooledLocals := f.newLocStateBuf()
		savedLocals = pooledLocals
		defer f.freeLocStateBuf(pooledLocals)
	}
	for i, local := range f.pinnedLocals {
		savedLocals[i] = f.locals[local].state
	}
	ref := f.materialize(object)
	if ref != RAX {
		f.a.MovReg64(RAX, ref)
	}
	f.release(ref)
	done := f.emitNativeTypedCheck(0, false, test, nativeAbstractTypeMask(heap))
	value := f.pushReg(RAX, mtI64)
	f.setStackGCRoot(value, true)
	f.pushValue(storage{kind: stConst, typ: mtI64, cval: heap})
	if nullable {
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: 1})
	} else {
		f.pushValue(storage{kind: stConst, typ: mtI32})
	}
	f.pushValue(storage{kind: stConst, typ: mtI32})
	anyref := wasm.RefVal(wasm.Ref(true, wasm.AbsHeap(wasm.HeapAny), false))
	helper, resultType, resultMachine := uint32(gcStructRefCast), anyref, mtI64
	if test {
		helper, resultType, resultMachine = gcStructRefTest, wasm.I32, mtI32
	}
	if err := f.callGCStructHelper(helper, []wasm.ValType{anyref, wasm.I64, wasm.I32, wasm.I32}, []wasm.ValType{resultType}); err != nil {
		return err
	}
	// The fast edge did not execute the helper's lazy local spills. Reload
	// only on the cold edge, then use the original state at the join.
	f.reloadConditionalGCPinnedLocals(savedLocals[:len(f.pinnedLocals)])
	result := f.materialize(f.popValue())
	if result != RAX {
		f.a.MovReg64(RAX, result)
	}
	f.release(result)
	done.patch(f.a)
	value = f.pushReg(RAX, resultMachine)
	if !test {
		f.setStackGCRoot(value, true)
	}
	return nil
}
