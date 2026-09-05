package railmach

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestResolveParallelBreaksRegisterCycle(t *testing.T) {
	left := Location{Kind: LocationRegister, Bank: BankGPR, Index: 0}
	right := Location{Kind: LocationRegister, Bank: BankGPR, Index: 1}
	copies := []pendingCopy{{src: right, dst: left, reg: 1}, {src: left, dst: right, reg: 2}}
	var moves []PhysicalMove
	var debt CopyDebt
	if err := resolveParallel(&moves, copies, 3, 0, PlaceSplitEdge, &debt); err != nil {
		t.Fatal(err)
	}
	if len(moves) != 3 || moves[0].Kind != MoveSaveTemporary || moves[1].Kind != MoveCopy || moves[2].Kind != MoveRestoreTemporary || debt.Cycles != 1 {
		t.Fatalf("cycle moves=%#v debt=%#v", moves, debt)
	}
}

func TestResolveParallelKeepsCycleTemporaryAcrossSpillCopy(t *testing.T) {
	spill0 := Location{Kind: LocationSpill, Bank: BankGPR, Index: 0}
	spill1 := Location{Kind: LocationSpill, Bank: BankGPR, Index: 1}
	spill2 := Location{Kind: LocationSpill, Bank: BankGPR, Index: 2}
	copies := []pendingCopy{{src: spill1, dst: spill0, reg: 1}, {src: spill2, dst: spill1, reg: 2}, {src: spill0, dst: spill2, reg: 3}}
	var moves []PhysicalMove
	var debt CopyDebt
	if err := resolveParallel(&moves, copies, 3, 0, PlaceSplitEdge, &debt); err != nil {
		t.Fatal(err)
	}
	if len(moves) != 6 || moves[0].Kind != MoveSaveTemporary || moves[0].Temporary != 0 || moves[1].Temporary != 1 || moves[2].Temporary != 1 || moves[len(moves)-1].Kind != MoveRestoreTemporary || moves[len(moves)-1].Temporary != 0 {
		t.Fatalf("spill cycle moves=%#v debt=%#v", moves, debt)
	}
}

func TestEdgeOrderedTransferSplit(t *testing.T) {
	for _, test := range []struct {
		name  string
		edges []uint32
		split int
		ok    bool
	}{
		{name: "one_run", edges: []uint32{0, 0, 2, 3}, split: 4, ok: true},
		{name: "stack_then_locals", edges: []uint32{0, 2, 3, 0, 1, 3}, split: 3, ok: true},
		{name: "arbitrary", edges: []uint32{1, 0, 2, 0}, ok: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			transfers := make([]EdgeTransfer, len(test.edges))
			for index, edge := range test.edges {
				transfers[index].Edge = edge
			}
			split, ok := edgeOrderedTransferSplit(transfers)
			if split != test.split || ok != test.ok {
				t.Fatalf("split=%d ok=%t, want %d/%t", split, ok, test.split, test.ok)
			}
		})
	}
}

func TestLateSSAExitPlacesIfResultCopies(t *testing.T) {
	m := machineModule([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, []byte{
		0x20, 0x00,
		0x04, 0x7f,
		0x41, 0x01,
		0x05,
		0x41, 0x02,
		0x0b,
		0x0b,
	})
	f := buildMachineTest(t, TargetARM64, m)
	allocation, err := AllocateLinearQ(f, DefaultLinearQConfig(TargetARM64), nil)
	if err != nil {
		t.Fatal(err)
	}
	exit, err := LateSSAExit(f, allocation, nil)
	if err != nil {
		t.Fatal(err)
	}
	if exit.Debt.Requested != uint32(len(f.Transfers)) || exit.Debt.Requested != exit.Debt.Coalesced+uint32(countNonemptyEdgeCopies(exit)) {
		t.Fatalf("copy debt=%#v transfers=%d moves=%#v", exit.Debt, len(f.Transfers), exit.Moves)
	}
	for _, move := range exit.Moves {
		if move.Placement == PlaceInvalid {
			t.Fatalf("unplaced move %#v", move)
		}
	}
}

func TestLateSSAExitMotionsCopiesIntoColderSuccessor(t *testing.T) {
	f := &Func{
		Target: TargetARM64,
		Insts:  []Inst{{Op: wasm.InstrBr}, {Op: wasm.InstrReturn}},
		VRegs: []VRegData{
			{},
			{Bank: BankGPR, Type: TypeI32, Flags: VRegInitial},
			{Bank: BankGPR, Type: TypeI32, Flags: VRegBlockParam},
			{Bank: BankGPR, Type: TypeI32, Flags: VRegInitial},
			{Bank: BankGPR, Type: TypeI32, Flags: VRegBlockParam},
		},
		Blocks: []Block{{InstStart: 0, InstCount: 1, Weight: 100}, {InstStart: 1, InstCount: 1, Weight: 1}},
		Edges:  []Edge{{From: 0, To: 1}},
		Transfers: []EdgeTransfer{{
			Src: 1, Dst: 2, Edge: 0, From: 0, To: 1, Weight: 1, Type: TypeI32,
		}, {
			Src: 3, Dst: 4, Edge: 0, From: 0, To: 1, Weight: 1, Type: TypeI32,
		}},
	}
	allocation := &Allocation{
		Locations: []Location{
			{},
			{Kind: LocationRegister, Bank: BankGPR, Index: 0},
			{Kind: LocationRegister, Bank: BankGPR, Index: 1},
			{Kind: LocationRegister, Bank: BankGPR, Index: 2},
			{Kind: LocationRegister, Bank: BankGPR, Index: 3},
		},
		Intervals: []LiveInterval{
			{Reg: 1, Start: 0, End: 6, Bank: BankGPR},
			{Reg: 2, Start: 6, End: 11, Bank: BankGPR},
			{Reg: 3, Start: 0, End: 6, Bank: BankGPR},
			{Reg: 4, Start: 6, End: 11, Bank: BankGPR},
		},
		InstructionPositions: []uint32{0, 1},
	}
	exit, err := LateSSAExit(f, allocation, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(exit.Moves) != 2 || exit.Moves[0].Placement != PlaceSuccessorStart || exit.Moves[1].Placement != PlaceSuccessorStart || exit.Debt.Motion != 2 {
		t.Fatalf("motion moves=%#v debt=%#v", exit.Moves, exit.Debt)
	}
	exit.Moves[1].Placement = PlacePredecessorEnd
	if err := VerifySSAExit(f, allocation, exit); err == nil {
		t.Fatal("mixed placement in a parallel edge bundle was accepted")
	}
}

func TestLateSSAExitPreservesV128EdgeTransfer(t *testing.T) {
	f := &Func{
		Target: TargetARM64,
		Insts:  []Inst{{Op: wasm.InstrBr}, {Op: wasm.InstrReturn}},
		VRegs: []VRegData{
			{},
			{Bank: BankFPR, Type: TypeV128, Flags: VRegInitial},
			{Bank: BankFPR, Type: TypeV128, Flags: VRegBlockParam},
		},
		Blocks: []Block{{InstStart: 0, InstCount: 1, Weight: 10}, {InstStart: 1, InstCount: 1, Weight: 1}},
		Edges:  []Edge{{From: 0, To: 1}},
		Transfers: []EdgeTransfer{{
			Src: 1, Dst: 2, Edge: 0, From: 0, To: 1, Weight: 1, Type: TypeV128,
		}},
	}
	allocation := &Allocation{
		Locations: []Location{
			{},
			{Kind: LocationRegister, Bank: BankFPR, Index: 0},
			{Kind: LocationSpill, Bank: BankFPR, Index: 0},
		},
		Intervals: []LiveInterval{
			{Reg: 1, Start: 0, End: 6, Bank: BankFPR},
			{Reg: 2, Start: 6, End: 11, Bank: BankFPR},
		},
		InstructionPositions: []uint32{0, 1},
		SpillSlots:           2,
		FrameBytes:           16,
	}
	exit, err := LateSSAExit(f, allocation, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(exit.Moves) != 1 || exit.Moves[0].Reg != 1 || exit.Moves[0].Bank != BankFPR || f.VRegs[exit.Moves[0].Reg].Type != TypeV128 {
		t.Fatalf("vector edge moves = %#v", exit.Moves)
	}
}

func countNonemptyEdgeCopies(exit *SSAExit) int {
	count := 0
	for _, moves := range exit.EdgeMoves {
		if moves.Count != 0 {
			count++
		}
	}
	return count
}
