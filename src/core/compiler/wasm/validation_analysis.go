package wasm

import "sync"

// ValidatedFuncFlags are architecture-neutral facts gathered while a function
// body is being validated. They describe only successfully validated code.
type ValidatedFuncFlags uint64

const (
	ValidatedFuncHasControl ValidatedFuncFlags = 1 << iota
	ValidatedFuncHasLoop
	ValidatedFuncHasDirectCall
	ValidatedFuncHasIndirectCall
	ValidatedFuncHasCallRef
	ValidatedFuncHasTailCall
	ValidatedFuncTouchesMemory
	ValidatedFuncTouchesTable
	ValidatedFuncTouchesGlobal
	ValidatedFuncUsesSIMD
	ValidatedFuncUsesThreads
	ValidatedFuncUsesBulkMemory
	ValidatedFuncUsesSignExtension
	ValidatedFuncUsesSaturatingTrunc
	ValidatedFuncUsesReferenceTypes
	ValidatedFuncUsesTypedFunctionReferences
	ValidatedFuncUsesGC
	ValidatedFuncUsesExceptionHandling
	ValidatedFuncUsesMemoryGrow
	ValidatedFuncUsesTableGrow
	ValidatedFuncUsesRefFunc
	ValidatedFuncUsesAtomicWait
	ValidatedFuncMayAllocate
	ValidatedFuncMayCollect
	ValidatedFuncDynamicReferenceCall
	ValidatedFuncUsesNonAtomicMemory
	// ValidatedFuncNeedsDetailedRequirements marks an instruction whose exact
	// persisted feature or footprint facts need more than the fixed summary.
	ValidatedFuncNeedsDetailedRequirements
	// ValidatedFuncNeedsDetailedAdmission marks proposal/product code that must
	// still pass through the frontend's contextual body admission scanner.
	ValidatedFuncNeedsDetailedAdmission
)

// ValidatedFuncFacts is the fixed, always-present summary produced for one
// successfully validated local function. Variable-sized facts belong in
// module-owned sidecars rather than per-function slices.
type ValidatedFuncFacts struct {
	Flags           ValidatedFuncFlags
	BodyBytes       uint32
	InstructionCost uint32
	LocalCount      uint16
	MaxOperandDepth uint16
	MaxControlDepth uint16
	// Segment counts saturate at 255 and set NeedsDetailedRequirements when the
	// exact count is larger, routing that uncommon function through the body scan.
	ElemStateCount uint8
	DataStateCount uint8
}

// ValidatedModuleAnalysis owns transient facts gathered by validation. Callers
// may consume or discard it after compilation; it is not retained by Module.
type ValidatedModuleAnalysis struct {
	Funcs           []ValidatedFuncFacts
	Flags           ValidatedFuncFlags
	BodyBytes       uint64
	InstructionCost uint64
	module          *Module
	valid           bool
}

func (a *ValidatedModuleAnalysis) reset(m *Module) {
	initValidatedFuncFlags()
	*a = ValidatedModuleAnalysis{Funcs: make([]ValidatedFuncFacts, len(m.Code)), module: m}
}

func (a *ValidatedModuleAnalysis) finish() {
	for i := range a.Funcs {
		facts := &a.Funcs[i]
		a.Flags |= facts.Flags
		a.BodyBytes += uint64(facts.BodyBytes)
		a.InstructionCost += uint64(facts.InstructionCost)
	}
	a.valid = true
}

// ValidFor reports whether a successful validation produced this analysis for
// m. Consumers must retain their exact scanner when the identity does not
// match; fixed summaries are proof artifacts, not caller assertions.
func (a *ValidatedModuleAnalysis) ValidFor(m *Module) bool {
	return a != nil && a.valid && a.module == m && len(a.Funcs) == len(m.Code)
}

func (f *ValidatedFuncFacts) observeInstruction(in *Instruction) {
	f.observe(in.Kind)
	for _, typ := range in.ValTypes() {
		f.observeValType(typ)
	}
	switch in.Kind {
	case InstrMemoryInit, InstrDataDrop:
		f.DataStateCount = f.recordSegmentStateCount(f.DataStateCount, in.Index)
	case InstrTableInit, InstrElemDrop:
		f.ElemStateCount = f.recordSegmentStateCount(f.ElemStateCount, in.Index)
	case InstrCallIndirect, InstrReturnCallIndirect:
		if in.Index2 != 0 {
			f.Flags |= ValidatedFuncUsesReferenceTypes
		}
	case InstrRefNull, InstrRefTest, InstrRefCast, InstrBrOnCast, InstrBrOnCastFail:
		// The heap immediate distinguishes typed function, GC, and exception
		// references. Keep the fixed summary compact and let these uncommon
		// functions use the exact existing scanner until a sparse heap sidecar
		// is justified by measurements.
		f.Flags |= ValidatedFuncNeedsDetailedRequirements
	}
}

// recordSegmentStateCount keeps the fixed per-function validation record small
// for the common case. A module with 256 or more segment state entries takes the
// existing exact requirements scan, so saturation never under-sizes runtime
// state and adds no variable sidecar to parallel validation.
func (f *ValidatedFuncFacts) recordSegmentStateCount(current uint8, index uint32) uint8 {
	if index >= uint32(^uint8(0)) {
		f.Flags |= ValidatedFuncNeedsDetailedRequirements
		return ^uint8(0)
	}
	count := uint8(index + 1)
	if count > current {
		return count
	}
	return current
}

func (f *ValidatedFuncFacts) observe(kind InstrKind) {
	if f.InstructionCost != ^uint32(0) {
		f.InstructionCost++
	}
	if kind < numInstrKinds {
		f.Flags |= validatedFuncFlagsByKind[kind]
		return
	}
	f.observeSlow(kind)
}

var (
	validatedFuncFlagsOnce   sync.Once
	validatedFuncFlagsByKind [numInstrKinds]ValidatedFuncFlags
)

func initValidatedFuncFlags() {
	validatedFuncFlagsOnce.Do(func() {
		for kind := InstrKind(0); kind < numInstrKinds; kind++ {
			var facts ValidatedFuncFacts
			facts.observeSlow(kind)
			validatedFuncFlagsByKind[kind] = facts.Flags
		}
	})
}

// observeSlow is the auditable source of the fixed instruction classifier.
// Successful validation uses validatedFuncFlagsByKind; tests compare every
// table entry with this definition so proposal additions cannot silently omit
// a resource or feature fact.
func (f *ValidatedFuncFacts) observeSlow(kind InstrKind) {
	switch kind {
	case InstrBlock, InstrIf, InstrTryTable, InstrBr, InstrBrIf, InstrBrTable,
		InstrBrOnNull, InstrBrOnNonNull, InstrBrOnCast, InstrBrOnCastFail,
		InstrReturn, InstrUnreachable:
		f.Flags |= ValidatedFuncHasControl
	case InstrLoop:
		f.Flags |= ValidatedFuncHasControl | ValidatedFuncHasLoop
	case InstrCall:
		f.Flags |= ValidatedFuncHasDirectCall | ValidatedFuncMayCollect
	case InstrReturnCall:
		f.Flags |= ValidatedFuncHasDirectCall | ValidatedFuncHasTailCall | ValidatedFuncHasControl | ValidatedFuncMayCollect
	case InstrCallIndirect:
		f.Flags |= ValidatedFuncHasIndirectCall | ValidatedFuncMayCollect
	case InstrReturnCallIndirect:
		f.Flags |= ValidatedFuncHasIndirectCall | ValidatedFuncHasTailCall | ValidatedFuncHasControl | ValidatedFuncMayCollect
	case InstrCallRef:
		f.Flags |= ValidatedFuncHasCallRef | ValidatedFuncUsesReferenceTypes | ValidatedFuncUsesTypedFunctionReferences | ValidatedFuncNeedsDetailedAdmission | ValidatedFuncMayCollect
	case InstrReturnCallRef:
		f.Flags |= ValidatedFuncHasCallRef | ValidatedFuncHasTailCall | ValidatedFuncHasControl | ValidatedFuncUsesReferenceTypes | ValidatedFuncUsesTypedFunctionReferences | ValidatedFuncNeedsDetailedAdmission | ValidatedFuncMayCollect
	case InstrGlobalGet, InstrGlobalSet:
		f.Flags |= ValidatedFuncTouchesGlobal
	case InstrTableGet, InstrTableSet, InstrTableSize, InstrTableGrow,
		InstrTableFill, InstrTableCopy, InstrTableInit, InstrElemDrop:
		f.Flags |= ValidatedFuncTouchesTable | ValidatedFuncUsesReferenceTypes
	}
	switch kind {
	case InstrReturnCall, InstrReturnCallIndirect, InstrReturnCallRef:
		f.Flags |= ValidatedFuncNeedsDetailedAdmission
	case InstrMemoryInit, InstrDataDrop, InstrMemoryCopy, InstrMemoryFill,
		InstrTableInit, InstrElemDrop, InstrTableCopy:
		f.Flags |= ValidatedFuncUsesBulkMemory
	case InstrI32Extend8S, InstrI32Extend16S, InstrI64Extend8S, InstrI64Extend16S, InstrI64Extend32S:
		f.Flags |= ValidatedFuncUsesSignExtension
	case InstrI32TruncSatF32S, InstrI32TruncSatF32U, InstrI32TruncSatF64S, InstrI32TruncSatF64U,
		InstrI64TruncSatF32S, InstrI64TruncSatF32U, InstrI64TruncSatF64S, InstrI64TruncSatF64U:
		f.Flags |= ValidatedFuncUsesSaturatingTrunc
	case InstrRefNull, InstrRefIsNull, InstrRefFunc, InstrRefEq, InstrRefAsNonNull,
		InstrBrOnNull, InstrBrOnNonNull:
		f.Flags |= ValidatedFuncUsesReferenceTypes
	case InstrRefI31, InstrI31GetS, InstrI31GetU, InstrRefTest, InstrRefCast,
		InstrBrOnCast, InstrBrOnCastFail, InstrAnyConvertExtern, InstrExternConvertAny:
		f.Flags |= ValidatedFuncUsesReferenceTypes | ValidatedFuncUsesGC | ValidatedFuncNeedsDetailedAdmission
	case InstrThrow, InstrThrowRef, InstrTryTable:
		f.Flags |= ValidatedFuncUsesExceptionHandling | ValidatedFuncNeedsDetailedAdmission
	case InstrMemoryGrow:
		f.Flags |= ValidatedFuncUsesMemoryGrow
	case InstrTableGrow:
		f.Flags |= ValidatedFuncUsesTableGrow
	case InstrMemoryAtomicNotify, InstrMemoryAtomicWait32, InstrMemoryAtomicWait64:
		f.Flags |= ValidatedFuncUsesAtomicWait
	}
	if kind == InstrRefFunc {
		f.Flags |= ValidatedFuncUsesRefFunc
	}
	if IsCoreAtomicInstructionKind(kind) {
		f.Flags |= ValidatedFuncUsesThreads | ValidatedFuncNeedsDetailedAdmission
	}
	if kind >= InstrV128Load && kind < numInstrKinds {
		f.Flags |= ValidatedFuncUsesSIMD
	}
	if kind >= InstrStringConst && kind <= InstrStringEncodeWtf8Array {
		f.Flags |= ValidatedFuncNeedsDetailedRequirements | ValidatedFuncNeedsDetailedAdmission
	}
	if kind >= InstrStructNew && kind <= InstrArrayInitElem {
		f.Flags |= ValidatedFuncUsesReferenceTypes | ValidatedFuncUsesGC | ValidatedFuncNeedsDetailedAdmission
	}
	if kind >= InstrRefGetDesc && kind <= InstrExternConvertAny {
		f.Flags |= ValidatedFuncUsesReferenceTypes | ValidatedFuncUsesGC | ValidatedFuncNeedsDetailedAdmission
	}
	if instructionTouchesMemory(kind) {
		f.Flags |= ValidatedFuncTouchesMemory
		if !IsCoreAtomicInstructionKind(kind) {
			f.Flags |= ValidatedFuncUsesNonAtomicMemory
		}
	}
	switch kind {
	case InstrStructNew, InstrStructNewDefault,
		InstrArrayNew, InstrArrayNewDefault, InstrArrayNewFixed, InstrArrayNewData, InstrArrayNewElem:
		f.Flags |= ValidatedFuncMayAllocate | ValidatedFuncMayCollect
	}
}

func instructionTouchesMemory(kind InstrKind) bool {
	if effect := opEffects[kind]; effect.cat == effLoad || effect.cat == effStore {
		return true
	}
	if IsCoreAtomicInstructionKind(kind) || kind == InstrMemorySize || kind == InstrMemoryGrow ||
		kind == InstrMemoryInit || kind == InstrMemoryCopy || kind == InstrMemoryFill {
		return true
	}
	return kind >= InstrV128Load && kind <= InstrV128Store ||
		kind >= InstrV128Load8Lane && kind <= InstrV128Load64Zero
}

func (f *ValidatedFuncFacts) observeValType(typ ValType) {
	switch typ.Kind() {
	case ValVec:
		f.Flags |= ValidatedFuncUsesSIMD
	case ValRef:
		f.Flags |= ValidatedFuncUsesReferenceTypes | ValidatedFuncNeedsDetailedRequirements | ValidatedFuncNeedsDetailedAdmission
	}
}

func (f *ValidatedFuncFacts) observeDirect(op *directOp) {
	switch op.kind {
	case directInstr:
		f.observeInstruction(&op.instr)
	case directBlock:
		f.observe(InstrBlock)
		if op.blockType.Kind == BlockVal {
			f.observeValType(op.blockType.Val)
		}
	case directLoop:
		f.observe(InstrLoop)
		if op.blockType.Kind == BlockVal {
			f.observeValType(op.blockType.Val)
		}
	case directIf:
		f.observe(InstrIf)
		if op.blockType.Kind == BlockVal {
			f.observeValType(op.blockType.Val)
		}
	case directTryTable:
		f.observe(InstrTryTable)
		if op.blockType.Kind == BlockVal {
			f.observeValType(op.blockType.Val)
		}
	}
}

func (f *ValidatedFuncFacts) recordDepths(operands, controls int) {
	if operands > int(f.MaxOperandDepth) {
		f.MaxOperandDepth = saturatingUint16(operands)
	}
	if controls > int(f.MaxControlDepth) {
		f.MaxControlDepth = saturatingUint16(controls)
	}
}

func saturatingUint16(n int) uint16 {
	if n >= int(^uint16(0)) {
		return ^uint16(0)
	}
	return uint16(n)
}

func saturatingUint32(n int) uint32 {
	if uint64(n) >= uint64(^uint32(0)) {
		return ^uint32(0)
	}
	return uint32(n)
}
