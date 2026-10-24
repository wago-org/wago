package wasm

// ValidationHintInstructionObserver receives successful byte-backed validation
// events. Inputs are scalar copies; no temporary decoder storage escapes.
// Kind is meaningful for ordinary instructions; structured events use Opcode.
type ValidationHintInstructionObserver interface {
	Observe(op byte, kind InstrKind, index, index2 uint32, bits uint64, meta uint8, start, end int)
}

// ValidationHintObserver owns one serial validation's transient hint state.
// Begin/End are ordered by local function index. Complete may decline reuse.
// Observer state, like Module, must remain immutable after successful validation.
type ValidationHintObserver interface {
	BeginFunction(index int) ValidationHintInstructionObserver
	EndFunction(index int)
	Complete() bool
}

type ValidationHintFactory func(*Module) ValidationHintObserver

const (
	ValidationHintTouchesMemory uint8 = 1 << iota
	ValidationHintHasMemoryIndex
)

// ValidateModuleWithHintObserver is the optional serial hint-fusion entry point.
// Factories run only after declaration validation. Parallel and tree validation
// retain ordinary analysis and can decline the optimization without changing errors.
func ValidateModuleWithHintObserver(m *Module, features ValidationFeatures, workers int, limits ValidationLimits, analysis *ValidatedModuleAnalysis, factory ValidationHintFactory) error {
	return validateModuleWithWorkersFeaturesAndLimitsAnalysisObserver(m, nil, workers, features, limits, analysis, factory)
}

// HintObserver returns a completed observer only with successful fixed analysis.
func (a *ValidatedModuleAnalysis) HintObserver() ValidationHintObserver {
	if a == nil || !a.valid {
		return nil
	}
	return a.hintObserver
}

func observeValidationHint(observer ValidationHintInstructionObserver, op byte, start, end int, in *Instruction) {
	index, index2, bits, meta := in.Index, in.Index2, uint64(0), uint8(0)
	switch op {
	case 0x41:
		bits = uint64(int64(in.I32))
	case 0x42:
		bits = uint64(in.I64)
	case 0x43:
		bits = uint64(in.F32Bits)
	case 0x44:
		bits = in.F64Bits
	}
	if op >= 0x28 && op <= 0x40 || op >= 0xfb && instructionTouchesMemory(in.Kind) {
		meta |= ValidationHintTouchesMemory
		if op >= 0x28 && op <= 0x3e || op == 0xfd || op == 0xfe {
			ma := in.MemArg()
			bits = ma.Offset
			index = 0
			if ma.Mem != nil {
				index = uint32(*ma.Mem)
				meta |= ValidationHintHasMemoryIndex
			}
		}
	}
	observer.Observe(op, in.Kind, index, index2, bits, meta, start, end)
}
