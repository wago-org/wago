package railmach

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railssa"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestAMD64CallSetupClobbersR12AcrossExactCallee(t *testing.T) {
	for _, count := range []uint16{7, 8, 10} {
		f := &Func{Target: TargetAMD64, Insts: []Inst{{Op: wasm.InstrCall, OperandCount: count}}}
		allocation := &Allocation{InstructionPositions: []uint32{1}}
		calls := collectCallPositions(f, allocation, nil)
		config := DefaultGreedyConfig(TargetAMD64)
		// Even a body with no clobbers cannot preserve the value that argument
		// staging overwrites before entering that body.
		config.CallClobbers = []CallClobber{{Instruction: 0}}
		survivors := allocationCallSurvivorMask(config, allocation, LiveInterval{Start: 1, End: 20, Bank: BankGPR}, calls)
		if got, want := survivors&(1<<9) != 0, count < 8; got != want {
			t.Errorf("%d arguments: R12 survives=%v, want %v", count, got, want)
		}
	}
}

func TestAMD64CallSetupIsSavedByEnclosingFunction(t *testing.T) {
	f := &Func{Target: TargetAMD64, Insts: []Inst{{Op: wasm.InstrCall, OperandCount: 8}}, VRegs: []VRegData{{}}}
	allocation := &GreedyAllocation{Allocation: Allocation{Locations: []Location{{}}}}
	metadata := &railssa.Metadata{Instructions: []railssa.InstructionMetadata{{Flags: railssa.EffectCall}}}
	contract, _, err := AnalyzeVerifiedABI(f, allocation, metadata, 0)
	if err != nil {
		t.Fatal(err)
	}
	if contract.GPRClobbers&(1<<9) == 0 || contract.CalleeGPRs&(1<<9) == 0 {
		t.Fatalf("argument setup omitted from enclosing preservation: %#v", contract)
	}
}

func TestAMD64CallSetupCannotEscapeShrinkWrappedSave(t *testing.T) {
	f := &Func{
		Target: TargetAMD64,
		Insts:  []Inst{{Op: wasm.InstrNop}, {Op: wasm.InstrI32Const, Result: 1}, {Op: wasm.InstrNop}, {Op: wasm.InstrCall, OperandCount: 8}, {Op: wasm.InstrNop}},
		VRegs:  []VRegData{{}, {Type: TypeI32, Bank: BankGPR, Def: 9}},
		Blocks: []Block{{InstCount: 1, Region: railssa.NoRegion}, {InstStart: 1, InstCount: 2, Region: railssa.NoRegion}, {InstStart: 3, InstCount: 2, Region: railssa.NoRegion}},
		Edges:  []Edge{{From: 0, To: 1}, {From: 1, To: 2}},
	}
	schedule := &Schedule{Order: []uint32{0, 1, 2, 3, 4}, BlockRanges: []MoveRange{{Start: 0, Count: 1}, {Start: 1, Count: 2}, {Start: 3, Count: 2}}, BlockOf: []railssa.BlockID{0, 1, 1, 2, 2}}
	allocation := &GreedyAllocation{Allocation: Allocation{
		Locations:            []Location{{}, {Kind: LocationRegister, Bank: BankGPR, Index: 9}},
		Intervals:            []LiveInterval{{Reg: 1, Start: 9, End: 11, Bank: BankGPR}},
		InstructionPositions: []uint32{0, 1, 2, 3, 4},
	}}
	contract := ABIContract{GPRClobbers: 1 << 9, CalleeGPRs: 1 << 9}
	frame := FrameLayout{CalleeSaveBytes: 8, TotalBytes: 16}
	region := []CalleeSaveRegion{{Block: 1, RestoreBlock: 1, RestoreBefore: 2, Bank: BankGPR, Physical: 9}}
	if err := VerifyCalleeSaveRegions(f, schedule, allocation, contract, frame, []bool{false, true, false}, nil, region); err == nil {
		t.Fatal("accepted restore before later R12 argument setup")
	}
}
