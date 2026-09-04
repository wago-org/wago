package shared

// QualityDebtReport is the backend-neutral Railshot signal used to prioritize
// future Dragline recompilation. It records emitted traffic, not an estimated
// speedup, and is never a correctness or routing decision by itself.
type QualityDebtReport struct {
	Functions []FunctionQualityDebt
}

type FunctionQualityDebt struct {
	Function uint32

	FrameBytes        uint32
	Spills            uint32
	Reloads           uint32
	Flushes           uint32
	BoundsChecks      uint32
	CallShuffles      uint32
	HelperTransitions uint32
	NativeBytes       uint32
}

// Total returns a saturating-free aggregate for reporting and prioritization.
// Individual compiler counters are ints, while practical module totals fit in
// uint64 even for adversarially large validated modules.
func (r QualityDebtReport) Total() FunctionQualityDebt {
	var out FunctionQualityDebt
	for _, function := range r.Functions {
		out.FrameBytes += function.FrameBytes
		out.Spills += function.Spills
		out.Reloads += function.Reloads
		out.Flushes += function.Flushes
		out.BoundsChecks += function.BoundsChecks
		out.CallShuffles += function.CallShuffles
		out.HelperTransitions += function.HelperTransitions
		out.NativeBytes += function.NativeBytes
	}
	return out
}

// HelperTransitionCount counts call classes that leave the cheap same-engine
// direct-call path. The keys are the stable shared Call* names.
func HelperTransitionCount(calls map[string]int) int {
	return calls[CallHost] + calls[CallHostSync] + calls[CallCrossInstance] + calls[CallImportDispatch] + calls[CallWrapper]
}
