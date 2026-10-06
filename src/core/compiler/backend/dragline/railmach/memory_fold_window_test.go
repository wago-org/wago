package railmach

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railssa"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestAMD64FoldedAddressSurvivesWindow(t *testing.T) {
	for _, name := range []string{"register", "dies", "hole", "overwrite", "fragment", "constant", "affine", "spill"} {
		t.Run(name, func(t *testing.T) {
			f := &Func{
				Target: TargetAMD64,
				Insts: []Inst{
					{Op: wasm.InstrI32Const, Result: 1},
					{Op: wasm.InstrF64Load, Result: 2, OperandCount: 1},
					{Op: wasm.InstrI32Const, Result: 3},
					{Op: wasm.InstrF64Add, Result: 4},
				},
				Operands: []Operand{{Reg: 1}},
				VRegs: []VRegData{{},
					{Type: TypeI32, Bank: BankGPR, Def: 3},
					{Type: TypeF64, Bank: BankFPR, Def: 9},
					{Type: TypeI32, Bank: BankGPR, Def: 15},
					{Type: TypeF64, Bank: BankFPR, Def: 21}},
			}
			schedule := &Schedule{Order: []uint32{0, 1, 2, 3}, BlockOf: make([]railssa.BlockID, 4)}
			address := Location{Kind: LocationRegister, Bank: BankGPR}
			allocation := &GreedyAllocation{Allocation: Allocation{
				Locations:            []Location{{}, address, {Kind: LocationRegister, Bank: BankFPR}, {Kind: LocationRegister, Bank: BankGPR, Index: 1}, {Kind: LocationRegister, Bank: BankFPR, Index: 1}},
				InstructionPositions: []uint32{0, 1, 2, 3},
				Intervals:            []LiveInterval{{Reg: 1, Start: 3, End: 23}},
			}}
			want := name == "register" || name == "constant" || name == "spill"
			switch name {
			case "dies":
				allocation.Intervals[0].End = 8
			case "hole":
				allocation.Intervals[0].Flags = liveIntervalSegmented
				allocation.LiveSegmentRanges = []LiveSegmentRange{{Reg: 1, SegmentCount: 2}}
				allocation.LiveSegments = []LiveSegment{{Start: 3, End: 8}, {Start: 20, End: 23}}
			case "overwrite":
				allocation.Locations[3] = address
			case "fragment":
				allocation.Fragments = []AllocationFragment{{Reg: 3, Start: 14, End: 17, Location: address, Victim: 1}}
			case "constant", "affine":
				allocation.Locations[1].Kind = LocationRematerialize
				if name == "affine" {
					f.Insts[0].Op = wasm.InstrI32Add
				}
			case "spill":
				allocation.Locations[1].Kind = LocationSpill
			}
			if got := AMD64FoldedAddressSurvivesWindow(f, schedule, allocation, 1, 3); got != want {
				t.Fatalf("survives = %v, want %v", got, want)
			}
		})
	}
}
