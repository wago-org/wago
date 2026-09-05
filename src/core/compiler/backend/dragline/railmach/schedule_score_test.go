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
	if first, second := schedulePriority(f, selection, 0, ScheduleKindLatencyFusion, remaining, heights, 2), schedulePriority(f, selection, 1, ScheduleKindLatencyFusion, remaining, heights, 2); first <= second {
		t.Fatalf("last-use priority %d did not beat critical-path priority %d", first, second)
	}
	remaining[1] = 2
	if first, second := schedulePriority(f, selection, 0, ScheduleKindLatencyFusion, remaining, heights, 2), schedulePriority(f, selection, 1, ScheduleKindLatencyFusion, remaining, heights, 2); first >= second {
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
