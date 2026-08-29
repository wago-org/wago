package railmach

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestGreedySpillDensityPrioritizesFrequentlyUsedShortRange(t *testing.T) {
	sparse := LiveInterval{Start: 0, End: 999, Weight: 2}
	dense := LiveInterval{Start: 0, End: 9, Weight: 8}
	if sparseCost, denseCost := greedySpillCost(sparse, 1000, false), greedySpillCost(dense, 1000, false); sparseCost <= denseCost {
		t.Fatalf("area costs sparse/dense = %d/%d, want sparse range prioritized", sparseCost, denseCost)
	}
	if sparseCost, denseCost := greedySpillCost(sparse, 1000, true), greedySpillCost(dense, 1000, true); denseCost <= sparseCost {
		t.Fatalf("density costs sparse/dense = %d/%d, want dense range prioritized", sparseCost, denseCost)
	}
	if got := greedyEffectiveMaxStage(391, true, 4); got != 3 {
		t.Fatalf("medium density max stage = %d, want 3", got)
	}
	if got := greedyEffectiveMaxStage(512, true, 4); got != 4 {
		t.Fatalf("large density max stage = %d, want 4", got)
	}
	if got := greedyEffectiveMaxStage(391, false, 4); got != 4 {
		t.Fatalf("area-priority max stage = %d, want 4", got)
	}
}

func TestAllocateGreedyPPromotesCallCrossingRange(t *testing.T) {
	m := machineModule([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}, []byte{
		0x20, 0x00,
		0x10, 0x00,
		0x1a,
		0x20, 0x00,
		0x0b,
	})
	f := buildMachineTest(t, TargetAMD64, m)
	config := GreedyConfig{Linear: LinearQConfig{GPRs: 3, FPRs: 1}, CallerGPRs: 2, CallerFPRs: 1, MaxStage: 3}
	linear, err := AllocateLinearQ(f, config.Linear, nil)
	if err != nil {
		t.Fatal(err)
	}
	greedy, err := AllocateGreedyP(f, config, nil)
	if err != nil {
		t.Fatal(err)
	}
	param := f.InstructionOperands(0)[0].Reg
	if linear.Locations[param].Kind == LocationRegister {
		t.Fatalf("linear call-crossing parameter unexpectedly in register: %#v", linear.Locations[param])
	}
	if greedy.Locations[param] != (Location{Kind: LocationRegister, Bank: BankGPR, Index: 2}) || greedy.Metrics.CalleeSaved == 0 {
		t.Fatalf("greedy parameter=%#v metrics=%#v", greedy.Locations[param], greedy.Metrics)
	}
}

func TestAllocateGreedyPUsesExactDirectCallClobbers(t *testing.T) {
	m := machineModule([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}, []byte{
		0x20, 0x00,
		0x10, 0x00,
		0x1a,
		0x20, 0x00,
		0x0b,
	})
	f := buildMachineTest(t, TargetAMD64, m)
	call := uint32(^uint32(0))
	for id, instruction := range f.Insts {
		if instruction.Op == wasm.InstrCall {
			call = uint32(id)
		}
	}
	config := GreedyConfig{
		Linear: LinearQConfig{GPRs: 3, FPRs: 1}, CallerGPRs: 2, CallerFPRs: 1, MaxStage: 3,
		CallClobbers: []CallClobber{{Instruction: call, GPR: 1 << 1}},
	}
	greedy, err := AllocateGreedyP(f, config, nil)
	if err != nil {
		t.Fatal(err)
	}
	param := f.InstructionOperands(0)[0].Reg
	if got := greedy.Locations[param]; got != (Location{Kind: LocationRegister, Bank: BankGPR, Index: 0}) {
		t.Fatalf("exact-clobber parameter = %#v, want safe caller register 0", got)
	}
	if greedy.Metrics.CalleeSaved != 0 || greedy.Metrics.PreservationCost != 0 {
		t.Fatalf("safe caller register charged preservation: %#v", greedy.Metrics)
	}
}

func TestAllocateGreedyPChargesFirstCalleeSavedRegister(t *testing.T) {
	m := machineModule([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}, []byte{
		0x20, 0x00,
		0x10, 0x00,
		0x1a,
		0x20, 0x00,
		0x0b,
	})
	f := buildMachineTest(t, TargetAMD64, m)
	config := GreedyConfig{Linear: LinearQConfig{GPRs: 3, FPRs: 1}, CallerGPRs: 2, CallerFPRs: 1, MaxStage: 3, PreserveGPRCost: ^uint16(0)}
	greedy, err := AllocateGreedyP(f, config, nil)
	if err != nil {
		t.Fatal(err)
	}
	param := f.InstructionOperands(0)[0].Reg
	if greedy.Locations[param].Kind != LocationSpill || greedy.Metrics.CalleeSaved != 0 || greedy.Metrics.PreservationCost != 0 {
		t.Fatalf("costed allocation parameter=%#v metrics=%#v", greedy.Locations[param], greedy.Metrics)
	}
}

func TestAllocateGreedyPStageOneDoesNotEvict(t *testing.T) {
	m := machineModule([]wasm.ValType{wasm.I64, wasm.I64}, []wasm.ValType{wasm.I64}, []byte{
		0x20, 0x00,
		0x20, 0x01,
		0x7c,
		0x0b,
	})
	f := buildMachineTest(t, TargetARM64, m)
	greedy, err := AllocateGreedyP(f, GreedyConfig{Linear: LinearQConfig{GPRs: 1, FPRs: 1}, CallerGPRs: 1, CallerFPRs: 1, MaxStage: 1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if greedy.Metrics.Evictions != 0 || greedy.SpillSlots == 0 {
		t.Fatalf("stage-one allocation = %#v metrics=%#v", greedy.Allocation, greedy.Metrics)
	}
	if len(greedy.SpillSets) == 0 || len(greedy.SpillMembers) == 0 {
		t.Fatalf("spillsets=%#v members=%#v", greedy.SpillSets, greedy.SpillMembers)
	}
}

func TestAllocateGreedyPSplitsSpilledRangeAroundCall(t *testing.T) {
	m := machineModule([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}, []byte{
		0x20, 0x00, 0x42, 0x01, 0x7c, 0x1a,
		0x20, 0x00, 0x42, 0x02, 0x7c, 0x1a,
		0x20, 0x00, 0x10, 0x00, 0x1a,
		0x20, 0x00, 0x42, 0x03, 0x7c, 0x1a,
		0x20, 0x00, 0x42, 0x04, 0x7c,
		0x0b,
	})
	f := buildMachineTest(t, TargetAMD64, m)
	config := GreedyConfig{
		Linear: LinearQConfig{GPRs: 3, FPRs: 1}, CallerGPRs: 2, CallerFPRs: 1,
		MaxStage: 4, PreserveGPRCost: ^uint16(0),
	}
	allocation, err := AllocateGreedyP(f, config, nil)
	if err != nil {
		t.Fatal(err)
	}
	param := f.InstructionOperands(1)[0].Reg
	if allocation.Locations[param].Kind != LocationSpill || len(allocation.Fragments) < 2 || allocation.Metrics.RegionalReloads < 2 {
		t.Fatalf("parameter=%#v fragments=%#v metrics=%#v", allocation.Locations[param], allocation.Fragments, allocation.Metrics)
	}
	for _, fragment := range allocation.Fragments {
		if fragment.Reg == param && allocation.LocationAt(param, fragment.Start).Kind != LocationRegister {
			t.Fatalf("fragment lookup failed: %#v", fragment)
		}
	}
}

func TestAllocateGreedyPDoesNotEvictHotLoopVictimWithoutCall(t *testing.T) {
	m := machineModule([]wasm.ValType{wasm.I64, wasm.I32}, []wasm.ValType{wasm.I64}, []byte{
		0x03, 0x40,
		0x20, 0x00, 0x42, 0x01, 0x7c, 0x1a,
		0x20, 0x01, 0x41, 0x01, 0x6a, 0x1a,
		0x20, 0x01, 0x0d, 0x00,
		0x0b,
		0x20, 0x00,
		0x0b,
	})
	f := buildMachineTest(t, TargetARM64, m)
	allocation, err := AllocateGreedyP(f, GreedyConfig{
		Linear: LinearQConfig{GPRs: 1, FPRs: 1}, CallerGPRs: 1, CallerFPRs: 1, MaxStage: 4,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range allocation.Fragments {
		if fragment.Victim != 0 {
			t.Fatalf("call-free loop evicted victim: fragments=%#v metrics=%#v", allocation.Fragments, allocation.Metrics)
		}
	}
}

func TestRegionalInactiveVictimRejectsResultDefinedBeforeRestore(t *testing.T) {
	f := &Func{VRegs: make([]VRegData, 3)}
	allocation := &GreedyAllocation{
		Allocation: Allocation{
			Locations: []Location{{}, {Kind: LocationRegister, Bank: BankGPR}, {Kind: LocationRegister, Bank: BankGPR}},
			Intervals: []LiveInterval{
				{Reg: 1, Start: 0, End: 2, Bank: BankGPR},
				{Reg: 2, Start: 3, End: 8, Bank: BankGPR},
			},
		},
		occupantNext: []uint32{2, 0},
	}
	allocation.occupantHead[0][0] = 1
	if victim, ok := regionalInactiveVictim(f, allocation, BankGPR, 0, 2, 2); ok {
		t.Fatalf("selected victim %d despite result definition before restore", victim)
	}
}

func TestRegionalVictimFragmentsDoNotEndAtBlockBoundary(t *testing.T) {
	m := machineModule([]wasm.ValType{wasm.I64, wasm.I32}, []wasm.ValType{wasm.I64}, []byte{
		0x03, 0x40,
		0x20, 0x00, 0x42, 0x01, 0x7c, 0x1a,
		0x20, 0x01, 0x0d, 0x00,
		0x0b,
		0x20, 0x00,
		0x0b,
	})
	f := buildMachineTest(t, TargetARM64, m)
	allocation, err := AllocateGreedyP(f, GreedyConfig{
		Linear: LinearQConfig{GPRs: 1, FPRs: 1}, CallerGPRs: 1, CallerFPRs: 1, MaxStage: 4,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for index, fragment := range allocation.Fragments {
		if fragment.Victim != 0 && regionalFragmentEndsAtBlockBoundary(f, nil, fragment.End) {
			t.Fatalf("victim fragment %d ends at block boundary: %#v", index, fragment)
		}
	}
}
