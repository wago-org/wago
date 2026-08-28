package railmach

import "fmt"

// ScheduleScore is a deterministic, target-neutral quality ordering computed
// only after a candidate has a complete allocation and late SSA exit. Fields
// are compared lexicographically in the order shown by BetterThan.
type ScheduleScore struct {
	WeightedSpillDebt uint64
	PhysicalCopies    uint32
	CopyCycles        uint32
	CopyMotion        uint32
	FixedRepairs      uint32
	BrokenFusions     uint32
	Kind              ScheduleKind
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
	return scoreVerifiedScheduleCandidate(f, selection, schedule, allocation, exit), nil
}

// ScoreVerifiedScheduleCandidate scores products returned by the verified
// schedule, allocator, and SSA-exit builders without replaying those complete
// verifiers at the immediately adjacent scoring boundary.
func ScoreVerifiedScheduleCandidate(f *Func, selection *SelectionPlan, schedule *Schedule, allocation *GreedyAllocation, exit *SSAExit) (ScheduleScore, error) {
	if f == nil || selection == nil || schedule == nil || allocation == nil || exit == nil || len(selection.Selections) != len(f.Insts) || len(schedule.Order) != len(f.Insts) || schedule.Kind < ScheduleKindSourceStable || schedule.Kind > ScheduleKindPressure {
		return ScheduleScore{}, fmt.Errorf("railmach: verified schedule score requires complete products")
	}
	return scoreVerifiedScheduleCandidate(f, selection, schedule, allocation, exit), nil
}

func scoreVerifiedScheduleCandidate(f *Func, selection *SelectionPlan, schedule *Schedule, allocation *GreedyAllocation, exit *SSAExit) ScheduleScore {
	// Schedule verification just reconstructed this exact inverse permutation.
	position := schedule.verifyPosition
	brokenFusions := uint32(0)
	for _, combination := range selection.Combinations {
		if combination.Kind == CombineCompareBranch && combination.Producer != ^uint32(0) && position[combination.Consumer] != position[combination.Producer]+1 {
			brokenFusions++
		}
	}
	return ScheduleScore{
		WeightedSpillDebt: allocation.Metrics.WeightedDebt,
		PhysicalCopies:    exit.Debt.Physical,
		CopyCycles:        exit.Debt.Cycles,
		CopyMotion:        exit.Debt.Motion,
		FixedRepairs:      uint32(len(allocation.FixedMoves)),
		BrokenFusions:     brokenFusions,
		Kind:              schedule.Kind,
	}
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
