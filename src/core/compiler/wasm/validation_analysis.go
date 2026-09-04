package wasm

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
	_               uint16
}

// ValidatedModuleAnalysis owns transient facts gathered by validation. Callers
// may consume or discard it after compilation; it is not retained by Module.
type ValidatedModuleAnalysis struct {
	Funcs           []ValidatedFuncFacts
	Flags           ValidatedFuncFlags
	BodyBytes       uint64
	InstructionCost uint64
}

func (a *ValidatedModuleAnalysis) reset(functions int) {
	*a = ValidatedModuleAnalysis{Funcs: make([]ValidatedFuncFacts, functions)}
}

func (a *ValidatedModuleAnalysis) finish() {
	for i := range a.Funcs {
		facts := &a.Funcs[i]
		a.Flags |= facts.Flags
		a.BodyBytes += uint64(facts.BodyBytes)
		a.InstructionCost += uint64(facts.InstructionCost)
	}
}

func (f *ValidatedFuncFacts) observe(kind InstrKind) {
	if f.InstructionCost != ^uint32(0) {
		f.InstructionCost++
	}
	switch kind {
	case InstrBlock, InstrIf, InstrTryTable, InstrBr, InstrBrIf, InstrBrTable,
		InstrBrOnNull, InstrBrOnNonNull, InstrBrOnCast, InstrBrOnCastFail,
		InstrReturn, InstrUnreachable:
		f.Flags |= ValidatedFuncHasControl
	case InstrLoop:
		f.Flags |= ValidatedFuncHasControl | ValidatedFuncHasLoop
	case InstrCall:
		f.Flags |= ValidatedFuncHasDirectCall
	case InstrReturnCall:
		f.Flags |= ValidatedFuncHasDirectCall | ValidatedFuncHasTailCall | ValidatedFuncHasControl
	case InstrCallIndirect:
		f.Flags |= ValidatedFuncHasIndirectCall
	case InstrReturnCallIndirect:
		f.Flags |= ValidatedFuncHasIndirectCall | ValidatedFuncHasTailCall | ValidatedFuncHasControl
	case InstrCallRef:
		f.Flags |= ValidatedFuncHasCallRef
	case InstrReturnCallRef:
		f.Flags |= ValidatedFuncHasCallRef | ValidatedFuncHasTailCall | ValidatedFuncHasControl
	case InstrGlobalGet, InstrGlobalSet:
		f.Flags |= ValidatedFuncTouchesGlobal
	case InstrTableGet, InstrTableSet, InstrTableSize, InstrTableGrow,
		InstrTableFill, InstrTableCopy, InstrTableInit, InstrElemDrop:
		f.Flags |= ValidatedFuncTouchesTable
	}
	if effect := opEffects[kind]; effect.cat == effLoad || effect.cat == effStore {
		f.Flags |= ValidatedFuncTouchesMemory
	}
}

func (f *ValidatedFuncFacts) observeDirect(op *directOp) {
	switch op.kind {
	case directInstr:
		f.observe(op.instr.Kind)
	case directBlock:
		f.observe(InstrBlock)
	case directLoop:
		f.observe(InstrLoop)
	case directIf:
		f.observe(InstrIf)
	case directTryTable:
		f.observe(InstrTryTable)
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
