package wasm

import "sync"

// ValidatedFuncFlags are architecture-neutral facts gathered while a function
// body is being validated. They describe only successfully validated code.
type ValidatedFuncFlags uint32

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
	// Type-indexed block encodings require multi-value even with zero results.
	ValidatedFuncUsesMultiValue
)

// ValidatedFuncFacts is the fixed, always-present summary produced for one
// successfully validated local function. Variable-sized facts belong in
// module-owned sidecars rather than per-function slices.
type ValidatedFuncFacts struct {
	Flags     ValidatedFuncFlags
	BodyBytes uint32
}

// ValidatedModuleAnalysis owns transient facts gathered by validation. Storage
// is private; accessors return values. The compilation phase must keep Module
// and its nested storage immutable from validation through the last consumer.
// It is not retained by Module. Mutation requires fresh validation.
type ValidatedModuleAnalysis struct {
	funcs          []ValidatedFuncFacts
	flags          ValidatedFuncFlags
	elemStateCount uint32
	dataStateCount uint32
	module         *Module
	valid          bool
}

func (a *ValidatedModuleAnalysis) reset(m *Module) {
	initValidatedFuncFlags()
	*a = ValidatedModuleAnalysis{funcs: make([]ValidatedFuncFacts, len(m.Code)), module: m}
}

func (a *ValidatedModuleAnalysis) finish() {
	for i := range a.funcs {
		facts := &a.funcs[i]
		// Tree validation does not gather instruction facts. Refuse the whole
		// analysis, including mixed modules, so every consumer uses its fallback.
		if facts.BodyBytes == 0 {
			return
		}
		a.flags |= facts.Flags
	}
	a.valid = true
}

// ValidFor reports whether a successful validation produced this analysis for
// m. Consumers must retain their exact scanner when the identity does not
// match; fixed summaries are proof artifacts, not caller assertions.
func (a *ValidatedModuleAnalysis) ValidFor(m *Module) bool {
	return a != nil && a.valid && a.module == m && len(a.funcs) == len(m.Code)
}

// FuncCount returns the number of local function summaries.
func (a *ValidatedModuleAnalysis) FuncCount() int { return len(a.funcs) }

// Func returns a copy of one local function's facts. Use only after ValidFor.
func (a *ValidatedModuleAnalysis) Func(index int) ValidatedFuncFacts { return a.funcs[index] }

// Flags returns the union of all function flags. Use only after ValidFor.
func (a *ValidatedModuleAnalysis) Flags() ValidatedFuncFlags { return a.flags }

// ElemStateCount returns the required element-state slots after ValidFor.
func (a *ValidatedModuleAnalysis) ElemStateCount() uint32 { return a.elemStateCount }

// DataStateCount returns the required data-state slots after ValidFor.
func (a *ValidatedModuleAnalysis) DataStateCount() uint32 { return a.dataStateCount }

func (v *funcValidator) observeValidatedInstruction(f *ValidatedFuncFacts, in *Instruction, segmentCounts *validationSegmentCounts) {
	f.observe(in.Kind)
	if in.Kind < numInstrKinds && !validatedFuncNeedsPayloadByKind[in.Kind] {
		return
	}
	v.observeValidatedInstructionPayload(f, in, segmentCounts)
}

// observeValidatedInstructionPayload records the uncommon facts that depend on
// an instruction immediate rather than only its kind. The byte-backed validator
// calls this only for kinds marked in validatedFuncNeedsPayloadByKind, keeping
// the ordinary scalar instruction path to two table lookups and one OR.
func (v *funcValidator) observeValidatedInstructionPayload(f *ValidatedFuncFacts, in *Instruction, segmentCounts *validationSegmentCounts) {
	for _, typ := range in.ValTypes() {
		f.observeValType(typ)
	}
	switch in.Kind {
	case InstrMemoryInit, InstrDataDrop:
		segmentCounts.data = max(segmentCounts.data, segmentStateCount(in.Index))
	case InstrTableInit, InstrElemDrop:
		segmentCounts.elem = max(segmentCounts.elem, segmentStateCount(in.Index))
	case InstrCallIndirect, InstrReturnCallIndirect:
		if in.Index2 != 0 {
			f.Flags |= ValidatedFuncUsesReferenceTypes
		}
		v.observeValidatedDynamicCall(f, in.Index)
	case InstrCallRef, InstrReturnCallRef:
		v.observeValidatedDynamicCall(f, in.Index)
	case InstrRefNull, InstrRefTest, InstrRefCast, InstrBrOnCast, InstrBrOnCastFail:
		// The heap immediate distinguishes typed function, GC, and exception
		// references. Keep the fixed summary compact and let these uncommon
		// functions use the exact existing scanner until a sparse heap sidecar
		// is justified by measurements.
		f.Flags |= ValidatedFuncNeedsDetailedRequirements | ValidatedFuncNeedsDetailedAdmission
	}
}

func (v *funcValidator) observeValidatedDynamicCall(f *ValidatedFuncFacts, typeIndex uint32) {
	ft := v.funcTypeFromTypeIdx(TypeIdx{Index: typeIndex})
	if ft == nil {
		return
	}
	for _, typ := range ft.Params {
		if typ.Kind() == ValRef {
			f.Flags |= ValidatedFuncDynamicReferenceCall
			return
		}
	}
	for _, typ := range ft.Results {
		if typ.Kind() == ValRef {
			f.Flags |= ValidatedFuncDynamicReferenceCall
			return
		}
	}
}

func (f *ValidatedFuncFacts) observe(kind InstrKind) {
	if kind < numInstrKinds {
		f.Flags |= validatedFuncFlagsByKind[kind]
		return
	}
	f.observeSlow(kind)
}

var (
	validatedFuncFlagsOnce          sync.Once
	validatedFuncFlagsByKind        [numInstrKinds]ValidatedFuncFlags
	validatedFuncNeedsPayloadByKind [numInstrKinds]bool
)

func initValidatedFuncFlags() {
	validatedFuncFlagsOnce.Do(func() {
		for kind := InstrKind(0); kind < numInstrKinds; kind++ {
			var facts ValidatedFuncFacts
			facts.observeSlow(kind)
			validatedFuncFlagsByKind[kind] = facts.Flags
			validatedFuncNeedsPayloadByKind[kind] = validatedInstructionNeedsPayload(kind)
		}
	})
}

func validatedInstructionNeedsPayload(kind InstrKind) bool {
	switch kind {
	case InstrSelect,
		InstrMemoryInit, InstrDataDrop, InstrTableInit, InstrElemDrop,
		InstrCallIndirect, InstrReturnCallIndirect, InstrCallRef, InstrReturnCallRef,
		InstrRefNull, InstrRefTest, InstrRefCast, InstrBrOnCast, InstrBrOnCastFail:
		return true
	default:
		return false
	}
}

// observeSlow is the auditable source of the fixed instruction classifier.
// Successful validation uses validatedFuncFlagsByKind. Table synchronization
// tests check its construction; independent admission and requirements tests
// check that the classification is sufficient to replace exact scanning.
func (f *ValidatedFuncFacts) observeSlow(kind InstrKind) {
	// Only these classes have complete admission rules in the fixed summary.
	// New instruction kinds retain exact admission and requirement scanning.
	if !validatedInstructionAdmissionComplete(kind) {
		f.Flags |= ValidatedFuncNeedsDetailedAdmission | ValidatedFuncNeedsDetailedRequirements
	}
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
		f.Flags |= ValidatedFuncHasIndirectCall | ValidatedFuncTouchesTable | ValidatedFuncMayCollect
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
	case InstrRefNull, InstrRefIsNull, InstrRefFunc, InstrRefEq:
		f.Flags |= ValidatedFuncUsesReferenceTypes
	case InstrRefAsNonNull, InstrBrOnNull, InstrBrOnNonNull:
		f.Flags |= ValidatedFuncUsesReferenceTypes | ValidatedFuncUsesTypedFunctionReferences | ValidatedFuncNeedsDetailedAdmission
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

// Keep the proposal classes explicit. Numeric ranges are closed at their last
// named member; appending a new proposal does not grant it summary admission.
func validatedInstructionAdmissionComplete(kind InstrKind) bool {
	if kind >= InstrI32Const && kind <= InstrI64Extend32S ||
		kind >= InstrI32Load && kind <= InstrMemoryGrow ||
		kind >= InstrI32TruncSatF32S && kind <= InstrI64TruncSatF64U ||
		kind >= InstrV128Load && kind <= InstrF64x2ConvertLowI32x4U {
		return true
	}
	switch kind {
	case InstrUnreachable, InstrNop, InstrBlock, InstrLoop, InstrIf,
		InstrBr, InstrBrIf, InstrBrTable, InstrReturn, InstrCall, InstrCallIndirect,
		InstrDrop, InstrSelect, InstrLocalGet, InstrLocalSet, InstrLocalTee,
		InstrGlobalGet, InstrGlobalSet, InstrTableGet, InstrTableSet,
		InstrMemoryInit, InstrDataDrop, InstrMemoryCopy, InstrMemoryFill,
		InstrTableInit, InstrElemDrop, InstrTableCopy, InstrTableGrow, InstrTableSize, InstrTableFill,
		InstrRefIsNull, InstrRefFunc, InstrRefEq:
		return true
	default:
		return false
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

func (f *ValidatedFuncFacts) observeStructuredDirect(op *directOp) {
	if op.blockType.Kind == BlockTypeIndex {
		f.Flags |= ValidatedFuncUsesMultiValue
	}
	switch op.kind {
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

func saturatingUint32(n int) uint32 {
	if uint64(n) >= uint64(^uint32(0)) {
		return ^uint32(0)
	}
	return uint32(n)
}
