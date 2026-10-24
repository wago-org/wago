package wago

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func moduleHasGCAllocationSitesWithValidation(m *wasm.Module, analysis *wasm.ValidatedModuleAnalysis) bool {
	if analysis.ValidFor(m) {
		allocates := analysis.Flags()&wasm.ValidatedFuncMayAllocate != 0
		if allocates || compiledArchitecture != 4 {
			return allocates
		}
		// An RV64 opaque-storage setter can allocate its first lifetime anchor.
		// The architecture-neutral Wasm summary only counts GC constructors.
	}
	return moduleHasGCAllocationSites(m)
}

func validGCModuleFrameRootPlanKinds(module *shared.GCModuleFrameRootPlan, allowFunctions, allowExterns bool) bool {
	if module == nil || module.FunctionCount() == 0 {
		return false
	}
	totalSafepoints, totalCallsites := 0, 0
	noCollectingBodies := true
	var previousID uint32
	for function := 0; function < module.FunctionCount(); function++ {
		if module.FunctionPending(function) {
			return false // fail closed if a producer omitted a collecting function
		}
		plan := module.Function(function)
		if plan == nil {
			continue // proven non-collecting function; no active-frame map is needed
		}
		if !plan.Candidate || !plan.Exact || !plan.ValidLiveMasks() || plan.AllocationMaskCount() != plan.SafepointCount() || len(plan.Locals) > shared.GCFrameTrackedLocalLimit {
			return false
		}
		active := plan.SafepointCount() != 0 || plan.CallsiteCount() != 0
		noCollectingBodies = noCollectingBodies && !active
		if active && plan.FrameBytes < 8 {
			return false
		}
		if active && !validGCFrameLocalsKinds(plan.Locals, plan.FrameBytes, allowFunctions, allowExterns) {
			return false
		}
		var previousReturn uint32
		if !plan.VisitCallsites(func(i int, callsite shared.GCFrameCallsite) bool {
			returnOffset, stackAdjust := callsite.ReturnOffset(), callsite.StackAdjust()
			if returnOffset == 0 || (i != 0 && returnOffset <= previousReturn) || stackAdjust%8 != 0 || stackAdjust > 1<<20 || !validGCFrameOffsetsKinds(callsite.Offsets(), plan.FrameBytes, allowFunctions, allowExterns) {
				return false
			}
			previousReturn = returnOffset
			totalCallsites++
			return true
		}) {
			return false
		}
		if !plan.VisitSafepoints(func(i int, offsets []uint32) bool {
			id64 := uint64(plan.SafepointBase) + uint64(i) + 1
			if id64 == 0 || id64 > uint64(shared.GCSafepointIDMax) || (totalSafepoints != 0 && uint32(id64) <= previousID) || !validGCFrameOffsetsKinds(offsets, plan.FrameBytes, allowFunctions, allowExterns) {
				return false
			}
			previousID = uint32(id64)
			totalSafepoints++
			return true
		}) {
			return false
		}
	}
	return totalSafepoints != 0 || totalCallsites != 0 || compiledArchitecture == 4 && noCollectingBodies
}
