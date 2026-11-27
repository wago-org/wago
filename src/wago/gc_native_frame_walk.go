// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !wago_standalone_no_collector && (!wago_no_gc || (linux && riscv64 && !tinygo && !wago_target_tinygo && !wago_no_exception_handling))

package wago

import (
	"sort"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

func gcNativeFrameFitsStack(base uintptr, frameBytes uint32, stack *coreruntime.Engine) bool {
	if stack == nil {
		return true
	}
	top := stack.StackTop()
	bytes := uintptr(stack.StackBytes())
	return bytes != 0 && top >= bytes && base >= top-bytes && base < top && uintptr(frameBytes) <= top-base
}

func (r *gcNativeFrameRoots) rangeChain(fn func(gc.RootSlot) bool, sink gc.RootRefSink, stack *coreruntime.Engine) bool {
	owner := r.owner
	base, offsets, frameBytes := r.base, r.offsets, r.frameBytes
	codeBase, codeBytes := r.codeBase, r.codeBytes
	adapterReturnOffsets, callsites := r.adapterReturnOffsets, r.callsites
	// Parked host activations carry their physical stack bounds because reentry
	// may replace owner.eng while their native frames remain on the old stack.
	// Other chains use the owner mapping only when their first frame lies in it.
	var stackTop uintptr
	if stack != nil {
		stackTop = stack.StackTop()
		if !gcNativeFrameFitsStack(base, frameBytes, stack) {
			panic(gcHelperFailuref("generic GC native frame exceeds stack bounds"))
		}
	} else if owner != nil && owner.eng != nil {
		top := owner.eng.StackTop()
		ownerStackBytes := uintptr(owner.eng.StackBytes())
		if ownerStackBytes != 0 && top >= ownerStackBytes && base >= top-ownerStackBytes && base < top {
			stackTop = top
		}
	}
	for {
		// A host-activation aggregate can carry no direct native frame of its own;
		// its parked activation records are walked separately below.
		if base == 0 {
			return true
		}
		if stackTop != 0 && (base >= stackTop || uintptr(frameBytes) > stackTop-base) {
			panic(gcHelperFailuref("generic GC native frame exceeds stack bounds"))
		}
		if managedFunctionGraphForImage && r.owner != nil && !visitManagedFunctionOwnerFrameRoot(owner, r.owner.gc, fn, sink) {
			return false
		}
		for _, off := range offsets {
			encoded := off
			off = shared.GCFrameRootOffset(encoded)
			if encoded&shared.GCFrameExternRefRootFlag != 0 {
				word := nativeGCFrameWord(base + uintptr(off))
				if r.owner == nil || !visitCompactExternFrameRoot(owner, r.owner.gc, word, fn, sink) {
					return false
				}
				continue
			}
			if encoded&shared.GCFrameFuncRefRootFlag != 0 {
				word := nativeGCFrameWord(base + uintptr(off))
				if !visitManagedFunctionFrameRoot(owner, word, fn, sink) {
					return false
				}
				continue
			}
			// gc.Ref is the low 32 bits of the validated little-endian native qword.
			// Expose the actual off-heap word so a moving/reference-rewriting policy
			// updates the parked frame rather than a copied scratch value.
			slot := nativeGCFrameRootSlot(base + uintptr(off))
			if sink != nil {
				if !sink.VisitRootRef(slot.GetRef()) {
					return false
				}
			} else if !fn(slot) {
				return false
			}
		}
		// A callsite-free foreign target can still return through a caller owned by
		// another instance. Inspect the saved return PC before concluding that the
		// chain is complete; the caller's root plan supplies the continuation map.
		returnPCBias, callerFrameBias := uintptr(0), uintptr(abi.AMD64CallReturnAddressBytes)
		returnAddressAfterFrame := true
		switch r.frameLayout & gcNativeFrameLayoutMask {
		case gcNativeFrameLayoutARM64:
			returnPCBias = uintptr(shared.ARM64SavedLROffset)
			callerFrameBias = uintptr(shared.ARM64FrameRecordBytes)
		case gcNativeFrameLayoutRISCV64:
			// RV64 saves RA in word zero of the callee's allocated frame. The
			// caller frame begins immediately after that complete allocation.
			returnAddressAfterFrame = false
			callerFrameBias = 0
		}
		frameEnd := base
		if returnAddressAfterFrame {
			if base > ^uintptr(0)-uintptr(frameBytes) {
				panic(gcHelperFailuref("generic GC native frame address overflows"))
			}
			frameEnd += uintptr(frameBytes)
		}
		if frameEnd > ^uintptr(0)-returnPCBias {
			panic(gcHelperFailuref("generic GC native frame address overflows"))
		}
		retAddr := frameEnd + returnPCBias
		if stackTop != 0 && (retAddr > stackTop || stackTop-retAddr < 8) {
			panic(gcHelperFailuref("generic GC native return address exceeds stack bounds"))
		}
		retPC := uintptr(nativeGCFrameWord(retAddr))
		if retPC < codeBase || retPC-codeBase >= codeBytes {
			if owner == nil || owner.refStore == nil || !owner.refStore.ownsGCCollector(owner.gc) {
				return true
			}
			foreign := owner.refStore.gcFrameOwner(retPC, owner.gc)
			if foreign == nil {
				if r.allowExternalReturn {
					return true
				}
				panic(gcHelperFailuref("generic GC foreign return PC %#x has no Runtime GC-domain owner", retPC))
			}
			owner = foreign
			plan := foreign.c.genericGCFrameRoots()
			codeBase, codeBytes = foreign.base, uintptr(len(foreign.c.code))
			adapterReturnOffsets, callsites = plan.adapterReturnOffsets, plan.callsites
		}
		rel := uint32(retPC - codeBase)
		adapterIndex := 0
		if len(adapterReturnOffsets) <= 8 {
			for adapterIndex < len(adapterReturnOffsets) && adapterReturnOffsets[adapterIndex] < rel {
				adapterIndex++
			}
		} else {
			adapterIndex = sort.Search(len(adapterReturnOffsets), func(i int) bool { return adapterReturnOffsets[i] >= rel })
		}
		if adapterIndex < len(adapterReturnOffsets) && adapterReturnOffsets[adapterIndex] == rel {
			return true
		}
		if base > ^uintptr(0)-uintptr(frameBytes)-callerFrameBias {
			panic(gcHelperFailuref("generic GC caller frame address overflows"))
		}
		returnBase := base + uintptr(frameBytes) + callerFrameBias
		// Tiny sorted tables beat binary-search setup; larger tables retain
		// logarithmic lookup. The measured crossover is between 8 and 16.
		callsiteIndex := 0
		if len(callsites) <= 8 {
			for callsiteIndex < len(callsites) && callsites[callsiteIndex].returnOffset < rel {
				callsiteIndex++
			}
		} else {
			callsiteIndex = sort.Search(len(callsites), func(i int) bool { return callsites[i].returnOffset >= rel })
		}
		if callsiteIndex == len(callsites) || callsites[callsiteIndex].returnOffset != rel {
			panic(gcHelperFailuref("generic GC native return offset %d has no callsite map", rel))
		}
		callsite := &callsites[callsiteIndex]
		offsets = callsite.offsets
		frameBytes = callsite.frameBytes
		stackAdjust := callsite.stackAdjust
		if returnBase > ^uintptr(0)-uintptr(stackAdjust) {
			panic(gcHelperFailuref("generic GC caller stack adjustment overflows"))
		}
		base = returnBase + uintptr(stackAdjust)
	}
}
