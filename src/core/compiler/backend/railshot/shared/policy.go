package shared

import "github.com/wago-org/wago/src/core/compiler/optimization"

// CodegenPolicy is one immutable per-compilation policy. Selection captures all
// catalog flags in a compact bitset. The numeric fields bound later finalizer,
// layout, inlining, and machine-window decisions without package-global state.
type CodegenPolicy struct {
	Capabilities  Capabilities
	Selection     optimization.Selection
	CompactNative bool

	FunctionAlignLog2 uint8
	InternalAlignLog2 uint8
	LoopAlignLog2     uint8

	MaxMachineWindow          uint8
	MaxRelaxIterations        uint8
	MaxFinalizerDeletions     uint8
	MaxRel32Sites             uint16
	MaxLoopCompactionBytes    uint32
	MaxJumpTableBranches      uint8
	MaxJumpTableRelaxIters    uint8
	MaxCompactInlineBodyBytes uint8
}

func (p CodegenPolicy) Enabled(name string) bool { return p.Selection.Enabled(name) }
func (p CodegenPolicy) EnabledOption(option optimization.Option) bool {
	return p.Selection.EnabledOption(option)
}
func (p CodegenPolicy) Valid() bool { return p.Selection.Valid() }

// DefaultCodegenPolicy preserves Wago's ordinary performance-oriented codegen.
func DefaultCodegenPolicy(selection optimization.Selection) CodegenPolicy {
	return codegenPolicy(selection, false)
}

// CompactCodegenPolicy enables the bounded native-compaction path. It is an
// internal measurement and rollout surface, not a public optimization profile.
func CompactCodegenPolicy(selection optimization.Selection) CodegenPolicy {
	return codegenPolicy(selection, true)
}

func codegenPolicy(selection optimization.Selection, compact bool) CodegenPolicy {
	functionAlign, internalAlign, loopAlign := uint8(4), uint8(4), uint8(4)
	compactNative := false
	maxFinalizerDeletions := uint8(8)
	maxRel32Sites := uint16(256)
	maxLoopCompactionBytes := uint32(16 << 10)
	maxJumpTableBranches := uint8(0)
	maxJumpTableRelaxIters := uint8(0)
	maxCompactInlineBodyBytes := uint8(0)
	if compact {
		// Zero requests the target's minimum legal code alignment. Backends clamp
		// it to their instruction/data requirements.
		functionAlign, internalAlign, loopAlign = 0, 0, 0
		maxCompactInlineBodyBytes = 16
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
		Capabilities:              CompiledCapabilities,
		Selection:                 selection,
		CompactNative:             compactNative,
		FunctionAlignLog2:         functionAlign,
		InternalAlignLog2:         internalAlign,
		LoopAlignLog2:             loopAlign,
		MaxMachineWindow:          24,
		MaxRelaxIterations:        8,
		MaxFinalizerDeletions:     maxFinalizerDeletions,
		MaxRel32Sites:             maxRel32Sites,
		MaxLoopCompactionBytes:    maxLoopCompactionBytes,
		MaxJumpTableBranches:      maxJumpTableBranches,
		MaxJumpTableRelaxIters:    maxJumpTableRelaxIters,
		MaxCompactInlineBodyBytes: maxCompactInlineBodyBytes,
	}
}
