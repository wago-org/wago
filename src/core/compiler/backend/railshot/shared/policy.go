package shared

import "github.com/wago-org/wago/src/core/compiler/optimization"

// OptimizationObjective is the coherent top-level tradeoff selected for one
// compilation. Individual optimization flags remain available for tests and
// bisection, while production decisions consume this objective through Policy.
type OptimizationObjective uint8

const (
	OptimizeSpeed OptimizationObjective = iota
	OptimizeBalanced
	OptimizeSize
	OptimizeEmbedded
)

// CodegenPolicy is one immutable per-compilation policy. Selection captures all
// catalog flags in a compact bitset. The numeric fields bound later finalizer,
// layout, inlining, and machine-window decisions without package-global state.
type CodegenPolicy struct {
	Objective     OptimizationObjective
	Capabilities  Capabilities
	Selection     optimization.Selection
	CompactNative bool

	FunctionAlignLog2 uint8
	InternalAlignLog2 uint8
	LoopAlignLog2     uint8

	MaxMachineWindow       uint8
	MaxRelaxIterations     uint8
	MaxFinalizerDeletions  uint8
	MaxRel32Sites          uint16
	MaxLoopCompactionBytes uint32
	MaxJumpTableBranches   uint8
	MaxJumpTableRelaxIters uint8
	MaxSizeInlineBodyBytes uint8
}

func (p CodegenPolicy) Enabled(name string) bool { return p.Selection.Enabled(name) }
func (p CodegenPolicy) EnabledOption(option optimization.Option) bool {
	return p.Selection.EnabledOption(option)
}
func (p CodegenPolicy) Valid() bool { return p.Selection.Valid() }

// DefaultCodegenPolicy preserves the public Balanced default.
func DefaultCodegenPolicy(selection optimization.Selection) CodegenPolicy {
	return CodegenPolicyForObjective(selection, OptimizeBalanced)
}

// CodegenPolicyForObjective resolves the small immutable policy consumed by a
// compilation. The four objectives own layout choices; individual selection
// bits remain available for testing and bisection.
func CodegenPolicyForObjective(selection optimization.Selection, objective OptimizationObjective) CodegenPolicy {
	functionAlign, internalAlign, loopAlign := uint8(4), uint8(4), uint8(4)
	compactNative := false
	maxFinalizerDeletions := uint8(8)
	maxRel32Sites := uint16(256)
	maxLoopCompactionBytes := uint32(16 << 10)
	maxJumpTableBranches := uint8(0)
	maxJumpTableRelaxIters := uint8(0)
	maxSizeInlineBodyBytes := uint8(0)
	if objective == OptimizeSize || objective == OptimizeEmbedded {
		// Zero requests the target's minimum legal code alignment. Backends clamp
		// it to their instruction/data requirements.
		functionAlign, internalAlign, loopAlign = 0, 0, 0
		maxSizeInlineBodyBytes = 16
		if CompiledCapabilities.Has(CapabilityNativeCompaction) {
			compactNative = true
			maxFinalizerDeletions = MaxWideOffsetMapDeletions
			maxRel32Sites = 2048
			maxLoopCompactionBytes = 64 << 10
			maxJumpTableBranches = 32
			maxJumpTableRelaxIters = 1
		}
	}
	return CodegenPolicy{
		Objective:              objective,
		Capabilities:           CompiledCapabilities,
		Selection:              selection,
		CompactNative:          compactNative,
		FunctionAlignLog2:      functionAlign,
		InternalAlignLog2:      internalAlign,
		LoopAlignLog2:          loopAlign,
		MaxMachineWindow:       24,
		MaxRelaxIterations:     8,
		MaxFinalizerDeletions:  maxFinalizerDeletions,
		MaxRel32Sites:          maxRel32Sites,
		MaxLoopCompactionBytes: maxLoopCompactionBytes,
		MaxJumpTableBranches:   maxJumpTableBranches,
		MaxJumpTableRelaxIters: maxJumpTableRelaxIters,
		MaxSizeInlineBodyBytes: maxSizeInlineBodyBytes,
	}
}
