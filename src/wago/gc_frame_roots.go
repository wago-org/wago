package wago

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
)

const gcNativeFrameRootLimit = 64

func validGCModuleFrameRootPlan(module *shared.GCModuleFrameRootPlan) bool {
	if module == nil || len(module.Functions) == 0 {
		return false
	}
	totalSafepoints := 0
	var previousID uint32
	for _, plan := range module.Functions {
		if plan == nil || !plan.Candidate || !plan.Exact || len(plan.LiveLocalMasks) != len(plan.Safepoints) || len(plan.LocalIndexes) != len(plan.LocalOffsets) || len(plan.LocalOffsets) > gcNativeFrameRootLimit {
			return false
		}
		active := len(plan.Safepoints) != 0 || len(plan.Callsites) != 0
		if active && plan.FrameBytes < shared.AMD64FrameHeaderBytes {
			return false
		}
		if active && !validGCFrameOffsets(plan.LocalOffsets, plan.FrameBytes) {
			return false
		}
		var previousReturn uint32
		for i := range plan.Callsites {
			callsite := &plan.Callsites[i]
			if callsite.ReturnOffset == 0 || (i != 0 && callsite.ReturnOffset <= previousReturn) || callsite.StackAdjust%8 != 0 || callsite.StackAdjust > 1<<20 || len(callsite.Offsets) > gcNativeFrameRootLimit || !validGCFrameOffsets(callsite.Offsets, plan.FrameBytes) {
				return false
			}
			previousReturn = callsite.ReturnOffset
		}
		for i := range plan.Safepoints {
			safepoint := &plan.Safepoints[i]
			if safepoint.ID == 0 || safepoint.ID > shared.GCSafepointIDMax || (totalSafepoints != 0 && safepoint.ID <= previousID) || len(safepoint.Offsets) > gcNativeFrameRootLimit || !validGCFrameOffsets(safepoint.Offsets, plan.FrameBytes) {
				return false
			}
			previousID = safepoint.ID
			totalSafepoints++
		}
	}
	return totalSafepoints != 0
}

func validateCompiledGCFrameRoots(c *Compiled, rootMap *compiledGCFrameRoots) error {
	if rootMap == nil {
		return nil
	}
	if c == nil || !c.usesGenericGCExecution() {
		return fmt.Errorf("GC frame-root metadata requires generic GC execution")
	}
	if c.HasStart || !validCompiledGCFunctionTables(c) || len(c.Funcs) == 0 {
		return fmt.Errorf("GC frame-root metadata requires a start-free local call graph with private tables")
	}
	for i := range c.GlobalImports {
		global := c.GlobalImports[i]
		if !isGCRefValType(global.Type) {
			return fmt.Errorf("GC frame-root metadata rejects global import %d", i)
		}
	}
	if c.NumImports != len(c.Imports) || len(c.importFuncSigs) != len(c.Imports) {
		return fmt.Errorf("GC frame-root metadata has inconsistent function imports")
	}
	for i := range c.importFuncSigs {
		if !gcFramePublicCallABI(c.importFuncSigs[i]) {
			return fmt.Errorf("GC frame-root metadata rejects import %d signature", i)
		}
	}
	if len(rootMap.safepoints) == 0 {
		return fmt.Errorf("GC frame-root metadata has no safepoints")
	}
	var previousAdapter uint32
	for i, off := range rootMap.adapterReturnOffsets {
		if off == 0 || uint64(off) >= uint64(len(c.Code)) || (i != 0 && off <= previousAdapter) {
			return fmt.Errorf("GC frame-root adapter return offset %d is invalid", off)
		}
		previousAdapter = off
	}
	var previousReturn uint32
	for i := range rootMap.callsites {
		callsite := &rootMap.callsites[i]
		if callsite.frameBytes < shared.AMD64FrameHeaderBytes || callsite.frameBytes > 1<<31-1 || callsite.returnOffset == 0 || uint64(callsite.returnOffset) >= uint64(len(c.Code)) || (i != 0 && callsite.returnOffset <= previousReturn) || callsite.stackAdjust%8 != 0 || callsite.stackAdjust > 1<<20 || len(callsite.offsets) > gcNativeFrameRootLimit || !validGCFrameOffsets(callsite.offsets, callsite.frameBytes) {
			return fmt.Errorf("GC frame-root callsite %d is malformed", callsite.returnOffset)
		}
		previousReturn = callsite.returnOffset
	}
	var previousID uint32
	for i := range rootMap.safepoints {
		safepoint := &rootMap.safepoints[i]
		if safepoint.frameBytes < shared.AMD64FrameHeaderBytes || safepoint.frameBytes > 1<<31-1 || safepoint.id == 0 || safepoint.id > shared.GCSafepointIDMax || (i != 0 && safepoint.id <= previousID) || len(safepoint.offsets) > gcNativeFrameRootLimit || !validGCFrameOffsets(safepoint.offsets, safepoint.frameBytes) {
			return fmt.Errorf("GC frame-root safepoint %d is malformed", safepoint.id)
		}
		previousID = safepoint.id
	}
	return nil
}

func validCompiledGCFunctionTables(c *Compiled) bool {
	if c == nil {
		return false
	}
	totalFuncs := c.NumImports + len(c.Funcs)
	validValues := func(refType ValType, values []RefInit) bool {
		refType = normalizedElemRefType(refType)
		for _, value := range values {
			if value.HasGlobal {
				if int(value.GlobalIndex) >= len(c.Globals) || c.Globals[value.GlobalIndex].Mutable || !isGCRefValType(c.Globals[value.GlobalIndex].Type) {
					return false
				}
				continue
			}
			if refType == ValFuncRef {
				if !value.Null && int(value.FuncIndex) >= totalFuncs {
					return false
				}
				continue
			}
			if refType == ValI31Ref {
				if !value.Null && value.FuncIndex&1 == 0 {
					return false
				}
				continue
			}
			if !isGCRefValType(refType) || !value.Null {
				return false
			}
		}
		return true
	}
	for tableIndex := 0; tableIndex < c.tableCount(); tableIndex++ {
		typ := c.tableElementType(tableIndex)
		if typ != ValFuncRef && !isGCRefValType(typ) {
			return false
		}
		if tableIndex == 0 {
			if c.HasTableInitFunc && (typ != ValFuncRef || int(c.TableInitFunc) >= totalFuncs) {
				return false
			}
			continue
		}
		table := &c.extraTables[tableIndex-1]
		if table.HasInitFunc && (typ != ValFuncRef || int(table.InitFunc) >= totalFuncs) {
			return false
		}
	}
	for i := range c.Elems {
		elem := &c.Elems[i]
		if elem.Mode != ElemModeActive || int(elem.TableIndex) >= c.tableCount() {
			return false
		}
		if normalizedElemRefType(elem.RefType) != c.tableElementType(int(elem.TableIndex)) || !validValues(elem.RefType, elem.Values) {
			return false
		}
	}
	for i := range c.passiveElems {
		elem := &c.passiveElems[i]
		if (elem.Mode != ElemModeDeclarative && elem.Mode != ElemModePassive) || !validValues(elem.RefType, elem.Values) {
			return false
		}
	}
	return true
}

func gcFramePublicCallABI(sig FuncSig) bool {
	if len(sig.Results) > 2 {
		return false
	}
	gp, fp := 0, 0
	for _, t := range sig.Params {
		switch t {
		case ValI32, ValI64, ValAnyRef, ValI31Ref:
			gp++
		case ValF32, ValF64:
			fp++
		default:
			return false
		}
	}
	if gp > 7 || fp > 8 {
		return false
	}
	integerResult := func(t ValType) bool {
		return t == ValI32 || t == ValI64 || t == ValAnyRef || t == ValI31Ref
	}
	for _, t := range sig.Results {
		if !integerResult(t) && t != ValF32 && t != ValF64 {
			return false
		}
	}
	return len(sig.Results) != 2 || (integerResult(sig.Results[0]) && integerResult(sig.Results[1]))
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
