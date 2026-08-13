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
	Objective OptimizationObjective
	Selection optimization.Selection

	FunctionAlignLog2 uint8
	InternalAlignLog2 uint8
	LoopAlignLog2     uint8

	InlineGrowthBudget int32
	MaxMachineWindow   uint8
	MaxRelaxIterations uint8
}

func (p CodegenPolicy) Enabled(name string) bool { return p.Selection.Enabled(name) }

// DefaultCodegenPolicy preserves current Balanced behavior. Objective-specific
// layout decisions are introduced by later measured changes.
func DefaultCodegenPolicy(selection optimization.Selection) CodegenPolicy {
	return CodegenPolicy{
		Objective:          OptimizeBalanced,
		Selection:          selection,
		FunctionAlignLog2:  4,
		InternalAlignLog2:  4,
		LoopAlignLog2:      4,
		MaxMachineWindow:   24,
		MaxRelaxIterations: 8,
	}
}
