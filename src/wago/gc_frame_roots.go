package wago

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
)

const gcNativeFrameRootLimit = 64

func validGCFrameRootPlan(plan *shared.GCFrameRootPlan) bool {
	if plan == nil || !plan.Candidate || !plan.Exact || len(plan.Safepoints) == 0 || len(plan.LiveLocalMasks) != len(plan.Safepoints) || len(plan.LiveCallLocalMasks) != len(plan.Callsites) || plan.FrameBytes < shared.AMD64FrameHeaderBytes || len(plan.LocalIndexes) != len(plan.LocalOffsets) || len(plan.LocalOffsets) > gcNativeFrameRootLimit || (len(plan.Callsites) != 0 && plan.AdapterReturnOffset == 0) {
		return false
	}
	if !validGCFrameOffsets(plan.LocalOffsets, plan.FrameBytes) {
		return false
	}
	var previousReturn uint32
	for i := range plan.Callsites {
		callsite := &plan.Callsites[i]
		if callsite.ReturnOffset == 0 || (i != 0 && callsite.ReturnOffset <= previousReturn) || len(callsite.Offsets) > gcNativeFrameRootLimit || !validGCFrameOffsets(callsite.Offsets, plan.FrameBytes) {
			return false
		}
		previousReturn = callsite.ReturnOffset
	}
	var previousID uint32
	for i := range plan.Safepoints {
		safepoint := &plan.Safepoints[i]
		if safepoint.ID == 0 || safepoint.ID > shared.GCSafepointIDMax || (i != 0 && safepoint.ID <= previousID) || len(safepoint.Offsets) > gcNativeFrameRootLimit || !validGCFrameOffsets(safepoint.Offsets, plan.FrameBytes) {
			return false
		}
		previousID = safepoint.ID
	}
	return true
}

func validateCompiledGCFrameRoots(c *Compiled, rootMap *compiledGCFrameRoots) error {
	if rootMap == nil {
		return nil
	}
	if c == nil || !c.usesGenericGCExecution() {
		return fmt.Errorf("GC frame-root metadata requires generic GC execution")
	}
	if c.NumImports != 0 || len(c.Imports) != 0 || len(c.GlobalImports) != 0 || len(c.Globals) != 0 || c.HasStart || c.HasTable || len(c.Elems) != 0 || len(c.passiveElems) != 0 || (c.memoryDir != nil && len(c.memoryDir.ehTags) != 0) || len(c.Funcs) != 1 {
		return fmt.Errorf("GC frame-root metadata requires one import/global/start/table/element/tag-free local function")
	}
	for _, t := range c.Funcs[0].Params {
		if t == ValAnyRef || t == ValI31Ref {
			return fmt.Errorf("GC frame-root metadata rejects public GC-reference parameters")
		}
	}
	for _, t := range c.Funcs[0].Results {
		if t == ValAnyRef || t == ValI31Ref {
			return fmt.Errorf("GC frame-root metadata rejects public GC-reference results")
		}
	}
	if rootMap.frameBytes < shared.AMD64FrameHeaderBytes || rootMap.frameBytes > 1<<31-1 || len(rootMap.safepoints) == 0 {
		return fmt.Errorf("GC frame-root frame size %d or safepoint count %d is invalid", rootMap.frameBytes, len(rootMap.safepoints))
	}
	if len(rootMap.callsites) != 0 && (rootMap.adapterReturnOffset == 0 || uint64(rootMap.adapterReturnOffset) >= uint64(len(c.Code))) {
		return fmt.Errorf("GC frame-root adapter return offset %d is invalid", rootMap.adapterReturnOffset)
	}
	var previousReturn uint32
	for i := range rootMap.callsites {
		callsite := &rootMap.callsites[i]
		if callsite.returnOffset == 0 || uint64(callsite.returnOffset) >= uint64(len(c.Code)) || (i != 0 && callsite.returnOffset <= previousReturn) || len(callsite.offsets) > gcNativeFrameRootLimit || !validGCFrameOffsets(callsite.offsets, rootMap.frameBytes) {
			return fmt.Errorf("GC frame-root callsite %d is malformed", callsite.returnOffset)
		}
		previousReturn = callsite.returnOffset
	}
	var previousID uint32
	for i := range rootMap.safepoints {
		safepoint := &rootMap.safepoints[i]
		if safepoint.id == 0 || safepoint.id > shared.GCSafepointIDMax || (i != 0 && safepoint.id <= previousID) || len(safepoint.offsets) > gcNativeFrameRootLimit || !validGCFrameOffsets(safepoint.offsets, rootMap.frameBytes) {
			return fmt.Errorf("GC frame-root safepoint %d is malformed", safepoint.id)
		}
		previousID = safepoint.id
	}
	return nil
}

func validGCFrameOffsets(offsets []uint32, frameBytes uint32) bool {
	var previous uint32
	for i, off := range offsets {
		if off < shared.AMD64FrameHeaderBytes || off%8 != 0 || off > frameBytes-8 || (i != 0 && off <= previous) {
			return false
		}
		previous = off
	}
	return true
}
