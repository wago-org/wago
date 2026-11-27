// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !wago_no_gc || (linux && riscv64 && !tinygo && !wago_target_tinygo && !wago_no_exception_handling)

package wago

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

type compiledGCFrameRootState struct {
	roots *compiledGCFrameRoots
}

var importOnlyGCFrameRoots = compiledGCFrameRoots{}

func (c *Compiled) genericGCFrameRoots() *compiledGCFrameRoots {
	if c == nil {
		return nil
	}
	if memo := c.loadValidateMemo(); memo != nil && memo.compiledGCFrameRootState.roots != nil {
		return memo.compiledGCFrameRootState.roots
	}
	// A module with no local functions cannot have a parked native Wasm frame.
	// Its exact root set is therefore empty. Keep the collector-domain admission
	// for GC-bearing Runtime plugin imports without pretending a failed backend
	// root-plan build is a safety error.
	if len(c.Funcs) == 0 && c.hasCollectorReferenceCallBoundary() {
		return &importOnlyGCFrameRoots
	}
	return nil
}

// needsExactNativeGCRoots is the compile/artifact predicate. Allocating generic
// GC instructions and collector-reference host/cross-instance boundaries can
// both collect while native Wasm frames remain live.
func (c *Compiled) needsExactNativeGCRoots() bool {
	return c != nil && (c.usesGenericGCExecution() || c.hasCollectorReferenceCallBoundary() || c.needsOwnedExceptionReferences() || c.hasCollectorDeclarationRoots())
}

func (c *Compiled) needsNativeGCABI() bool {
	return c != nil && (c.needsExactNativeGCRoots() || c.usesGCStructHelpers() || c.usesGCArrayHelpers())
}

func (c *Compiled) hasCollectorDeclarationRoots() bool {
	return c != nil && c.stagedGCStructProduct() == 0 && c.stagedGCArrayProduct() == 0 &&
		c.nativeGCABIRequirement() != 0 && c.genericGCFrameRoots() != nil && (c.hasFunctionEnvironmentFrames() || c.hasOpaqueExternFrames() || c.hasInternalCollectorReferenceFunctions() || c.hasCollectorReferenceCallBoundary())
}

// needsRuntimeGCCollectorDomain is the instantiated-module predicate. A module
// may need a collector solely because a Runtime-owned host import allocates or
// inspects values selected by the caller's exact GC types.
func (c *Compiled) needsRuntimeGCCollectorDomain() bool {
	return c != nil && (c.usesGenericGCExecution() || c.hasCollectorReferenceCallBoundary() || c.needsOwnedExceptionReferences() || c.opaqueExternHelperSlots() != 0 || c.hasCollectorDeclarationRoots() && (len(c.genericGCFrameRoots().safepoints) != 0 || len(c.genericGCFrameRoots().callsites) != 0))
}

// Persistent extern storage can retain converted collector references even
// when the module has no allocating instruction of its own. The return value
// is the largest internal helper arity needed by that storage.
func (c *Compiled) opaqueExternHelperSlots() int {
	if !c.hasOpaqueExternFrames() {
		return 0
	}
	for i := 0; i < c.tableCount(); i++ {
		if c.tableElementType(i) == ValExternRef {
			return 5
		}
	}
	for _, global := range c.Globals {
		if global.Type == ValExternRef && global.Mutable {
			return 2
		}
	}
	return 0
}

func (c *Compiled) compactExternFrameRoots() bool {
	if !rv64CollectorBuilt {
		return false
	}
	return c != nil && c.nativeGCABIRequirement() == gc.NativeABIVersion && c.genericGCFrameRoots() != nil &&
		(c.usesGenericGCExecution() || c.needsOwnedExceptionReferences() || c.hasCollectorDeclarationRoots())
}

func (c *Compiled) setCompiledGCFrameRoots(gcFrameRoots *shared.GCModuleFrameRootPlan, deadInductionLayouts []uint64) {
	adapterCount, safepointCount, callsiteCount := 0, 0, 0
	for function := 0; function < gcFrameRoots.FunctionCount(); function++ {
		plan := gcFrameRoots.Function(function)
		if plan == nil {
			continue
		}
		if plan.AdapterReturnOffset != 0 {
			adapterCount++
		}
		safepointCount += plan.SafepointCount()
		callsiteCount += plan.CallsiteCount()
	}
	rootMap := &compiledGCFrameRoots{
		deadInductionLayouts: deadInductionLayouts,
		adapterReturnOffsets: make([]uint32, 0, adapterCount),
		safepoints:           make([]compiledGCFrameSafepoint, 0, safepointCount),
		callsites:            make([]compiledGCFrameCallsite, 0, callsiteCount),
	}
	var offsetInterner gcFrameOffsetInterner
	for function := 0; function < gcFrameRoots.FunctionCount(); function++ {
		plan := gcFrameRoots.Function(function)
		if plan == nil {
			continue
		}
		functionBase := uint32(c.Entry[function])
		if plan.AdapterReturnOffset != 0 {
			rootMap.adapterReturnOffsets = append(rootMap.adapterReturnOffsets, functionBase+plan.AdapterReturnOffset)
		}
		_ = plan.VisitSafepoints(func(i int, offsets []uint32) bool {
			rootMap.safepoints = append(rootMap.safepoints, compiledGCFrameSafepoint{id: plan.SafepointBase + uint32(i) + 1, frameBytes: plan.FrameBytes, offsets: offsetInterner.intern(offsets, true)})
			return true
		})
		_ = plan.VisitCallsites(func(_ int, callsite shared.GCFrameCallsite) bool {
			rootMap.callsites = append(rootMap.callsites, compiledGCFrameCallsite{returnOffset: functionBase + callsite.ReturnOffset(), frameBytes: callsite.FrameBytes(plan.FrameBytes), stackAdjust: callsite.StackAdjust(), offsets: offsetInterner.intern(callsite.Offsets(), true)})
			return true
		})
	}
	rootMap.adapterReturnOffsets = normalizeAdapterReturnOffsets(rootMap.adapterReturnOffsets)
	c.validateMemo.compiledGCFrameRootState.roots = rootMap
}
