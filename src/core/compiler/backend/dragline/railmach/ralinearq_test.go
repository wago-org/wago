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
