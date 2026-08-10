package wago

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const gcNativeFrameRootLimit = shared.GCFrameRootLimit

// GCNativeRootAdmission describes whether a compiled generic-GC module can
// collect while native frames are active. Reason is populated for fail-closed
// collection-disabled admission. MetadataBytes is the direct serialized root-map
// payload estimate, excluding outer codec section framing.
type GCNativeRootAdmission struct {
	Required      bool
	Exact         bool
	Reason        string
	Safepoints    uint32
	Callsites     uint32
	MaximumRoots  uint32
	MetadataBytes uint64
}

// GCNativeRootAdmission reports exact native-root coverage and actionable
// fail-closed diagnostics without exposing live frames or process-local handles.
func (c *Compiled) GCNativeRootAdmission() GCNativeRootAdmission {
	status := GCNativeRootAdmission{Required: c != nil && c.usesGenericGCExecution()}
	if c == nil {
		status.Reason = "nil compiled module"
		return status
	}
	rootMap := c.genericGCFrameRoots()
	if rootMap == nil {
		if !status.Required {
			status.Reason = "module does not require generic collector execution"
		} else {
			status.Reason = c.gcRootAdmissionFailure()
		}
		return status
	}
	status.Exact = true
	status.Safepoints = uint32(len(rootMap.safepoints))
	status.Callsites = uint32(len(rootMap.callsites))
	status.MetadataBytes = uint64(compiledUvarintLen(uint64(len(rootMap.adapterReturnOffsets)))+len(rootMap.adapterReturnOffsets)*4) +
		uint64(compiledUvarintLen(uint64(len(rootMap.safepoints)))) +
		uint64(compiledUvarintLen(uint64(len(rootMap.callsites))))
	for i := range rootMap.safepoints {
		roots := len(rootMap.safepoints[i].offsets)
		if roots > int(status.MaximumRoots) {
			status.MaximumRoots = uint32(roots)
		}
		status.MetadataBytes += uint64(8 + compiledUvarintLen(uint64(roots)) + roots*4)
	}
	for i := range rootMap.callsites {
		roots := len(rootMap.callsites[i].offsets)
		if roots > int(status.MaximumRoots) {
			status.MaximumRoots = uint32(roots)
		}
		status.MetadataBytes += uint64(12 + compiledUvarintLen(uint64(roots)) + roots*4)
	}
	return status
}

func gcFrameCollectorElementExprSafe(expr wasm.Expr) bool {
	body := expr.BodyBytes
	if len(body) == 0 {
		encoded, err := wasm.EncodeExpr(expr)
		if err != nil {
			return false
		}
		body = encoded
	}
	// Module validation and elementPayloads perform the full type/opcode proof.
	// Frame-root admission only needs to recognize the retained, single-result GC
	// expression form so its passive slot can participate in persistent roots.
	return len(body) != 0 && body[len(body)-1] == 0x0b
}

func validGCModuleFrameRootPlan(module *shared.GCModuleFrameRootPlan) bool {
	if module == nil || len(module.Functions) == 0 {
		return false
	}
	totalSafepoints := 0
	var previousID uint32
	for _, plan := range module.Functions {
		if plan == nil {
			continue // proven non-collecting function; no active-frame map is needed
		}
		if !plan.Candidate || !plan.Exact || !plan.ValidLiveMasks() || len(plan.LiveLocalMasks) != len(plan.Safepoints) || len(plan.LocalIndexes) != len(plan.LocalOffsets) || len(plan.LocalOffsets) > gcNativeFrameRootLimit {
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
	if !validCompiledGCFunctionTables(c) || len(c.Funcs) == 0 {
		return fmt.Errorf("GC frame-root metadata requires a validated local call graph with private tables")
	}
	if c.HasStart && (c.StartIsImport || c.StartLocalFunc < 0 || c.StartLocalFunc >= len(c.Funcs)) {
		return fmt.Errorf("GC frame-root metadata requires an exact local start function")
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
		if off == 0 || uint64(off) >= uint64(len(c.code)) || (i != 0 && off <= previousAdapter) {
			return fmt.Errorf("GC frame-root adapter return offset %d is invalid", off)
		}
		previousAdapter = off
	}
	var previousReturn uint32
	for i := range rootMap.callsites {
		callsite := &rootMap.callsites[i]
		if callsite.frameBytes < shared.AMD64FrameHeaderBytes || callsite.frameBytes > 1<<31-1 || callsite.returnOffset == 0 || uint64(callsite.returnOffset) >= uint64(len(c.code)) || (i != 0 && callsite.returnOffset <= previousReturn) || callsite.stackAdjust%8 != 0 || callsite.stackAdjust > 1<<20 || len(callsite.offsets) > gcNativeFrameRootLimit || !validGCFrameOffsets(callsite.offsets, callsite.frameBytes) {
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
			if len(value.Expr) != 0 {
				if !c.usesGenericGCExecution() || (refType != ValAnyRef && refType != ValI31Ref) || value.Expr[len(value.Expr)-1] != 0x0b {
					return false
				}
				continue
			}
			if value.HasGlobal {
				if int(value.GlobalIndex) >= len(c.Globals) || c.Globals[value.GlobalIndex].Mutable {
					return false
				}
				if value.I31Wrap {
					if refType != ValI31Ref || c.Globals[value.GlobalIndex].Type != ValI32 {
						return false
					}
				} else if !isGCRefValType(c.Globals[value.GlobalIndex].Type) {
					return false
				}
				continue
			}
			if value.I31Wrap {
				return false
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
