package railmach

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestGreedySpillDensityPrioritizesFrequentlyUsedShortRange(t *testing.T) {
	sparse := LiveInterval{Start: 0, End: 999, Weight: 2}
	dense := LiveInterval{Start: 0, End: 9, Weight: 8}
	if sparseCost, denseCost := greedySpillCost(sparse, 1000, false, false), greedySpillCost(dense, 1000, false, false); sparseCost <= denseCost {
		t.Fatalf("area costs sparse/dense = %d/%d, want sparse range prioritized", sparseCost, denseCost)
	}
	if sparseCost, denseCost := greedySpillCost(sparse, 1000, true, false), greedySpillCost(dense, 1000, true, false); denseCost <= sparseCost {
		t.Fatalf("density costs sparse/dense = %d/%d, want dense range prioritized", sparseCost, denseCost)
	}
	if plain, floored := greedySpillCost(sparse, 1000, true, false), greedySpillCost(sparse, 1000, true, true); floored <= plain {
		t.Fatalf("long-range density costs plain/floored = %d/%d, want scalar-FP floor", plain, floored)
	}
	if got := greedyEffectiveMaxStage(TargetARM64, 391, true, false, 4); got != 3 {
		t.Fatalf("medium density max stage = %d, want 3", got)
	}
	if got := greedyEffectiveMaxStage(TargetARM64, 512, true, false, 4); got != 4 {
		t.Fatalf("large density max stage = %d, want 4", got)
	}
	if got := greedyEffectiveMaxStage(TargetARM64, 391, false, false, 4); got != 4 {
		t.Fatalf("area-priority max stage = %d, want 4", got)
	}
	if got := greedyEffectiveMaxStage(TargetAMD64, 512, false, true, 4); got != 3 {
		t.Fatalf("AMD64 cyclic-call max stage = %d, want 3", got)
	}
	if got := greedyEffectiveMaxStage(TargetAMD64, 512, false, false, 4); got != 4 {
		t.Fatalf("AMD64 ordinary max stage = %d, want 4", got)
	}
	if got := greedyEffectiveMaxStage(TargetARM64, 512, false, true, 4); got != 4 {
		t.Fatalf("ARM64 cyclic-call max stage = %d, want 4", got)
	}
	if got := greedyEffectiveMaxStage(TargetARM64, greedyRegionalMaxInstructions, false, false, 4); got != 3 {
		t.Fatalf("giant-function max stage = %d, want 3", got)
	}
}

func TestGreedyRegisterCandidateUsesTransferAffinityOnlyToBreakCostTie(t *testing.T) {
	if !greedyRegisterCandidateBetter(3, 3, 4, 1, 4) {
		t.Fatal("equal-cost transfer-affine register was not preferred")
	}
	if greedyRegisterCandidateBetter(3, 3, 5, 1, 4) {
		t.Fatal("transfer affinity overrode a cheaper register")
	}
	if !greedyRegisterCandidateBetter(2, 3, 3, 1, 4) {
		t.Fatal("cheaper non-affine register was not preferred")
	}
}

func TestRecolorGreedyTransferAffinitiesRequiresMultipleCoalescedEdges(t *testing.T) {
	config := GreedyConfig{Linear: LinearQConfig{GPRs: 3, FPRs: 1}}
	base := GreedyAllocation{Allocation: Allocation{
		Locations: []Location{{}, {Kind: LocationRegister, Bank: BankGPR, Index: 0}, {Kind: LocationRegister, Bank: BankGPR, Index: 0}, {Kind: LocationRegister, Bank: BankGPR, Index: 2}, {Kind: LocationRegister, Bank: BankGPR, Index: 0}},
		Intervals: []LiveInterval{
			{Reg: 1, Start: 0, End: 1, Bank: BankGPR},
			{Reg: 2, Start: 2, End: 3, Bank: BankGPR},
			{Reg: 3, Start: 4, End: 8, Bank: BankGPR},
		},
	}}
	f := &Func{Target: TargetAMD64, VRegs: make([]VRegData, 5), Transfers: []EdgeTransfer{{Src: 1, Dst: 3, Weight: 8}, {Src: 2, Dst: 3, Weight: 8}}}
	recolorGreedyTransferAffinities(f, &base, config, nil)
	if got := base.Locations[3].Index; got != 0 {
		t.Fatalf("two-edge affine register = %d, want 0", got)
	}
	arm := GreedyAllocation{Allocation: Allocation{
		Locations: append([]Location(nil), base.Locations...),
		Intervals: append([]LiveInterval(nil), base.Intervals...),
	}}
	arm.Locations[3].Index = 2
	f.Target = TargetARM64
	recolorGreedyTransferAffinities(f, &arm, config, nil)
	if got := arm.Locations[3].Index; got != 2 {
		t.Fatalf("ARM64 affine register = %d, want unchanged 2", got)
	}
	f.Target = TargetAMD64

	single := GreedyAllocation{Allocation: Allocation{
		Locations: []Location{{}, {Kind: LocationRegister, Bank: BankGPR, Index: 0}, {}, {Kind: LocationRegister, Bank: BankGPR, Index: 2}},
		Intervals: []LiveInterval{{Reg: 1, Start: 0, End: 1, Bank: BankGPR}, {Reg: 3, Start: 4, End: 8, Bank: BankGPR}},
	}}
	f.Transfers = f.Transfers[:1]
	recolorGreedyTransferAffinities(f, &single, config, nil)
	if got := single.Locations[3].Index; got != 2 {
		t.Fatalf("single-edge affine register = %d, want unchanged 2", got)
	}

	blocked := GreedyAllocation{Allocation: Allocation{
		Locations: append([]Location(nil), base.Locations...),
		Intervals: append(append([]LiveInterval(nil), base.Intervals...), LiveInterval{Reg: 4, Start: 6, End: 7, Bank: BankGPR}),
	}}
	blocked.Locations[3].Index = 2
	f.Transfers = append(f.Transfers, EdgeTransfer{Src: 2, Dst: 3, Weight: 8})
	recolorGreedyTransferAffinities(f, &blocked, config, nil)
	if got := blocked.Locations[3].Index; got != 2 {
		t.Fatalf("occupied affine register = %d, want unchanged 2", got)
	}
}

func TestGreedyDensitySupportsAMD64ScalarFPRs(t *testing.T) {
	for _, test := range []struct {
		name   string
		target Target
		type_  MachineType
		bank   Bank
		want   bool
	}{
		{name: "arm64 scalar float", target: TargetARM64, type_: TypeF64, bank: BankFPR, want: true},
		{name: "amd64 vector", target: TargetAMD64, type_: TypeV128, bank: BankFPR, want: true},
		{name: "amd64 scalar float", target: TargetAMD64, type_: TypeF64, bank: BankFPR, want: true},
		{name: "amd64 integer", target: TargetAMD64, type_: TypeI64, bank: BankGPR, want: true},
		{name: "unsupported", target: 0, want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := &Func{
				Target: test.target,
				Insts:  make([]Inst, greedyDensityMinInstructions),
				VRegs:  []VRegData{{}, {Type: test.type_, Bank: test.bank}},
			}
			if got := greedyUsesDensityCost(f, false); got != test.want {
				t.Fatalf("greedyUsesDensityCost = %t, want %t", got, test.want)
			}
		})
	}
}

func TestGreedyDensityRetainsConservativeScalarFPRPolicyForRecursiveCalls(t *testing.T) {
	f := &Func{
		Target: TargetAMD64,
		Insts:  make([]Inst, greedyDensityMinInstructions),
		VRegs:  []VRegData{{}, {Type: TypeF64, Bank: BankFPR}},
	}
	if greedyUsesDensityCost(f, true) {
		t.Fatal("recursive scalar-FP function enabled density priority")
	}
	if !greedyUsesDensityLongRangeFloor(f, false) || greedyUsesDensityLongRangeFloor(f, true) {
		t.Fatal("scalar-FP long-range floor did not follow recursive-call policy")
	}
	f.VRegs[1] = VRegData{Type: TypeI64, Bank: BankGPR}
	if !greedyUsesDensityCost(f, true) {
		t.Fatal("recursive integer function disabled density priority")
	}
}

func TestGreedyColdRegionalFragmentsAreLargeScalarARM64Only(t *testing.T) {
	for _, test := range []struct {
		name   string
		target Target
		insts  int
		type_  MachineType
		want   bool
	}{
		{name: "large scalar ARM64", target: TargetARM64, insts: greedyRegionalDensityMinInstructions, type_: TypeI32, want: true},
		{name: "small scalar ARM64", target: TargetARM64, insts: greedyRegionalDensityMinInstructions - 1, type_: TypeI32},
		{name: "large vector ARM64", target: TargetARM64, insts: greedyRegionalDensityMinInstructions, type_: TypeV128},
		{name: "large scalar AMD64", target: TargetAMD64, insts: greedyRegionalDensityMinInstructions, type_: TypeI32},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := &Func{Target: test.target, Insts: make([]Inst, test.insts), VRegs: []VRegData{{}, {Type: test.type_}}}
			if got := greedyUsesColdRegionalFragments(f); got != test.want {
				t.Fatalf("greedyUsesColdRegionalFragments = %t, want %t", got, test.want)
			}
		})
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

func TestAllocateGreedyPLeavesSegmentedLivenessStaged(t *testing.T) {
	f := liveRangeHoleFunc()
	config := GreedyConfig{
		Linear:     LinearQConfig{GPRs: 1, FPRs: 1},
		CallerGPRs: 1,
		CallerFPRs: 1,
		MaxStage:   3,
	}
	allocation, err := AllocateGreedyP(f, config, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(allocation.LiveSegments) != 0 || len(allocation.LiveSegmentRanges) != 0 {
		t.Fatalf("GreedyP activated staged segmented liveness: ranges=%#v segments=%#v", allocation.LiveSegmentRanges, allocation.LiveSegments)
	}
	segmented, err := allocateGreedyP(f, nil, config, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(segmented.LiveSegments) != 2 || len(segmented.LiveSegmentRanges) != 1 {
		t.Fatalf("segmented GreedyP omitted exact holes: ranges=%#v segments=%#v", segmented.LiveSegmentRanges, segmented.LiveSegments)
	}
}

func TestAllocateFastMachineRetainsVerifiedSpillSets(t *testing.T) {
	m := machineModule([]wasm.ValType{wasm.I64, wasm.I64}, []wasm.ValType{wasm.I64}, []byte{
		0x20, 0x00,
		0x20, 0x01,
		0x7c,
		0x0b,
	})
	f, selection, _, dag := buildScheduleTest(t, TargetARM64, m)
	schedule, err := BuildSchedule(f, selection, dag, ScheduleKindSourceStable, nil)
	if err != nil {
		t.Fatal(err)
	}
	allocation, err := AllocateFastMachineForSchedule(f, schedule, GreedyConfig{Linear: LinearQConfig{GPRs: 1, FPRs: 1}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if allocation.Stage != 0 || allocation.SpillSlots == 0 || len(allocation.SpillSets) == 0 {
		t.Fatalf("fast allocation stage=%d spill slots=%d sets=%#v", allocation.Stage, allocation.SpillSlots, allocation.SpillSets)
	}
	if err := VerifySpillSets(allocation); err != nil {
		t.Fatal(err)
	}
}

func TestAllocateFastMachinePromotesCallLiveRangeWithoutEviction(t *testing.T) {
	m := machineModule([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}, []byte{
		0x20, 0x00,
		0x10, 0x00,
		0x1a,
		0x20, 0x00,
		0x0b,
	})
	f, selection, _, dag := buildScheduleTest(t, TargetARM64, m)
	schedule, err := BuildSchedule(f, selection, dag, ScheduleKindSourceStable, nil)
	if err != nil {
		t.Fatal(err)
	}
	allocation, err := AllocateFastMachineForSchedule(f, schedule, GreedyConfig{
		Linear: LinearQConfig{GPRs: 2, FPRs: 1}, CallerGPRs: 1, CallerFPRs: 1,
		PreserveGPRCost: 1,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	param := f.InstructionOperands(0)[0].Reg
	if got := allocation.Locations[param]; got != (Location{Kind: LocationRegister, Bank: BankGPR, Index: 1}) {
		t.Fatalf("fast call-live parameter = %#v, want callee-saved register 1", got)
	}
	if allocation.Metrics.Promotions != 1 || allocation.Metrics.CalleeSaved != 1 || allocation.Metrics.PreservationCost != 1 || allocation.Metrics.Evictions != 0 {
		t.Fatalf("fast promotion metrics = %#v", allocation.Metrics)
	}
}

func TestAllocateFastMachineRejectsPromotionBelowPreservationCost(t *testing.T) {
	m := machineModule([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}, []byte{
		0x20, 0x00,
		0x10, 0x00,
		0x1a,
		0x20, 0x00,
		0x0b,
	})
	f, selection, _, dag := buildScheduleTest(t, TargetARM64, m)
	schedule, err := BuildSchedule(f, selection, dag, ScheduleKindSourceStable, nil)
	if err != nil {
		t.Fatal(err)
	}
	allocation, err := AllocateFastMachineForSchedule(f, schedule, GreedyConfig{
		Linear: LinearQConfig{GPRs: 2, FPRs: 1}, CallerGPRs: 1, CallerFPRs: 1,
		PreserveGPRCost: ^uint16(0),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	param := f.InstructionOperands(0)[0].Reg
	if allocation.Locations[param].Kind != LocationSpill || allocation.Metrics.Promotions != 0 || allocation.Metrics.PreservationCost != 0 {
		t.Fatalf("costed fast allocation parameter=%#v metrics=%#v", allocation.Locations[param], allocation.Metrics)
	}
}

func TestAllocateFastMachineUsesExactDirectCallClobbers(t *testing.T) {
	m := machineModule([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}, []byte{
		0x20, 0x00,
		0x10, 0x00,
		0x1a,
		0x20, 0x00,
		0x0b,
	})
	f, selection, _, dag := buildScheduleTest(t, TargetAMD64, m)
	schedule, err := BuildSchedule(f, selection, dag, ScheduleKindSourceStable, nil)
	if err != nil {
		t.Fatal(err)
	}
	call := uint32(^uint32(0))
	for instructionID, instruction := range f.Insts {
		if instruction.Op == wasm.InstrCall {
			call = uint32(instructionID)
		}
	}
	allocation, err := AllocateFastMachineForSchedule(f, schedule, GreedyConfig{
		Linear: LinearQConfig{GPRs: 2, FPRs: 1}, CallerGPRs: 2, CallerFPRs: 1,
		CallClobbers: []CallClobber{{Instruction: call, GPR: 1 << 1}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	param := f.InstructionOperands(0)[0].Reg
	if got := allocation.Locations[param]; got != (Location{Kind: LocationRegister, Bank: BankGPR, Index: 0}) {
		t.Fatalf("fast exact-clobber parameter = %#v, want safe caller register 0", got)
	}
	if allocation.Metrics.CalleeSaved != 0 || allocation.Metrics.PreservationCost != 0 {
		t.Fatalf("safe caller register charged preservation: %#v", allocation.Metrics)
	}
}

func TestDefaultARM64GreedyConfigModelsNoncontiguousCallerFPRs(t *testing.T) {
	config := DefaultGreedyConfig(TargetARM64)
	want := lowMask(16) | uint64(0xf)<<24
	if got := config.CallerMask(BankFPR); got != want {
		t.Fatalf("caller FPR mask = %#x, want %#x", got, want)
	}
	for physical := uint(16); physical < 24; physical++ {
		if want&(uint64(1)<<physical) != 0 {
			t.Fatalf("private callee-saved FPR %d is caller-clobbered", physical)
		}
	}
	if config.Linear.FPRs != 28 {
		t.Fatalf("allocatable ARM64 FPRs = %d, want 28", config.Linear.FPRs)
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
