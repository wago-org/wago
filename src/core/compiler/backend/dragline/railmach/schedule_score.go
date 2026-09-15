package railmach

import "fmt"

// ScheduleScore is a deterministic, target-neutral quality vector computed
// after a candidate has a complete allocation and late SSA exit. Optional
// verifier-gated post-RA opportunity fields are attached by
// ScorePostRAOpportunities. BetterThan preserves the calibrated production debt
// ordering; Dominates exposes the independent dimensions for frontier analysis.
type ScheduleScore struct {
	EstimatedCycles   uint64
	ResourceCycles    uint64
	SelectedBytes     uint64
	PostRARewrites    uint32
	PostRAElisions    uint32
	PostRAWrapSpills  uint32
	EliminatedMoves   uint32
	WeightedSpillDebt uint64
	PhysicalCopies    uint32
	CopyCycles        uint32
	CopyMotion        uint32
	FixedRepairs      uint32
	BrokenFusions     uint32
	LoopInvariantOps  uint32
	Kind              ScheduleKind
}

// ScorePostRAOpportunities attaches verifier-gated target opportunities to a
// complete candidate score. Planned elisions are deliberately conservative:
// only rewrites whose normal realization removes selected instructions receive
// credit. Final emission still determines exact native bytes.
func ScorePostRAOpportunities(score ScheduleScore, schedule *Schedule, postRA *PostRAPlan) ScheduleScore {
	if schedule == nil || postRA == nil || len(schedule.verifyPosition) != len(schedule.Order) {
		return score
	}
	score.PostRARewrites = uint32(len(postRA.Rewrites))
	score.PostRAWrapSpills = uint32(len(postRA.WrapSpills))
	score.EliminatedMoves = postRA.EliminatedMoves
	for _, rewrite := range postRA.Rewrites {
		first, second := rewrite.First, rewrite.Second
		if int(first) >= len(schedule.verifyPosition) {
			continue
		}
		firstPosition := schedule.verifyPosition[first]
		secondPosition := firstPosition
		if second != ^uint32(0) {
			if int(second) >= len(schedule.verifyPosition) {
				continue
			}
			secondPosition = schedule.verifyPosition[second]
			if secondPosition < firstPosition {
				continue
			}
		}
		var elisions uint32
		switch rewrite.Kind {
		case RewriteARM64Pair, RewriteAMD64MemoryFold, RewriteARM64LogicalShift, RewriteARM64BitmaskPopcnt:
			elisions = 1
		case RewriteAMD64ByteSwap, RewriteARM64ByteSwap, RewriteARM64Narrow16To8, RewriteARM64RepeatedAdd:
			elisions = secondPosition - firstPosition
		case RewriteARM64ByteWiden:
			if secondPosition > firstPosition+1 {
				elisions = secondPosition - firstPosition - 1
			}
		}
		score.PostRAElisions = uint32(min(uint64(^uint32(0)), uint64(score.PostRAElisions)+uint64(elisions)))
	}
	return score
}

// ScoreScheduleCandidate measures debts that the backend must really encode.
// It deliberately avoids process-global target state and estimated IR-only
// pressure once an actual allocation is available.
func ScoreScheduleCandidate(f *Func, selection *SelectionPlan, dag *DependencyDAG, schedule *Schedule, allocation *GreedyAllocation, exit *SSAExit) (ScheduleScore, error) {
	if f == nil || selection == nil || dag == nil || schedule == nil || allocation == nil || exit == nil || len(selection.Selections) != len(f.Insts) || len(schedule.Order) != len(f.Insts) {
		return ScheduleScore{}, fmt.Errorf("railmach: schedule score requires a complete backend candidate")
	}
	if schedule.Kind < ScheduleKindSourceStable || schedule.Kind > ScheduleKindPressure {
		return ScheduleScore{}, fmt.Errorf("railmach: schedule score saw invalid kind %d", schedule.Kind)
	}
	if err := verifyScheduleReusingScratch(f, dag, schedule); err != nil {
		return ScheduleScore{}, err
	}
	if err := verifyAllocationReusingScratch(f, &allocation.Allocation, DefaultLinearQConfig(f.Target)); err != nil {
		return ScheduleScore{}, err
	}
	if err := VerifySSAExit(f, &allocation.Allocation, exit); err != nil {
		return ScheduleScore{}, err
	}
	return scoreVerifiedScheduleCandidate(f, selection, dag, schedule, allocation, exit), nil
}

// ScoreVerifiedScheduleCandidate scores products returned by the verified
// schedule, allocator, and SSA-exit builders without replaying those complete
// verifiers at the immediately adjacent scoring boundary.
func ScoreVerifiedScheduleCandidate(f *Func, selection *SelectionPlan, dag *DependencyDAG, schedule *Schedule, allocation *GreedyAllocation, exit *SSAExit) (ScheduleScore, error) {
	if f == nil || selection == nil || dag == nil || schedule == nil || allocation == nil || exit == nil ||
		len(selection.Selections) != len(f.Insts) || len(dag.Offsets) != len(f.Insts)+1 ||
		len(schedule.Order) != len(f.Insts) || len(schedule.BlockRanges) != len(f.Blocks) ||
		len(schedule.verifyPosition) != len(f.Insts) || len(schedule.criticalHeight) != len(f.Insts) ||
		schedule.Kind < ScheduleKindSourceStable || schedule.Kind > ScheduleKindPressure {
		return ScheduleScore{}, fmt.Errorf("railmach: verified schedule score requires complete products")
	}
	return scoreVerifiedScheduleCandidate(f, selection, dag, schedule, allocation, exit), nil
}

func scoreVerifiedScheduleCandidate(f *Func, selection *SelectionPlan, dag *DependencyDAG, schedule *Schedule, allocation *GreedyAllocation, exit *SSAExit) ScheduleScore {
	// Schedule verification just reconstructed this exact inverse permutation.
	position := schedule.verifyPosition
	brokenFusions := uint32(0)
	for _, combination := range selection.Combinations {
		if combination.Kind == CombineCompareBranch && combination.Producer != ^uint32(0) && position[combination.Consumer] != position[combination.Producer]+1 {
			brokenFusions++
		}
	}
	estimatedCycles, resourceCycles, selectedBytes := estimateScheduleCost(f, selection, dag, schedule)
	return ScheduleScore{
		EstimatedCycles:   estimatedCycles,
		ResourceCycles:    resourceCycles,
		SelectedBytes:     selectedBytes,
		WeightedSpillDebt: allocation.Metrics.WeightedDebt,
		PhysicalCopies:    exit.Debt.Physical,
		CopyCycles:        exit.Debt.Cycles,
		CopyMotion:        exit.Debt.Motion,
		FixedRepairs:      uint32(len(allocation.FixedMoves)),
		BrokenFusions:     brokenFusions,
		LoopInvariantOps:  schedule.CommittedLICM,
		Kind:              schedule.Kind,
	}
}

// estimateScheduleCost models one in-order issue stream per block. Dependency
// latency may overlap independent work, while selected uops occupy the issue
// stream. Block weights make loop scheduling visible without collapsing the
// independent execution, spill, copy, and byte dimensions into one sum.
// criticalHeight is scheduler-only scratch after construction, so reusing it
// here avoids retaining another instruction-sized candidate slab.
func estimateScheduleCost(f *Func, selection *SelectionPlan, dag *DependencyDAG, schedule *Schedule) (estimatedCycles, resourceCycles, selectedBytes uint64) {
	completion := schedule.criticalHeight
	clear(completion)
	for _, selected := range selection.Selections {
		selectedBytes = saturatingAdd(selectedBytes, uint64(selected.Cost.Bytes))
	}
	for blockID, blockRange := range schedule.BlockRanges {
		end := blockRange.Start + blockRange.Count
		issueAt, blockFinish, blockResources := uint64(0), uint64(0), uint64(0)
		for position := blockRange.Start; position < end; position++ {
			instructionID := schedule.Order[position]
			readyAt := uint64(0)
			for _, dependency := range dag.Dependencies[dag.Offsets[instructionID]:dag.Offsets[instructionID+1]] {
				dependencyPosition := schedule.verifyPosition[dependency.Instruction]
				if dependencyPosition >= blockRange.Start && dependencyPosition < end {
					readyAt = max(readyAt, completion[dependency.Instruction])
				}
			}
			selected := selection.Selections[instructionID]
			latency := uint64(scheduleInstructionLatency(f.Target, f.Insts[instructionID].Op, selected.Cost.Latency))
			resources := uint64(max(selected.Cost.ResourceCost, 1))
			start := max(issueAt, readyAt)
			finish := saturatingAdd(start, latency)
			completion[instructionID] = finish
			issueAt = saturatingAdd(start, resources)
			blockFinish = max(blockFinish, finish)
			blockResources = saturatingAdd(blockResources, resources)
		}
		weight := uint64(max(f.Blocks[blockID].Weight, 1))
		estimatedCycles = saturatingAdd(estimatedCycles, saturatingMultiply(blockFinish, weight))
		resourceCycles = saturatingAdd(resourceCycles, saturatingMultiply(blockResources, weight))
	}
	return estimatedCycles, resourceCycles, selectedBytes
}

func saturatingMultiply(a, b uint64) uint64 {
	if a != 0 && b > ^uint64(0)/a {
		return ^uint64(0)
	}
	return a * b
}

// Dominates reports whether s is no worse in every recorded quality dimension
// and strictly better in at least one. Kind is deliberately excluded: it is a
// deterministic policy tie-break, not machine-code quality. Callers must not
// use this alone for production selection until retry-complete post-RA
// opportunity and realized-byte costs are present as well.
func (s ScheduleScore) Dominates(other ScheduleScore) bool {
	noWorse := s.EstimatedCycles <= other.EstimatedCycles &&
		s.ResourceCycles <= other.ResourceCycles &&
		s.SelectedBytes <= other.SelectedBytes &&
		s.PostRAElisions >= other.PostRAElisions &&
		s.PostRAWrapSpills >= other.PostRAWrapSpills &&
		s.EliminatedMoves >= other.EliminatedMoves &&
		s.WeightedSpillDebt <= other.WeightedSpillDebt &&
		s.PhysicalCopies <= other.PhysicalCopies &&
		s.CopyCycles <= other.CopyCycles &&
		s.CopyMotion >= other.CopyMotion &&
		s.FixedRepairs <= other.FixedRepairs &&
		s.BrokenFusions <= other.BrokenFusions &&
		s.LoopInvariantOps >= other.LoopInvariantOps
	strictlyBetter := s.EstimatedCycles < other.EstimatedCycles ||
		s.ResourceCycles < other.ResourceCycles ||
		s.SelectedBytes < other.SelectedBytes ||
		s.PostRAElisions > other.PostRAElisions ||
		s.PostRAWrapSpills > other.PostRAWrapSpills ||
		s.EliminatedMoves > other.EliminatedMoves ||
		s.WeightedSpillDebt < other.WeightedSpillDebt ||
		s.PhysicalCopies < other.PhysicalCopies ||
		s.CopyCycles < other.CopyCycles ||
		s.CopyMotion > other.CopyMotion ||
		s.FixedRepairs < other.FixedRepairs ||
		s.BrokenFusions < other.BrokenFusions ||
		s.LoopInvariantOps > other.LoopInvariantOps
	return noWorse && strictlyBetter
}

// ScheduleFrontier returns one bit per candidate on the recorded frontier.
// Production currently evaluates at most three candidates, but the 64-bit
// result keeps this helper useful to offline scheduler experiments as well.
func ScheduleFrontier(scores []ScheduleScore) uint64 {
	count := min(len(scores), 64)
	var frontier uint64
	for candidate := range count {
		dominated := false
		for other := range count {
			if candidate != other && scores[other].Dominates(scores[candidate]) {
				dominated = true
				break
			}
		}
		if !dominated {
			frontier |= uint64(1) << candidate
		}
	}
	return frontier
}

// BetterThan reports whether s is preferred to other. Stable kind ordering is
// the final tie break, so equal candidates choose source-stable first.
func (s ScheduleScore) BetterThan(other ScheduleScore) bool {
	if s.WeightedSpillDebt != other.WeightedSpillDebt {
		return s.WeightedSpillDebt < other.WeightedSpillDebt
	}
	if s.CopyCycles != other.CopyCycles {
		return s.CopyCycles < other.CopyCycles
	}
	if s.PhysicalCopies != other.PhysicalCopies {
		return s.PhysicalCopies < other.PhysicalCopies
	}
	if s.CopyMotion != other.CopyMotion {
		return s.CopyMotion > other.CopyMotion
	}
	if s.FixedRepairs != other.FixedRepairs {
		return s.FixedRepairs < other.FixedRepairs
	}
	if s.BrokenFusions != other.BrokenFusions {
		return s.BrokenFusions < other.BrokenFusions
	}
	return s.Kind < other.Kind
}
