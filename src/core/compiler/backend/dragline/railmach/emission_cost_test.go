package railmach

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railssa"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestAllocatedEmissionCostAccountsForFoldedLoadAndSpills(t *testing.T) {
	f := &Func{Target: TargetAMD64,
		Insts:    []Inst{{Op: wasm.InstrI32Const, Result: 1}, {Op: wasm.InstrF64Load, Result: 2, OperandCount: 1}, {Op: wasm.InstrF64Add, Result: 4, OperandStart: 1, OperandCount: 2}},
		Operands: []Operand{{Reg: 1}, {Reg: 2}, {Reg: 3}},
		VRegs:    []VRegData{{}, {Type: TypeI32, Bank: BankGPR}, {Type: TypeF64, Bank: BankFPR}, {Type: TypeF64, Bank: BankFPR}, {Type: TypeF64, Bank: BankFPR}},
		Blocks:   []Block{{InstCount: 1, Weight: 1}, {InstStart: 1, InstCount: 2, Weight: 8}},
		Edges:    []Edge{{From: 0, To: 1}},
	}
	selection := &SelectionPlan{Selections: make([]Selection, 3)}
	schedule := &Schedule{Order: []uint32{0, 1, 2}, verifyPosition: []uint32{0, 1, 2}, BlockOf: []railssa.BlockID{0, 1, 1}, BlockRanges: []MoveRange{{Count: 1}, {Start: 1, Count: 2}}}
	allocation := &GreedyAllocation{Allocation: Allocation{Locations: []Location{{}, {Kind: LocationRegister, Bank: BankGPR}, {Kind: LocationSpill, Bank: BankFPR}, {Kind: LocationSpill, Bank: BankFPR, Index: 1}, {Kind: LocationRegister, Bank: BankFPR}}, InstructionPositions: []uint32{0, 1, 2}}}
	exit := &SSAExit{Moves: []PhysicalMove{{}}, EdgeMoves: []MoveRange{{Count: 1}}}
	postRA := &PostRAPlan{}
	baseline := ScoreAllocatedEmission(ScheduleScore{}, f, selection, schedule, allocation, exit, postRA)
	if baseline.NativeResourceCost != 42 {
		t.Fatalf("unfolded resources=%d, want 42", baseline.NativeResourceCost)
	}
	postRA.Rewrites = []Rewrite{{First: 1, Second: 2, Kind: RewriteAMD64MemoryFold}}
	folded := ScoreAllocatedEmission(ScheduleScore{}, f, selection, schedule, allocation, exit, postRA)
	if folded.NativeResourceCost != 18 {
		t.Fatalf("folded resources=%d, want 18", folded.NativeResourceCost)
	}
	if again := ScoreAllocatedEmission(ScheduleScore{}, f, selection, schedule, allocation, exit, postRA); again != folded {
		t.Fatalf("score accumulated scratch: %+v / %+v", folded, again)
	}

	// A store/load-forwarded load is not also realized as a memory fold.
	postRA.Rewrites = append(postRA.Rewrites, Rewrite{First: 0, Second: 1, Kind: RewriteLoadStoreForward})
	if conflict := ScoreAllocatedEmission(ScheduleScore{}, f, selection, schedule, allocation, exit, postRA); conflict.NativeResourceCost != baseline.NativeResourceCost {
		t.Fatalf("credited an unrealizable fold: %+v", conflict)
	}
	postRA.Rewrites = postRA.Rewrites[:1]
	allocation.Fragments = []AllocationFragment{{Reg: 3, Start: 14, End: 14, Location: Location{Kind: LocationRegister, Bank: BankFPR, Index: 1}}}
	regional := ScoreAllocatedEmission(ScheduleScore{}, f, selection, schedule, allocation, exit, postRA)
	if regional.NativeResourceCost != folded.NativeResourceCost {
		t.Fatalf("single-use region must exchange a use reload for an entry reload: %+v", regional)
	}
	allocation.Fragments[0].Victim = 4
	if victim := ScoreAllocatedEmission(ScheduleScore{}, f, selection, schedule, allocation, exit, postRA); victim.NativeResourceCost != folded.NativeResourceCost+16 {
		t.Fatalf("victim save/restore not counted: %+v", victim)
	}
}
