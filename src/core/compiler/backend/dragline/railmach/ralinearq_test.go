package railmach

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railssa"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestAllocateLinearQSpillsUnderPressure(t *testing.T) {
	m := machineModule([]wasm.ValType{wasm.I64, wasm.I64}, []wasm.ValType{wasm.I64}, []byte{
		0x20, 0x00,
		0x20, 0x01,
		0x7c,
		0x0b,
	})
	f := buildMachineTest(t, TargetARM64, m)
	allocation, err := AllocateLinearQ(f, LinearQConfig{GPRs: 1, FPRs: 1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if allocation.SpillSlots == 0 || allocation.FrameBytes == 0 {
		t.Fatalf("allocation has no spill under pressure: %#v", allocation)
	}
	if err := VerifyAllocation(f, allocation, LinearQConfig{GPRs: 1, FPRs: 1}); err != nil {
		t.Fatal(err)
	}
}

func TestAllocateLinearQUsesAlignedV128SpillHomes(t *testing.T) {
	m := machineModule([]wasm.ValType{wasm.V128, wasm.V128}, []wasm.ValType{wasm.V128}, []byte{
		0x20, 0x00,
		0x20, 0x01,
		0xfd, 0x51,
		0x0b,
	})
	f := buildMachineTest(t, TargetARM64, m)
	config := LinearQConfig{GPRs: 1, FPRs: 1}
	allocation, err := AllocateLinearQ(f, config, nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for reg, location := range allocation.Locations {
		if location.Kind != LocationSpill || f.VRegs[reg].Type != TypeV128 {
			continue
		}
		found = true
		if location.Index&1 != 0 || uint32(location.Index)+2 > uint32(allocation.SpillSlots) {
			t.Fatalf("v128 spill r%d = %#v within %d slots", reg, location, allocation.SpillSlots)
		}
	}
	if !found || allocation.SpillSlots < 2 || allocation.FrameBytes < 16 {
		t.Fatalf("allocation lacks 16-byte vector home: slots=%d frame=%d locations=%#v", allocation.SpillSlots, allocation.FrameBytes, allocation.Locations)
	}
	if err := VerifyAllocation(f, allocation, config); err != nil {
		t.Fatal(err)
	}
}

func TestAllocateLinearQWeightsInstructionUsesByBlockFrequency(t *testing.T) {
	f := buildMachineTest(t, TargetARM64, machineModule([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, []byte{
		0x20, 0x00,
		0x41, 0x01,
		0x6a,
		0x0b,
	}))
	if len(f.Blocks) == 0 || f.Blocks[0].InstCount != uint32(len(f.Insts)) {
		t.Fatalf("blocks = %#v", f.Blocks)
	}
	f.Blocks[0].Weight = 64
	allocation, err := AllocateLinearQ(f, DefaultLinearQConfig(TargetARM64), nil)
	if err != nil {
		t.Fatal(err)
	}
	add := f.Insts[len(f.Insts)-1]
	for _, operand := range f.InstructionOperands(uint32(len(f.Insts) - 1)) {
		interval, ok := allocationInterval(allocation.Intervals, operand.Reg)
		if !ok {
			t.Fatalf("add operand v%d has no interval", operand.Reg)
		}
		if interval.Weight != 65 {
			t.Fatalf("add operand v%d weight = %d, want 65", operand.Reg, interval.Weight)
		}
	}
	if add.Op != wasm.InstrI32Add {
		t.Fatalf("last instruction = %s, want i32.add", add.Op)
	}
}

func TestAllocateLinearQRecordsConflictingFixedUses(t *testing.T) {
	m := machineModule([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}, []byte{
		0x42, 0x01,
		0x20, 0x00,
		0x86,
		0x1a,
		0x20, 0x00,
		0x42, 0x02,
		0x7f,
		0x0b,
	})
	f := buildMachineTest(t, TargetAMD64, m)
	allocation, err := AllocateLinearQ(f, LinearQConfig{GPRs: 2, FPRs: 1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(allocation.FixedMoves) == 0 {
		t.Fatalf("conflicting fixed uses produced no moves: locations=%#v", allocation.Locations)
	}
}

func TestAllocateLinearQRematerializesConstants(t *testing.T) {
	m := machineModule(nil, []wasm.ValType{wasm.I64}, []byte{
		0x42, 0x01,
		0x42, 0x02,
		0x7c,
		0x42, 0x03,
		0x7c,
		0x0b,
	})
	f := buildMachineTest(t, TargetARM64, m)
	allocation, err := AllocateLinearQ(f, LinearQConfig{GPRs: 1, FPRs: 1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for reg, location := range allocation.Locations {
		if location.Kind == LocationRematerialize {
			found = true
			if f.VRegs[reg].Flags&VRegRematerializable == 0 {
				t.Fatalf("non-rematerializable r%d assigned %#v", reg, location)
			}
		}
	}
	if !found {
		t.Fatalf("allocation did not rematerialize under pressure: %#v", allocation.Locations)
	}
}

func TestVerifyAllocationAllowsSharedRegisterAcrossLiveRangeHole(t *testing.T) {
	f := &Func{
		Insts:  make([]Inst, 4),
		VRegs:  []VRegData{{}, {Type: TypeI64, Bank: BankGPR}, {Type: TypeI64, Bank: BankGPR}},
		Blocks: []Block{{InstCount: 4}},
	}
	allocation := &Allocation{
		Locations: []Location{
			{},
			{Kind: LocationRegister, Bank: BankGPR, Index: 0},
			{Kind: LocationRegister, Bank: BankGPR, Index: 0},
		},
		Intervals: []LiveInterval{
			{Reg: 1, Start: 0, End: 20, Bank: BankGPR, Flags: liveIntervalSegmented},
			{Reg: 2, Start: 6, End: 14, Bank: BankGPR},
		},
		LiveSegments:         []LiveSegment{{Start: 0, End: 5}, {Start: 15, End: 20}},
		LiveSegmentRanges:    []LiveSegmentRange{{Reg: 1, SegmentCount: 2}},
		InstructionPositions: []uint32{0, 1, 2, 3},
	}
	if err := VerifyAllocation(f, allocation, LinearQConfig{GPRs: 1, FPRs: 1}); err != nil {
		t.Fatal(err)
	}
}

func TestAllocateLinearQSharesRegisterAcrossCFGLayoutHole(t *testing.T) {
	f := liveRangeHoleFunc()
	allocation, err := AllocateLinearQ(f, LinearQConfig{GPRs: 1, FPRs: 1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, reg := range []VReg{1, 2} {
		if got := allocation.Locations[reg]; got.Kind != LocationRegister || got.Index != 0 {
			t.Fatalf("r%d location = %#v, want shared register 0", reg, got)
		}
	}
	if len(allocation.LiveSegments) != 2 || len(allocation.LiveSegmentRanges) != 1 || allocation.LiveSegmentRanges[0].Reg != 1 {
		t.Fatalf("segmented allocation = intervals %#v ranges %#v segments %#v", allocation.Intervals, allocation.LiveSegmentRanges, allocation.LiveSegments)
	}
}

func TestAllocateLinearQKeepsLoopRangesConservative(t *testing.T) {
	f := liveRangeHoleFunc()
	f.Blocks[1].Flags |= railssa.BlockLoopHeader
	allocation, err := AllocateLinearQ(f, LinearQConfig{GPRs: 1, FPRs: 1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(allocation.LiveSegments) != 0 || len(allocation.LiveSegmentRanges) != 0 || allocation.SpillSlots == 0 {
		t.Fatalf("loop allocation activated staged segments: spills=%d ranges=%#v segments=%#v", allocation.SpillSlots, allocation.LiveSegmentRanges, allocation.LiveSegments)
	}
}

func liveRangeHoleFunc() *Func {
	return &Func{
		Target: TargetARM64,
		Insts: []Inst{
			{Result: 1, Op: wasm.InstrI64Const},
			{Result: 2, Op: wasm.InstrI64Const},
			{OperandStart: 0, OperandCount: 1, Op: wasm.InstrDrop},
			{OperandStart: 1, OperandCount: 1, Op: wasm.InstrDrop},
		},
		Operands: []Operand{
			{Reg: 2, Bank: BankGPR, Fixed: NoFixedReg, Flags: OperandUse},
			{Reg: 1, Bank: BankGPR, Fixed: NoFixedReg, Flags: OperandUse},
		},
		VRegs: []VRegData{
			{},
			{Type: TypeI64, Bank: BankGPR, Def: 3},
			{Type: TypeI64, Bank: BankGPR, Def: 9},
		},
		Blocks: []Block{
			{InstStart: 0, InstCount: 1},
			{InstStart: 1, InstCount: 2},
			{InstStart: 3, InstCount: 1},
		},
		Edges: []Edge{{From: 0, To: 1}, {From: 0, To: 2}},
	}
}

func TestAllocateLinearQKeepsColdAffineRematerializationBaseLive(t *testing.T) {
	module := machineModule([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}, []byte{
		0x20, 0x00, // local.get 0
		0x42, 0x03, // i64.const 3
		0x7c,       // i64.add
		0x42, 0x02, // i64.const 2
		0x7e, // i64.mul
		0x0b,
	})
	f := buildMachineTest(t, TargetARM64, module)
	affineValue := f.Insts[1].Result
	base := f.InstructionOperands(1)[0].Reg
	pressure := &railssa.PressurePlan{
		Remats:   []railssa.RematRecipe{{Value: railssa.FlowValueID(affineValue), Base: railssa.FlowValueID(base), Aux: 3, Kind: railssa.RematAffine}},
		ColdUses: []railssa.ColdUse{{Value: railssa.FlowValueID(affineValue), Instruction: 3, HotWeight: 8, ColdWeight: 1}},
	}
	priced := &RematPlan{Decisions: []RematDecision{{Value: affineValue, Base: base, RecipeCost: 2, SpillCost: 20, Profitable: true}}}
	if committed, err := ApplyColdRematerialization(f, pressure, priced); err != nil || committed != 1 {
		t.Fatalf("ApplyColdRematerialization committed=%d err=%v", committed, err)
	}
	allocation, err := AllocateLinearQ(f, LinearQConfig{GPRs: 1, FPRs: 1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	interval, ok := allocationInterval(allocation.Intervals, base)
	if !ok {
		t.Fatalf("base vreg %d has no live interval", base)
	}
	wantEnd := allocation.InstructionPositions[3]*6 + 2
	if interval.End < wantEnd {
		t.Fatalf("base vreg %d ends at %d before cold rematerialization use %d", base, interval.End, wantEnd)
	}
}

func TestExtendLoopLiveIntervalsUsesScheduledBlockBounds(t *testing.T) {
	f := &Func{
		Insts: make([]Inst, 5),
		VRegs: []VRegData{
			{},
			{},
			{Flags: VRegBlockParam},
		},
		Blocks: []Block{
			{InstStart: 0, InstCount: 1},
			{InstStart: 1, InstCount: 2, Flags: railssa.BlockLoopHeader},
			{InstStart: 3, InstCount: 2},
		},
		Edges: []Edge{{From: 2, To: 1}},
	}
	// Scheduling moved an invariant into the preheader, making the scheduled
	// loop occupy [2, 5) even though its source-linear extent is [1, 5).
	schedule := &Schedule{Order: []uint32{0, 2, 1, 3, 4}, BlockRanges: []MoveRange{
		{Start: 0, Count: 2},
		{Start: 2, Count: 1},
		{Start: 3, Count: 2},
	}}
	starts := []uint32{0, 10, 12}
	ends := []uint32{0, 14, 14}
	used := []bool{false, true, true}

	extendLoopLiveIntervals(f, schedule, starts, ends, used)

	if got, want := ends[1], uint32(30); got != want {
		t.Fatalf("loop invariant end = %d, want scheduled backedge %d", got, want)
	}
	if got, want := ends[2], uint32(14); got != want {
		t.Fatalf("loop block parameter end = %d, want unchanged %d", got, want)
	}
}

func TestAllocateLinearQReusesScratchWithoutRetainingState(t *testing.T) {
	m := machineModule(nil, []wasm.ValType{wasm.I64}, []byte{
		0x42, 0x01,
		0x42, 0x02,
		0x7c,
		0x42, 0x03,
		0x7c,
		0x0b,
	})
	f := buildMachineTest(t, TargetARM64, m)
	config := LinearQConfig{GPRs: 1, FPRs: 1}
	allocation, err := AllocateLinearQ(f, config, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantSpills := allocation.SpillSlots
	wantFrame := allocation.FrameBytes
	wantLocations := append([]Location(nil), allocation.Locations...)

	allocation, err = AllocateLinearQ(f, config, allocation)
	if err != nil {
		t.Fatal(err)
	}
	if allocation.SpillSlots != wantSpills || allocation.FrameBytes != wantFrame {
		t.Fatalf("reused allocation changed spill layout: slots/frame = %d/%d, want %d/%d", allocation.SpillSlots, allocation.FrameBytes, wantSpills, wantFrame)
	}
	if len(allocation.Locations) != len(wantLocations) {
		t.Fatalf("reused allocation has %d locations, want %d", len(allocation.Locations), len(wantLocations))
	}
	for reg, want := range wantLocations {
		if got := allocation.Locations[reg]; got != want {
			t.Fatalf("reused allocation r%d = %#v, want %#v", reg, got, want)
		}
	}
	if err := VerifyAllocation(f, allocation, config); err != nil {
		t.Fatal(err)
	}
}

func TestAllocateGreedyPUsesVerifiedSchedulePositions(t *testing.T) {
	m := machineModule(nil, []wasm.ValType{wasm.I64}, []byte{
		0x42, 0x01,
		0x42, 0x02,
		0x7c,
		0x42, 0x08,
		0x42, 0x02,
		0x7f,
		0x7c,
		0x0b,
	})
	f, _, _, dag := buildScheduleTest(t, TargetAMD64, m)
	order := make([]uint32, len(f.Insts))
	for id := range order {
		order[id] = uint32(id)
	}
	ranges := make([]MoveRange, len(f.Blocks))
	for blockID, block := range f.Blocks {
		ranges[blockID] = MoveRange{Start: block.InstStart, Count: block.InstCount}
		if block.InstCount == 7 {
			for ordinal, local := range []uint32{3, 4, 5, 0, 1, 2, 6} {
				order[int(block.InstStart)+ordinal] = block.InstStart + local
			}
		}
	}
	schedule := &Schedule{Kind: ScheduleKindLatencyFusion, Order: order, BlockRanges: ranges}
	if err := VerifySchedule(f, dag, schedule); err != nil {
		t.Fatalf("test schedule is not dependency legal: %v", err)
	}
	allocation, err := AllocateGreedyPForSchedule(f, schedule, DefaultGreedyConfig(TargetAMD64), nil)
	if err != nil {
		t.Fatal(err)
	}
	for ordinal, instruction := range schedule.Order {
		if got := allocation.InstructionPositions[instruction]; got != uint32(ordinal) {
			t.Fatalf("instruction %d position = %d, want %d", instruction, got, ordinal)
		}
	}
	if err := VerifyAllocation(f, &allocation.Allocation, DefaultLinearQConfig(TargetAMD64)); err != nil {
		t.Fatal(err)
	}
}
