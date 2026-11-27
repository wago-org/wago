// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

import (
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

const gcExternGlobalSet uint32 = 47
const (
	managedStructAggregateHelpers = 1<<gcStructAllocOne | 1<<gcStructSet
	managedArrayUniformHelpers    = 1<<gcArrayAllocUniform | 1<<gcArrayAllocUniformNative
	managedArrayFixedHelpers      = 1<<gcArrayAllocFixed | 1<<gcArrayAllocFixedNative
	managedArrayElementHelpers    = 1<<gcArrayAllocElem | 1<<gcArrayInitElem
	managedArrayAggregateHelpers  = managedArrayUniformHelpers | managedArrayFixedHelpers | managedArrayElementHelpers |
		1<<gcArraySet | 1<<gcArrayFill | 1<<gcArrayCopy
)
const (
	gcExceptionAlloc   uint32 = 45
	gcExceptionPayload uint32 = 46

	exceptionAllocHelperForImage   = ownedExceptionReferencesForImage && gcHelperMaskForImage&(1<<gcExceptionAlloc) != 0
	exceptionPayloadHelperForImage = ownedExceptionReferencesForImage && gcHelperMaskForImage&(1<<gcExceptionPayload) != 0
)

//lint:ignore U1000 retained as the unparked native helper entrypoint
func (in *Instance) dispatchGCHelper(helper uint32, args, results []uint64) {
	in.dispatchGCHelperParked(0, helper, 0, args, results)
}

func (in *Instance) dispatchGCHelperParked(ctrl uintptr, helper, safepoint uint32, args, results []uint64) {
	in.dispatchGCHelperParkedOnStack(ctrl, helper, safepoint, args, results, nil)
}

func (in *Instance) dispatchGCHelperParkedOnStack(ctrl uintptr, helper, safepoint uint32, args, results []uint64, stack *coreruntime.Engine) {
	functionSubtypeTest := functionSubtypeHelperForImage && helper == gcFuncRefTest
	if !nativeCollectorFrameRootsBuilt {
		if in != nil && functionSubtypeTest {
			in.dispatchGCFunctionRefTest(in, args, results)
			return
		}
		panic(gcHelperFailuref("collector helper %d has no live collector", helper))
	}
	if gcHelperMaskForImage&(1<<gcExternGlobalSet) != 0 && in.dispatchCompactExternStore(ctrl, helper, safepoint, args, results, stack) {
		return
	}
	if (exceptionAllocHelperForImage || exceptionPayloadHelperForImage) &&
		dispatchOwnedExceptionHelper(in, ctrl, helper, safepoint, args, results, stack) {
		return
	}
	if in == nil || (in.gc == nil && !functionSubtypeTest) {
		panic(gcHelperFailuref("collector helper %d has no live collector", helper))
	}
	defer drainGCMetadataReclaims(in.gc)
	if functionSubtypeTest {
		in.dispatchGCFunctionRefTest(in, args, results)
		return
	}

	// Externref table helpers share the collector lock and parked roots with GC
	// helpers. Their implementation is available independently of Wasm GC.
	lockedDomain := in.lockGCCollector()
	defer unlockGCCollector(lockedDomain)
	var state *gcPublicState
	var frameRoots gc.RootSet = gc.EmptyRoots{}
	externConversion := gcHelperMaskForImage&(1<<gcAnyConvertExtern) != 0 && helper == gcAnyConvertExtern ||
		gcHelperMaskForImage&(1<<gcExternConvertAny) != 0 && helper == gcExternConvertAny
	compactConversion := gcCompactExternDispatchForImage && externConversion && in.c.compactExternFrameRoots()
	if (wasmGCBuilt && (gcHelperMayAllocate(helper) || compactConversion || managedAggregateHelpersForImage != 0 && in.managedFunctionAggregateMayAllocate(helper, args))) ||
		compactTableAllocationForImage && in.compactExternPersistentMayAllocate(helper, args) {
		// Cancel reservations before Go allocation or a collecting fallback.
		in.gc.CancelNativeAllocationBatch()
		state = in.publicGCState()
		state.mu.Lock()
		defer state.mu.Unlock()
		if nativeCollectorFrameRootsBuilt {
			defer state.frameRoots.clearPhysicalStack()
		}
		frameRoots = in.gcHelperRoots(ctrl, state, safepoint, stack)
	}
	if wasmGCBuilt && managedAggregateHelpersForImage != 0 && in.dispatchManagedFunctionAggregate(helper, args, results, frameRoots, state) {
		return
	}
	if collectorTableHelpersForImage && in.dispatchGCTableHelperLocked(helper, args, results, lockedDomain, frameRoots) {
		return
	}
	if !wasmGCBuilt {
		panic(gcHelperFailuref("Wasm GC helper %d is disabled in this build", helper))
	}
	if (gcExternConversionForImage || gcCompactExternDispatchForImage) && externConversion {
		in.dispatchGCExternConversionLocked(helper, args, results, lockedDomain, frameRoots, compactConversion)
		return
	}
	if helper < gcArrayAllocDefault || helper == gcStructReserveDeadNative || helper == gcStructReserveDeadBatch {
		if !gcStructHelpersForImage {
			panic(gcHelperFailuref("GC struct helper %d exceeds the standalone runtime profile", helper))
		}
		in.dispatchGCStructHelperLocked(helper, args, results, state, frameRoots)
		return
	}
	if !gcArrayHelpersForImage {
		panic(gcHelperFailuref("GC array helper %d exceeds the standalone runtime profile", helper))
	}
	in.dispatchGCArrayHelperLocked(helper, args, results, state, frameRoots)
}
