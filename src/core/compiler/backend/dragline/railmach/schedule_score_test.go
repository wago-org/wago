package railmach

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railssa"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestScheduleScoreOrdersCompleteBackendDebt(t *testing.T) {
	base := ScheduleScore{Kind: ScheduleKindSourceStable, PhysicalCopies: 2}
	if !(ScheduleScore{Kind: ScheduleKindPressure, PhysicalCopies: 99, WeightedSpillDebt: 0}).BetterThan(ScheduleScore{Kind: ScheduleKindSourceStable, WeightedSpillDebt: 1}) {
		t.Fatal("spill debt was not the primary schedule criterion")
	}
	if !(ScheduleScore{Kind: ScheduleKindPressure, PhysicalCopies: 1}).BetterThan(base) {
		t.Fatal("physical copy debt did not break an allocation tie")
	}
	if (ScheduleScore{Kind: ScheduleKindPressure, PhysicalCopies: 2}).BetterThan(base) {
		t.Fatal("stable schedule kind did not break an exact quality tie")
	}
	if !(ScheduleScore{Kind: ScheduleKindPressure, PhysicalCopies: 2, CopyMotion: 1}).BetterThan(base) {
		t.Fatal("copy motion did not break an equal-debt tie")
	}
}

func TestScheduleScoreParetoFrontier(t *testing.T) {
	scores := []ScheduleScore{
		{Kind: ScheduleKindSourceStable, EstimatedCycles: 12, ResourceCycles: 8, SelectedBytes: 16, WeightedSpillDebt: 3},
		{Kind: ScheduleKindLatencyFusion, EstimatedCycles: 10, ResourceCycles: 8, SelectedBytes: 16, WeightedSpillDebt: 3},
		{Kind: ScheduleKindPressure, EstimatedCycles: 11, ResourceCycles: 8, SelectedBytes: 16, WeightedSpillDebt: 2},
	}
	if scores[0].Dominates(scores[1]) || !scores[1].Dominates(scores[0]) {
		t.Fatal("cycle improvement did not dominate otherwise equal candidate")
	}
	if scores[1].Dominates(scores[2]) || scores[2].Dominates(scores[1]) {
		t.Fatal("execution/spill tradeoff was incorrectly dominated")
	}
	if got, want := ScheduleFrontier(scores), uint64(0b110); got != want {
		t.Fatalf("frontier = %03b, want %03b", got, want)
	}
}

func TestScheduleScoreParetoFrontierKeepsEqualCandidates(t *testing.T) {
	scores := []ScheduleScore{{Kind: ScheduleKindSourceStable}, {Kind: ScheduleKindLatencyFusion}}
	if got, want := ScheduleFrontier(scores), uint64(0b11); got != want {
		t.Fatalf("equal frontier = %02b, want %02b", got, want)
	}
}

func TestScorePostRAOpportunitiesCountsOnlyPlannedInstructionElisions(t *testing.T) {
	schedule := &Schedule{
		Order:          []uint32{0, 1, 2, 3, 4, 5, 6, 7, 8, 9},
		verifyPosition: []uint32{0, 1, 2, 3, 4, 5, 6, 7, 8, 9},
	}
	postRA := &PostRAPlan{
		Rewrites: []Rewrite{
			{First: 0, Second: 1, Kind: RewriteARM64Pair},
			{First: 2, Second: 5, Kind: RewriteARM64RepeatedAdd},
			{First: 6, Second: 9, Kind: RewriteARM64ByteWiden},
			{First: 9, Second: ^uint32(0), Kind: RewriteARM64PrePostIndex},
		},
		WrapSpills:      []uint32{3, 7},
		EliminatedMoves: 4,
	}
	score := ScorePostRAOpportunities(ScheduleScore{}, schedule, postRA)
	if score.PostRARewrites != 4 || score.PostRAElisions != 6 || score.PostRAWrapSpills != 2 || score.EliminatedMoves != 4 {
		t.Fatalf("post-RA opportunity score = %#v", score)
	}
}

func TestScheduleScoreFrontierPreservesPostRATradeoff(t *testing.T) {
	fast := ScheduleScore{EstimatedCycles: 10, ResourceCycles: 8, SelectedBytes: 16}
	rewrite := ScheduleScore{EstimatedCycles: 11, ResourceCycles: 8, SelectedBytes: 16, PostRAElisions: 1}
	if fast.Dominates(rewrite) || rewrite.Dominates(fast) {
		t.Fatal("execution/post-RA tradeoff was incorrectly dominated")
	}
	if got, want := ScheduleFrontier([]ScheduleScore{fast, rewrite}), uint64(0b11); got != want {
		t.Fatalf("post-RA frontier = %02b, want %02b", got, want)
	}
}

func TestEstimateScheduleCostDistinguishesLatencyExposure(t *testing.T) {
	f := &Func{
		Target: TargetARM64,
		Insts:  []Inst{{Op: wasm.InstrI64Mul}, {Op: wasm.InstrI64Add}, {Op: wasm.InstrI64Add}},
		Blocks: []Block{{InstCount: 3, Weight: 2}},
	}
	selection := &SelectionPlan{Selections: []Selection{
		{Cost: SelectCost{Latency: 5, ResourceCost: 1, Bytes: 4}},
		{Cost: SelectCost{Latency: 1, ResourceCost: 1, Bytes: 4}},
		{Cost: SelectCost{Latency: 1, ResourceCost: 1, Bytes: 4}},
	}}
	dag := &DependencyDAG{Offsets: []uint32{0, 0, 0, 1}, Dependencies: []Dependency{{Instruction: 0, Kind: DependencyData}}}
	makeSchedule := func(order []uint32) *Schedule {
		position := make([]uint32, len(order))
		for index, instruction := range order {
			position[instruction] = uint32(index)
		}
		return &Schedule{Order: order, BlockRanges: []MoveRange{{Count: 3}}, verifyPosition: position, criticalHeight: make([]uint64, 3)}
	}
	fastCycles, fastResources, fastBytes := estimateScheduleCost(f, selection, dag, makeSchedule([]uint32{0, 1, 2}))
	slowCycles, slowResources, slowBytes := estimateScheduleCost(f, selection, dag, makeSchedule([]uint32{1, 0, 2}))
	if fastCycles != 12 || slowCycles != 14 {
		t.Fatalf("estimated cycles = %d/%d, want 12/14", fastCycles, slowCycles)
	}
	if fastResources != 6 || slowResources != 6 || fastBytes != 12 || slowBytes != 12 {
		t.Fatalf("resource/byte costs = %d/%d and %d/%d, want 6/6 and 12/12", fastResources, slowResources, fastBytes, slowBytes)
	}
}

func TestScoreScheduleCandidateValidatesCompleteCandidate(t *testing.T) {
	m, selection, _, dag := buildScheduleTest(t, TargetAMD64, machineModule(nil, nil, []byte{0x41, 0x00, 0x04, 0x40, 0x0b, 0x0b}))
	schedule, err := BuildSchedule(m, selection, dag, ScheduleKindSourceStable, nil)
	if err != nil {
		t.Fatal(err)
	}
	allocation, err := AllocateGreedyPForSchedule(m, schedule, DefaultGreedyConfig(TargetAMD64), nil)
	if err != nil {
		t.Fatal(err)
	}
	exit, err := LateSSAExit(m, &allocation.Allocation, nil)
	if err != nil {
		t.Fatal(err)
	}
	score, err := ScoreScheduleCandidate(m, selection, dag, schedule, allocation, exit)
	if err != nil {
		t.Fatal(err)
	}
	if score.Kind != ScheduleKindSourceStable {
		t.Fatalf("score = %#v", score)
	}
}

func TestLatencyPriorityOrdersLastUseBeforeCriticalHeight(t *testing.T) {
	f := &Func{
		Target: TargetARM64,
		VRegs:  []VRegData{{}, {Type: TypeI64, Bank: BankGPR}, {Type: TypeI64, Bank: BankGPR}, {Type: TypeI64, Bank: BankGPR}, {Type: TypeI64, Bank: BankGPR}},
		Insts: []Inst{
			{OperandStart: 0, OperandCount: 1, Result: 2, Op: wasm.InstrI64Add},
			{OperandStart: 1, OperandCount: 1, Result: 3, Op: wasm.InstrI64Mul},
		},
		Operands: []Operand{{Reg: 1, Bank: BankGPR}, {Reg: 4, Bank: BankGPR}},
	}
	selection := &SelectionPlan{Selections: []Selection{
		{Cost: SelectCost{Latency: 1, ResourceCost: 1}},
		{Cost: SelectCost{Latency: 8, ResourceCost: 4}},
	}}
	remaining := []uint32{0, 1, 0, 0, 2}
	heights := []uint64{62, 64}
	resultCounts := []uint8{1, 1}
	if first, second := latencySchedulePriority(f, selection, 0, remaining, heights, resultCounts, 2), latencySchedulePriority(f, selection, 1, remaining, heights, resultCounts, 2); first <= second {
		t.Fatalf("last-use priority %d did not beat critical-path priority %d", first, second)
	}
	remaining[1] = 2
	if first, second := latencySchedulePriority(f, selection, 0, remaining, heights, resultCounts, 2), latencySchedulePriority(f, selection, 1, remaining, heights, resultCounts, 2); first >= second {
		t.Fatalf("critical-path priority %d did not beat local priority %d", second, first)
	}
}

func TestScheduleLastUseHeightCreditTracksRegisterPressure(t *testing.T) {
	capacity := DefaultLinearQConfig(TargetARM64)
	pressure := &railssa.PressurePlan{Blocks: []railssa.BlockPressure{{}}}
	if got := scheduleLastUseHeightCredit(TargetARM64, pressure, 0); got != 0 {
		t.Fatalf("low-pressure credit = %d, want 0", got)
	}
	pressure.Blocks[0].PeakFPR = uint16(capacity.FPRs)
	if got := scheduleLastUseHeightCredit(TargetARM64, pressure, 0); got != 2 {
		t.Fatalf("at-capacity FPR credit = %d, want 2", got)
	}
	pressure.Blocks[0].PeakFPR = 0
	pressure.Blocks[0].PeakGPR = uint16(capacity.GPRs) + (uint16(capacity.GPRs)*3+3)/4
	if got := scheduleLastUseHeightCredit(TargetARM64, pressure, 0); got != 3 {
		t.Fatalf("high-pressure GPR credit = %d, want 3", got)
	}
}
