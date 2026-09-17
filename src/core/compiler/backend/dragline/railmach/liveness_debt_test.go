package railmach

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railssa"
)

func TestMeasureLivenessDebtFindsBranchLayoutHole(t *testing.T) {
	f := &Func{
		Target: TargetARM64,
		Insts: []Inst{
			{Op: 1, Result: 1},
			{Op: 1, Result: 2},
			{Op: 1, Result: 3, OperandStart: 0, OperandCount: 2},
		},
		Operands: []Operand{{Reg: 1, Bank: BankGPR}, {Reg: 1, Bank: BankGPR}},
		VRegs: []VRegData{
			{},
			{Type: TypeI64, Bank: BankGPR},
			{Type: TypeI64, Bank: BankGPR},
			{Type: TypeI64, Bank: BankGPR},
		},
		Blocks: []Block{
			{InstStart: 0, InstCount: 1},
			{InstStart: 1, InstCount: 1},
			{InstStart: 2, InstCount: 1},
		},
		Edges: []Edge{{From: 0, To: 1}, {From: 0, To: 2}},
	}
	schedule := &Schedule{
		Order:       []uint32{0, 1, 2},
		BlockRanges: []MoveRange{{Start: 0, Count: 1}, {Start: 1, Count: 1}, {Start: 2, Count: 1}},
		BlockOf:     []railssa.BlockID{0, 1, 2},
	}
	allocation := &GreedyAllocation{Allocation: Allocation{Intervals: []LiveInterval{
		{Reg: 1, Start: 2, End: 14, Weight: 8, Bank: BankGPR},
		{Reg: 2, Start: 8, End: 10, Weight: 4, Bank: BankGPR},
	}}}
	debt, err := MeasureLivenessDebt(f, schedule, allocation)
	if err != nil {
		t.Fatal(err)
	}
	if debt.ValuesWithHoles != 1 || debt.TotalSegments != 3 || debt.FalseLivePositions != 6 ||
		debt.FalseInterferencePairs != 1 || debt.WeightedFalseInterference != 4 ||
		debt.PeakCurrentGPR != 2 || debt.PeakSegmentedGPR != 1 {
		t.Fatalf("liveness debt = %#v", debt)
	}
}
