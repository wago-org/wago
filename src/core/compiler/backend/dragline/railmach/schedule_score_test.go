package railmach

import "testing"

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
