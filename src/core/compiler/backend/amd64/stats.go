package amd64

// CodegenStats accumulates backend code-generation counters. It is opt-in: set
// CompileOptions.Stats to a non-nil value to collect. The default compile path
// leaves it nil, and every helper below is a no-op on a nil receiver, so
// instrumentation costs nothing when stats are not requested.
//
// Counters are running totals across every function compiled with the same
// CompileOptions, with Functions tracking how many fed the bucket — divide by it
// for the per-function ratios that drive single-pass tuning: spills/fn,
// flushes/fn, flush-slots/fn, bounds-checks/fn, bytes/fn, and the call counts.
//
// This is a deliberately small first cut. The chokepoints instrumented here are
// the ones with a single, unambiguous emit site; finer counters (constant folds,
// local/tee peepholes) can be added as those paths gain dedicated helpers.
type CodegenStats struct {
	Functions          int // functions compiled into this bucket
	BytesEmitted       int // machine-code bytes across function bodies (excludes inter-function padding)
	Spills             int // register values forced to their canonical frame slot
	Reloads            int // spilled values reloaded from a frame slot
	Flushes            int // flush() calls (operand stack materialized at a control boundary)
	FlushSlots         int // total stack entries materialized across all flushes
	BoundsChecks       int // inline linear-memory bounds checks emitted
	BoundsChecksElided int // memory accesses with the check elided (guard-page mode)
	CompareFusions     int // comparisons fused into a following if/br_if/select (no setcc)
	DirectCalls        int // direct internal calls (call)
	IndirectCalls      int // call_indirect
	HostCalls          int // imported host calls
	PinnedLocals       int // integer locals pinned to registers
	PinnedGlobals      int // mutable integer globals pinned to registers
	MaxStackDepth      int // deepest symbolic operand stack seen across functions
}

func (s *CodegenStats) noteSpill() {
	if s != nil {
		s.Spills++
	}
}

func (s *CodegenStats) noteReload() {
	if s != nil {
		s.Reloads++
	}
}

func (s *CodegenStats) noteFlush(slots int) {
	if s != nil {
		s.Flushes++
		s.FlushSlots += slots
	}
}

func (s *CodegenStats) noteBoundsCheck() {
	if s != nil {
		s.BoundsChecks++
	}
}

func (s *CodegenStats) noteBoundsElided() {
	if s != nil {
		s.BoundsChecksElided++
	}
}

func (s *CodegenStats) noteCompareFusion() {
	if s != nil {
		s.CompareFusions++
	}
}

func (s *CodegenStats) noteDirectCall() {
	if s != nil {
		s.DirectCalls++
	}
}

func (s *CodegenStats) noteIndirectCall() {
	if s != nil {
		s.IndirectCalls++
	}
}

func (s *CodegenStats) noteHostCall() {
	if s != nil {
		s.HostCalls++
	}
}

// noteFunction records one compiled function's per-function totals.
func (s *CodegenStats) noteFunction(bytes, maxDepth, pinnedLocals, pinnedGlobals int) {
	if s == nil {
		return
	}
	s.Functions++
	s.BytesEmitted += bytes
	s.PinnedLocals += pinnedLocals
	s.PinnedGlobals += pinnedGlobals
	if maxDepth > s.MaxStackDepth {
		s.MaxStackDepth = maxDepth
	}
}
