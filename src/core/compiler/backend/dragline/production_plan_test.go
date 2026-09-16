package dragline

import (
	"slices"
	"testing"

	corecompiler "github.com/wago-org/wago/src/core/compiler"
	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railmach"
	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railssa"
	compilerprofile "github.com/wago-org/wago/src/core/compiler/profile"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestNativeDenseLocalTableTargets(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x02})),
		wasmtest.Section(9, wasmtest.Vec([]byte{0x00, 0x41, 0x00, 0x0b, 0x02, 0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x6a, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x6b, 0x0b}),
			wasmtest.Code([]byte{0x20, 1, 0x20, 2, 0x20, 0, 0x11, 0, 0, 0x0b}),
		)),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	targets, ok := nativeDenseLocalTableTargets(m)
	if !ok || !slices.Equal(targets, []uint32{0, 1}) {
		t.Fatalf("dense local targets = %v, %v", targets, ok)
	}
	m.Exports = append(m.Exports, wasm.Export{Index: wasm.ExternIdx{Kind: wasm.ExternTable}})
	if targets, ok := nativeDenseLocalTableTargets(m); ok {
		t.Fatalf("exported table targets = %v, want no proof", targets)
	}
}

func TestNativeARM64VectorAllocatableFPRs(t *testing.T) {
	vector := railmach.VRegData{Type: railmach.TypeV128, Bank: railmach.BankFPR}
	integer := railmach.VRegData{Type: railmach.TypeI32, Bank: railmach.BankGPR}
	safe := &railmach.Func{
		VRegs: []railmach.VRegData{{}, vector, vector, {Type: integer.Type, Bank: integer.Bank, Def: 3}},
		Insts: []railmach.Inst{
			{Op: wasm.InstrI32Const, Result: 3},
			{Op: wasm.InstrV128Xor, Result: 2, OperandStart: 0, OperandCount: 2},
			{Op: wasm.InstrI32x4ShrU, Result: 1, OperandStart: 2, OperandCount: 2},
		},
		Operands: []railmach.Operand{
			{Reg: 1}, {Reg: 2},
			{Reg: 2}, {Reg: 3, Flags: railmach.OperandColdRemat},
		},
	}
	if got := nativeARM64VectorAllocatableFPRs(safe); got != 28 {
		t.Fatalf("scratch-free vector FPRs = %d, want 28", got)
	}
	safe.Insts[0].Op = wasm.InstrI32Add
	if got := nativeARM64VectorAllocatableFPRs(safe); got != 24 {
		t.Fatalf("variable-shift vector FPRs = %d, want 24", got)
	}
	safe.Insts[1].Op = wasm.InstrV128Bitselect
	if got := nativeARM64VectorAllocatableFPRs(safe); got != 24 {
		t.Fatalf("scratch-using vector FPRs = %d, want 24", got)
	}
}

func TestPreserveNativeARM64RepeatedAddInvariant(t *testing.T) {
	machine := &railmach.Func{
		Target: railmach.TargetARM64,
		VRegs: []railmach.VRegData{
			{},
			{Type: railmach.TypeI32, Bank: railmach.BankGPR, Flags: railmach.VRegInitial},
			{Type: railmach.TypeI32, Bank: railmach.BankGPR, Def: 3, Flags: railmach.VRegRematerializable},
			{Type: railmach.TypeI32, Bank: railmach.BankGPR, Def: 9},
			{Type: railmach.TypeI32, Bank: railmach.BankGPR, Def: 15},
			{Type: railmach.TypeI32, Bank: railmach.BankGPR, Def: 21},
			{Type: railmach.TypeI32, Bank: railmach.BankGPR, Def: 27},
		},
		Insts: []railmach.Inst{
			{Op: wasm.InstrI32Const, Aux: 1, Result: 2},
			{Op: wasm.InstrI32Add, Result: 3, OperandStart: 0, OperandCount: 2},
			{Op: wasm.InstrI32Add, Result: 4, OperandStart: 2, OperandCount: 2},
			{Op: wasm.InstrI32Add, Result: 5, OperandStart: 4, OperandCount: 2},
			{Op: wasm.InstrI32Add, Result: 6, OperandStart: 6, OperandCount: 2},
		},
		Operands: []railmach.Operand{
			{Reg: 1}, {Reg: 2}, {Reg: 3}, {Reg: 2},
			{Reg: 4}, {Reg: 2}, {Reg: 5}, {Reg: 2},
		},
		Results: []railmach.VReg{6},
		Blocks:  []railmach.Block{{InstCount: 5}},
	}
	schedule := &railmach.Schedule{
		Order:       []uint32{0, 1, 2, 3, 4},
		BlockRanges: []railmach.MoveRange{{Count: 5}},
		BlockOf:     make([]railssa.BlockID, 5),
	}
	var repeats nativeInstructionRelation
	repeats.prepare(5, true)
	repeats.set(4, 1)
	var skipped nativeBitSet
	skipped.prepare(5, true)
	for bit := uint32(0); bit < 4; bit++ {
		skipped.set(bit, true)
	}
	preserveNativeARM64RepeatedAddInputs(machine, schedule, repeats, &skipped)
	if skipped.has(0) {
		t.Fatal("repeated-add invariant constant remained suppressed")
	}
}

func TestNativeARM64AllocatableFPRsRespectReservedRegisters(t *testing.T) {
	tests := []struct {
		name  string
		insts []railmach.Inst
		want  uint8
	}{
		{name: "call-free", want: 25},
		{name: "call-free f32 copysign", insts: []railmach.Inst{{Op: wasm.InstrF32Copysign}}, want: 24},
		{name: "call", insts: []railmach.Inst{{Op: wasm.InstrCall}}, want: 28},
		{name: "call and f32 copysign", insts: []railmach.Inst{{Op: wasm.InstrCall}, {Op: wasm.InstrF32Copysign}}, want: 27},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := nativeARM64AllocatableFPRs(&railmach.Func{Insts: test.insts}); got != test.want {
				t.Fatalf("allocatable FPRs = %d, want %d", got, test.want)
			}
		})
	}
}

func TestNativeMachineHasExternalCall(t *testing.T) {
	stack := &railssa.StackFunc{ImportedFuncs: 1}
	if !nativeMachineHasExternalCall(stack, &railmach.Func{Insts: []railmach.Inst{{Op: wasm.InstrCall, Aux: 0}}}) {
		t.Fatal("imported call was not external")
	}
	if nativeMachineHasExternalCall(stack, &railmach.Func{Insts: []railmach.Inst{{Op: wasm.InstrCall, Aux: 1}}}) {
		t.Fatal("private local call was external")
	}
	if !nativeMachineHasExternalCall(stack, &railmach.Func{Insts: []railmach.Inst{{Op: wasm.InstrMemoryGrow}}}) {
		t.Fatal("runtime helper call was not external")
	}
}

func TestNativeARM64CachesGlobalDescriptorsOnlyWhenDense(t *testing.T) {
	machine := &railmach.Func{Target: railmach.TargetARM64, Insts: []railmach.Inst{
		{Op: wasm.InstrGlobalGet}, {Op: wasm.InstrGlobalSet}, {Op: wasm.InstrGlobalGet},
	}}
	if nativeARM64CachesGlobals(machine) {
		t.Fatal("three global accesses enabled the ARM64 descriptor cache")
	}
	machine.Insts = append(machine.Insts, railmach.Inst{Op: wasm.InstrGlobalGet})
	if !nativeARM64CachesGlobals(machine) {
		t.Fatal("four global accesses did not enable the ARM64 descriptor cache")
	}
	stack := &railssa.StackFunc{Globals: []wasm.ValType{wasm.I32}}
	if _, ok := nativeARM64CachedGlobal(stack, machine); ok {
		t.Fatal("call-free function enabled the write-through global-value cache")
	}
	machine.Insts = append(machine.Insts,
		railmach.Inst{Op: wasm.InstrGlobalSet}, railmach.Inst{Op: wasm.InstrGlobalGet},
		railmach.Inst{Op: wasm.InstrGlobalSet}, railmach.Inst{Op: wasm.InstrGlobalGet},
		railmach.Inst{Op: wasm.InstrCall},
	)
	if index, ok := nativeARM64CachedGlobal(stack, machine); !ok || index != 0 {
		t.Fatalf("write-through cached global = %d, %t; want 0, true", index, ok)
	}
	stack.Globals = append(stack.Globals, wasm.I32)
	for range 5 {
		machine.Insts = append(machine.Insts,
			railmach.Inst{Op: wasm.InstrGlobalSet, Aux: 1},
			railmach.Inst{Op: wasm.InstrGlobalGet, Aux: 1})
	}
	if globals, count := nativeARM64CachedGlobals(stack, machine); count != 2 || globals != [2]uint32{1, 0} {
		t.Fatalf("write-through cached globals = %v, %d; want [1 0], 2", globals, count)
	}
	machine.Target = railmach.TargetAMD64
	if nativeARM64CachesGlobals(machine) {
		t.Fatal("AMD64 function enabled the ARM64 descriptor cache")
	}
}

func TestNativeAMD64CachesGlobalDescriptorsOnlyWhenDense(t *testing.T) {
	machine := &railmach.Func{Target: railmach.TargetAMD64, Insts: []railmach.Inst{
		{Op: wasm.InstrGlobalGet}, {Op: wasm.InstrGlobalSet}, {Op: wasm.InstrGlobalGet},
	}, Blocks: []railmach.Block{{InstCount: 3, Weight: 1}}}
	if nativeAMD64CachesGlobalDescriptors(machine) {
		t.Fatal("three global accesses enabled the AMD64 descriptor-array cache")
	}
	if nativeAMD64CachesGlobals(machine) {
		t.Fatal("cold global accesses enabled the AMD64 descriptor cache")
	}
	machine.Insts = append(machine.Insts, railmach.Inst{Op: wasm.InstrGlobalGet})
	machine.Blocks[0].InstCount = 4
	if nativeAMD64CachesGlobalDescriptors(machine) {
		t.Fatal("four cold global accesses enabled the AMD64 descriptor-array cache")
	}
	machine.Blocks[0].Weight = 4
	if !nativeAMD64CachesGlobalDescriptors(machine) {
		t.Fatal("four global accesses did not enable the AMD64 descriptor-array cache")
	}
	if index, ok := nativeAMD64CachedGlobal(machine); !ok || index != 0 {
		t.Fatalf("hot cached global = %d, %t; want 0, true", index, ok)
	}
	machine.Insts = append(machine.Insts, railmach.Inst{Op: wasm.InstrCall})
	if nativeAMD64CachesGlobals(machine) {
		t.Fatal("call-crossing function enabled the AMD64 descriptor cache")
	}
	if !nativeAMD64CachesGlobalDescriptors(machine) {
		t.Fatal("call-crossing function disabled the AMD64 descriptor-array cache")
	}
	machine.Target = railmach.TargetARM64
	if nativeAMD64CachesGlobalDescriptors(machine) {
		t.Fatal("ARM64 function enabled the AMD64 descriptor-array cache")
	}
	if nativeAMD64CachesGlobals(machine) {
		t.Fatal("ARM64 function enabled the AMD64 descriptor cache")
	}
}

func TestNativeAMD64StackCachesProfitableCallCrossingGlobals(t *testing.T) {
	stack := &railssa.StackFunc{Globals: []wasm.ValType{wasm.I32, wasm.I64, wasm.ExternRef}}
	machine := &railmach.Func{
		Target: railmach.TargetAMD64,
		Insts: []railmach.Inst{
			{Op: wasm.InstrCall},
			{Op: wasm.InstrGlobalGet, Aux: 0},
			{Op: wasm.InstrGlobalGet, Aux: 1},
			{Op: wasm.InstrGlobalGet, Aux: 2},
			{Op: wasm.InstrGlobalGet, Aux: 0},
		},
		Blocks: []railmach.Block{
			{InstStart: 0, InstCount: 1, Weight: 1},
			{InstStart: 1, InstCount: 1, Weight: 64},
			{InstStart: 2, InstCount: 1, Weight: 32},
			{InstStart: 3, InstCount: 1, Weight: 128},
			{InstStart: 4, InstCount: 1, Weight: 1},
		},
	}
	if globals, count := nativeAMD64StackCachedGlobals(stack, machine); count != 2 || globals != [2]uint32{0, 1} {
		t.Fatalf("stack-cached globals = %v, %d; want [0 1], 2", globals, count)
	}
	machine.Insts[0].Op = wasm.InstrNop
	if _, count := nativeAMD64StackCachedGlobals(stack, machine); count != 0 {
		t.Fatal("call-free function enabled the call-crossing frame cache")
	}
}

func TestNativeAMD64CachedMemoryBoundSelectsHotAccessEnd(t *testing.T) {
	p := new(nativeBackendPlanner)
	stack := &railssa.StackFunc{MemoryMinBytes: 1 << 16}
	machine := &railmach.Func{
		Target: railmach.TargetAMD64,
		Insts: []railmach.Inst{
			{Op: wasm.InstrI32Load, Aux: 0},
			{Op: wasm.InstrI64Load, Aux: 0},
			{Op: wasm.InstrF64Store, Aux: 0},
		},
		Blocks: []railmach.Block{
			{InstStart: 0, InstCount: 1, Weight: 1},
			{InstStart: 1, InstCount: 2, Weight: 4},
		},
	}
	pressure := &railssa.PressurePlan{Blocks: make([]railssa.BlockPressure, len(machine.Blocks))}
	if end, ok := p.nativeAMD64CachedMemoryBound(stack, machine, pressure); !ok || end != 8 {
		t.Fatalf("cached memory bound = (%d, %t), want (8, true)", end, ok)
	}
	machine.Insts = append(machine.Insts, make([]railmach.Inst, 30)...)
	if end, ok := p.nativeAMD64CachedMemoryBound(stack, machine, pressure); ok || end != 0 {
		t.Fatalf("large-function cached memory bound = (%d, %t), want disabled", end, ok)
	}
	machine.Insts = machine.Insts[:3]
	p.signalsBounds = true
	if end, ok := p.nativeAMD64CachedMemoryBound(stack, machine, pressure); ok || end != 0 {
		t.Fatalf("signals-based cached memory bound = (%d, %t), want disabled", end, ok)
	}
	p.signalsBounds = false
	machine.VRegs = []railmach.VRegData{{Bank: railmach.BankFPR}}
	if end, ok := p.nativeAMD64CachedMemoryBound(stack, machine, pressure); ok || end != 0 {
		t.Fatalf("mixed floating access cached memory bound = (%d, %t), want disabled", end, ok)
	}
	machine.VRegs = nil
	pressure.Blocks[0].PeakGPR = 32
	if end, ok := p.nativeAMD64CachedMemoryBound(stack, machine, pressure); ok || end != 0 {
		t.Fatalf("high-pressure cached memory bound = (%d, %t), want disabled", end, ok)
	}
	pressure.Blocks[0].PeakGPR = 0
	machine.Insts = append(machine.Insts, railmach.Inst{Op: wasm.InstrMemoryGrow})
	if end, ok := p.nativeAMD64CachedMemoryBound(stack, machine, pressure); ok || end != 0 {
		t.Fatalf("growing function cached memory bound = (%d, %t), want disabled", end, ok)
	}
}

func TestNativeBackendPlannerBuildsCompleteRailMachProduct(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I64}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x42, 7, 0x7c, 0x20, 1, 0x42, 3, 0x7d, 0x84, 0x0b}))),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	stack, err := railssa.BuildStackFunc(m, 0)
	if err != nil {
		t.Fatal(err)
	}
	target, err := corecompiler.HostTarget(corecompiler.TargetNative)
	if err != nil {
		t.Fatal(err)
	}
	var planner nativeBackendPlanner
	plan, err := planner.Plan(stack, target)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Machine == nil || plan.Selection == nil || plan.Schedule == nil || plan.Allocation == nil || plan.Exit == nil || plan.PostRA == nil || plan.Score.Kind == 0 {
		t.Fatalf("incomplete native backend plan: %#v", plan)
	}
	if len(plan.Machine.Memory) != 0 {
		t.Fatalf("non-memory function has %d memory descriptors", len(plan.Machine.Memory))
	}
	if planner.memoryCheckSlots.capacityBytes() != 0 || cap(planner.memoryCheckEnds) != 0 || cap(planner.memoryCheckTouched) != 0 {
		t.Fatalf("non-memory function retained bounds scratch: slots=%d ends=%d touched=%d", planner.memoryCheckSlots.capacityBytes(), cap(planner.memoryCheckEnds), cap(planner.memoryCheckTouched))
	}
	if err := railmach.VerifyAllocation(plan.Machine, &plan.Allocation.Allocation, railmach.DefaultLinearQConfig(plan.Machine.Target)); err != nil {
		t.Fatal(err)
	}
	if got, machineBytes := planner.CapacityBytes(), railmach.CapacityBytes(plan.Machine); got <= machineBytes {
		t.Fatalf("native planner capacity = %d, want more than machine-only %d", got, machineBytes)
	}
	breakdown := railssa.MeasurePipelineCapacity(&planner.cfg, &planner.locals, &planner.flow, &planner.semantic, &planner.metadata, &planner.simplified, &planner.pressure, &planner.specialize, &planner.emission)
	if breakdown.Total() == 0 || breakdown.Total() >= planner.CapacityBytes() {
		t.Fatalf("RailSSA capacity breakdown = %#v, planner = %d", breakdown, planner.CapacityBytes())
	}
	t.Logf("RailSSA retained capacity: %#v", breakdown)
}

func TestNativeBackendPlannerSizesMemoryCheckScratchFromUniqueAddresses(t *testing.T) {
	body := []byte{0x20, 0x00, 0x41, 0x01, 0x6a, 0x1a, 0x20, 0x00, 0x28, 0x02, 0x00, 0x1a, 0x20, 0x00, 0x28, 0x02, 0x00, 0x0b}
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	stack, err := railssa.BuildStackFunc(m, 0)
	if err != nil {
		t.Fatal(err)
	}
	target, err := corecompiler.HostTarget(corecompiler.TargetNative)
	if err != nil {
		t.Fatal(err)
	}
	var planner nativeBackendPlanner
	plan, err := planner.Plan(stack, target)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Machine.Memory) != 2 || len(plan.Machine.Insts) <= len(plan.Machine.Memory) {
		t.Fatalf("machine instructions/accesses = %d/%d", len(plan.Machine.Insts), len(plan.Machine.Memory))
	}
	if got, want := cap(planner.memoryCheckTouched), 1; got != want || cap(planner.memoryCheckEnds) != want {
		t.Fatalf("memory-check scratch capacity = touched:%d ends:%d, want unique address count %d", got, cap(planner.memoryCheckEnds), want)
	}
	firstSlot, firstOK := plan.MemoryCheckSlots.get(uint32(plan.Machine.Memory[0].AddressValue))
	secondSlot, secondOK := plan.MemoryCheckSlots.get(uint32(plan.Machine.Memory[1].AddressValue))
	if !firstOK || !secondOK || firstSlot != secondSlot {
		t.Fatalf("memory-check slots = %d/%t and %d/%t, want one shared slot", firstSlot, firstOK, secondSlot, secondOK)
	}
	if cap(planner.deadGCReservations) != 0 || cap(planner.noBarrierGCStores) != 0 {
		t.Fatalf("non-GC function retained GC scratch: reservations=%d barriers=%d", cap(planner.deadGCReservations), cap(planner.noBarrierGCStores))
	}
}

func TestRetainNativeBackendPlannerWithin(t *testing.T) {
	planner := &nativeBackendPlanner{edgeWeights: make([]uint64, 0, 3)}
	if got := retainNativeBackendPlannerWithin(planner, 24); got != planner {
		t.Fatal("planner at retention limit was released")
	}
	if got := retainNativeBackendPlannerWithin(planner, 23); got != nil {
		t.Fatal("planner above retention limit was retained")
	}
	if got := retainNativeBackendPlannerWithin(nil, 0); got != nil {
		t.Fatal("nil planner was replaced")
	}
}

func TestReleaseNativeBackendPlanningScratchAbove(t *testing.T) {
	planner := &nativeBackendPlanner{
		locals:            railssa.LocalSSA{EntryValues: make([]railssa.EnvValueID, 1)},
		flow:              railssa.ValueFlow{Values: make([]railssa.FlowValue, 1)},
		metadata:          railssa.Metadata{Instructions: make([]railssa.InstructionMetadata, 1)},
		pressure:          railssa.PressurePlan{Blocks: make([]railssa.BlockPressure, 1)},
		candidateScratch:  new([2]nativeCandidateWorkspace),
		edgeWeights:       make([]uint64, 1),
		edgeObserved:      make([]bool, 1),
		blockBytes:        make([]uint32, 1),
		coldBlocks:        make([]bool, 1),
		immediateUses:     make([]uint32, 1),
		gcValues:          make([]railssa.GCValueFact, 1),
		amd64MemoryBounds: make([]nativeAMD64MemoryBoundUse, 1),
	}
	planner.plan.Pressure = &planner.pressure
	planner.semantic.Insts = make([]railssa.SemanticInst, 1)
	planner.plan.Semantic = &planner.semantic
	if planner.releasePlanningScratchAbove(planner.CapacityBytes()) {
		t.Fatal("planner at release threshold was trimmed")
	}
	if !planner.releasePlanningScratchAbove(0) {
		t.Fatal("planner above release threshold was retained")
	}
	if planner.locals.EntryValues != nil || planner.flow.Values != nil || planner.metadata.Instructions != nil || planner.pressure.Blocks != nil || planner.plan.Pressure != nil ||
		planner.candidateScratch != nil || planner.edgeWeights != nil || planner.edgeObserved != nil || planner.blockBytes != nil || planner.coldBlocks != nil ||
		planner.immediateUses != nil || planner.gcValues != nil || planner.amd64MemoryBounds != nil {
		t.Fatalf("planning scratch retained: %#v", planner)
	}
	if planner.plan.Semantic != &planner.semantic || len(planner.semantic.Insts) != 1 {
		t.Fatal("finalizer-owned semantic plan was released")
	}
	if (&nativeBackendPlanner{}).releasePlanningScratchAbove(0) {
		t.Fatal("empty planner reported released scratch")
	}
}

func TestReleaseExceptionalNativeBackendSSAConstructionScratch(t *testing.T) {
	planner := &nativeBackendPlanner{
		locals: railssa.LocalSSA{EntryValues: make([]railssa.EnvValueID, 8)},
		flow:   railssa.ValueFlow{Values: make([]railssa.FlowValue, 4)},
	}
	if !planner.releaseLocalSSAScratchAbove(0) {
		t.Fatal("exceptional local SSA scratch was retained")
	}
	if planner.locals.EntryValues != nil || !planner.exceptionalFunction || planner.peakCapacityBytes == 0 || planner.peakRailSSA.LocalSSA == 0 {
		t.Fatalf("local SSA release state = %#v", planner)
	}
	planner.releaseValueFlowScratch()
	if planner.flow.Values != nil {
		t.Fatal("exceptional value-flow scratch was retained")
	}
	if !planner.releasePlanningScratchAbove(^uint64(0)) {
		t.Fatal("exceptional planner was retained after early scratch release")
	}
}

func TestNativeBackendPlannerReservesVerifiedCollectorRootSlots(t *testing.T) {
	importEntry := append(wasmtest.Name("env"), wasmtest.Name("tick")...)
	importEntry = append(importEntry, 0)
	importEntry = append(importEntry, wasmtest.ULEB(0)...)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.AnyRef}, []wasm.ValType{wasm.AnyRef}),
		)),
		wasmtest.Section(2, wasmtest.Vec(importEntry)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0x00, 0x20, 0x00, 0x0b}))),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	stack, err := railssa.BuildStackFunc(m, 0)
	if err != nil {
		t.Fatal(err)
	}
	target, err := corecompiler.HostTarget(corecompiler.TargetNative)
	if err != nil {
		t.Fatal(err)
	}
	var planner nativeBackendPlanner
	plan, err := planner.Plan(stack, target)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Roots == nil || plan.Roots.SlotCount != 1 || len(plan.Roots.Sites) != 1 || plan.Roots.Sites[0].Count != 1 || plan.Frame.RootBytes != 8 {
		t.Fatalf("native root plan=%#v frame=%#v", plan.Roots, plan.Frame)
	}
}

func TestNativeBackendPlannerAdmitsMixedVectorAndReferenceValues(t *testing.T) {
	importEntry := append(wasmtest.Name("env"), wasmtest.Name("tick")...)
	importEntry = append(importEntry, 0)
	importEntry = append(importEntry, wasmtest.ULEB(0)...)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.AnyRef, wasm.V128}, []wasm.ValType{wasm.AnyRef, wasm.V128}),
		)),
		wasmtest.Section(2, wasmtest.Vec(importEntry)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x10, 0x00,
			0x20, 0x00,
			0x20, 0x01,
			0x0b,
		}))),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	stack, err := railssa.BuildStackFunc(m, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !stack.HasReferences || !stackHasV128Value(stack) {
		t.Fatalf("mixed stack flags: references=%t vector-value=%t", stack.HasReferences, stackHasV128Value(stack))
	}
	if !railMachCandidate(stack, true) {
		t.Fatal("mixed vector/reference function was not admitted to RailMach")
	}
	target, err := corecompiler.HostTarget(corecompiler.TargetNative)
	if err != nil {
		t.Fatal(err)
	}
	var planner nativeBackendPlanner
	plan, err := planner.Plan(stack, target)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Machine == nil || plan.Allocation == nil || plan.Roots == nil || plan.Roots.SlotCount != 1 || len(plan.Roots.Sites) != 1 || plan.Roots.Sites[0].Count != 1 {
		t.Fatalf("incomplete mixed vector/reference plan: %#v", plan)
	}
	var metrics Metrics
	compiled, err := (Compiler{Metrics: &metrics}).Compile(corecompiler.Input{Module: m, Source: source, Target: target})
	if err != nil {
		t.Fatal(err)
	}
	if len(metrics.Functions) != 1 || !metrics.Functions[0].RailMachFinalized {
		t.Fatalf("mixed vector/reference emitter metrics: %#v", metrics.Functions)
	}
	if len(compiled.GCCallsites) != 1 || compiled.GCCallsites[0].RootCount != 1 || len(compiled.GCRoots) != 1 {
		t.Fatalf("mixed vector/reference roots: callsites=%#v roots=%#v", compiled.GCCallsites, compiled.GCRoots)
	}
}

func TestNativeBackendPlannerAllocatesOnlyRequiredPostRAScratch(t *testing.T) {
	var planner nativeBackendPlanner
	if !planner.preparePostRAScratch(railmach.TargetARM64, 64, []railmach.Rewrite{{Kind: railmach.RewriteARM64CompareBranch}}) {
		t.Fatal("ARM64 compare/branch was not recognized as realizable")
	}
	if len(planner.postRAFusionWith.narrow) != 64 || len(planner.postRAFusionWith.wide) != 0 || planner.postRAPairWith.capacityBytes() != 0 || planner.postRASkip.capacityBytes() != 0 || planner.postRAForwardFrom.capacityBytes() != 0 || planner.postRAMemoryFrom.capacityBytes() != 0 || planner.postRARepeatFirst.capacityBytes() != 0 {
		t.Fatalf("compare/branch scratch = fusion:%#v pair:%#v skip:%#v forward:%#v memory:%#v repeat:%#v", planner.postRAFusionWith, planner.postRAPairWith, planner.postRASkip, planner.postRAForwardFrom, planner.postRAMemoryFrom, planner.postRARepeatFirst)
	}
	planner.postRAFusionWith.set(1, 63)
	planner.postRAFusionWith.set(63, 1)
	if related, ok := planner.postRAFusionWith.get(1); !ok || related != 63 {
		t.Fatalf("compact fusion relation = %d/%t, want 63/true", related, ok)
	}
	if !planner.preparePostRAScratch(railmach.TargetARM64, 32, []railmach.Rewrite{{Kind: railmach.RewriteARM64Pair}}) {
		t.Fatal("ARM64 pair was not recognized as realizable")
	}
	if len(planner.postRAPairWith.narrow) != 32 || len(planner.postRAPairWith.wide) != 0 || !planner.postRASkip.prepared(32) || len(planner.postRAFusionWith.narrow) != 0 || len(planner.postRAFusionWith.wide) != 0 {
		t.Fatalf("pair scratch = pair:%#v skip:%#v fusion:%#v", planner.postRAPairWith, planner.postRASkip, planner.postRAFusionWith)
	}
	planner.postRAPairWith.set(1, 31)
	if related, ok := planner.postRAPairWith.get(1); !ok || related != 31 {
		t.Fatalf("compact pair relation = %d/%t, want 31/true", related, ok)
	}
	if planner.preparePostRAScratch(railmach.TargetARM64, 32, []railmach.Rewrite{{Kind: railmach.RewriteAMD64MemoryFold}}) {
		t.Fatal("cross-target rewrite allocated realization scratch")
	}
	if !planner.preparePostRAScratch(railmach.TargetARM64, 32, []railmach.Rewrite{{Kind: railmach.RewriteLoadStoreForward}}) || len(planner.postRAForwardFrom.narrow) != 32 || len(planner.postRAForwardFrom.wide) != 0 {
		t.Fatalf("compact forward scratch = %#v", planner.postRAForwardFrom)
	}
	planner.postRAForwardFrom.set(31, 1)
	if related, ok := planner.postRAForwardFrom.get(31); !ok || related != 1 {
		t.Fatalf("compact forward relation = %d/%t, want 1/true", related, ok)
	}
	if !planner.preparePostRAScratch(railmach.TargetAMD64, 32, []railmach.Rewrite{{Kind: railmach.RewriteAMD64MemoryFold}}) || len(planner.postRAMemoryFrom.narrow) != 32 || len(planner.postRAMemoryFrom.wide) != 0 {
		t.Fatalf("compact memory-fold scratch = %#v", planner.postRAMemoryFrom)
	}
	if !planner.preparePostRAScratch(railmach.TargetARM64, 32, []railmach.Rewrite{{Kind: railmach.RewriteARM64RepeatedAdd}}) || len(planner.postRARepeatFirst.narrow) != 32 || len(planner.postRARepeatFirst.wide) != 0 {
		t.Fatalf("compact repeated-add scratch = %#v", planner.postRARepeatFirst)
	}
	if !planner.preparePostRAScratch(railmach.TargetARM64, 32, []railmach.Rewrite{{Kind: railmach.RewriteARM64PrePostIndex, Second: 1}}) || len(planner.postRAPostIndexWith.narrow) != 32 || len(planner.postRAPostIndexWith.wide) != 0 {
		t.Fatalf("compact post-index scratch = %#v", planner.postRAPostIndexWith)
	}
	if !planner.preparePostRAScratch(railmach.TargetARM64, 1<<16, []railmach.Rewrite{{Kind: railmach.RewriteARM64CompareBranch}}) || len(planner.postRAFusionWith.narrow) != 0 || len(planner.postRAFusionWith.wide) != 1<<16 {
		t.Fatalf("large fusion scratch = %#v", planner.postRAFusionWith)
	}
	planner.postRAFusionWith.set(1, 1<<16-1)
	if related, ok := planner.postRAFusionWith.get(1); !ok || related != 1<<16-1 {
		t.Fatalf("wide fusion relation = %d/%t, want %d/true", related, ok, 1<<16-1)
	}
	if !planner.preparePostRAScratch(railmach.TargetARM64, 1<<16, []railmach.Rewrite{{Kind: railmach.RewriteARM64Pair}}) || len(planner.postRAPairWith.narrow) != 0 || len(planner.postRAPairWith.wide) != 1<<16 {
		t.Fatalf("large pair scratch = %#v", planner.postRAPairWith)
	}
	planner.postRAPairWith.set(1, 1<<16-1)
	if related, ok := planner.postRAPairWith.get(1); !ok || related != 1<<16-1 {
		t.Fatalf("wide pair relation = %d/%t, want %d/true", related, ok, 1<<16-1)
	}
	if !planner.preparePostRAScratch(railmach.TargetAMD64, 1<<16, []railmach.Rewrite{{Kind: railmach.RewriteLoadStoreForward}}) || len(planner.postRAForwardFrom.narrow) != 0 || len(planner.postRAForwardFrom.wide) != 1<<16 {
		t.Fatalf("large forward scratch = %#v", planner.postRAForwardFrom)
	}
	planner.postRAForwardFrom.set(1<<16-1, 1)
	if related, ok := planner.postRAForwardFrom.get(1<<16 - 1); !ok || related != 1 {
		t.Fatalf("wide forward relation = %d/%t, want 1/true", related, ok)
	}
}

func TestNativeBitSetCoversWordBoundaryAndClear(t *testing.T) {
	var set nativeBitSet
	set.prepare(65, true)
	for _, bit := range []uint32{0, 63, 64} {
		set.set(bit, true)
		if !set.has(bit) {
			t.Fatalf("bit %d was not retained", bit)
		}
		set.set(bit, false)
		if set.has(bit) {
			t.Fatalf("bit %d was not cleared", bit)
		}
	}
	if set.has(65) || !set.prepared(65) || set.capacityBytes() != 16 {
		t.Fatalf("bitset state = %#v", set)
	}
	set.prepare(65, false)
	if set.capacityBytes() != 16 || set.has(0) {
		t.Fatalf("released bitset state = %#v", set)
	}
}

func TestNativeCallArgumentBytesCoversLargestCanonicalVector(t *testing.T) {
	machine := &railmach.Func{
		Insts: []railmach.Inst{
			{Op: wasm.InstrCall, OperandStart: 0, OperandCount: 11},
			{Op: wasm.InstrCallIndirect, OperandStart: 11, OperandCount: 3},
			{Op: wasm.InstrCall, Result: 1},
		},
		Operands: make([]railmach.Operand, 14),
		VRegs:    make([]railmach.VRegData, 16),
	}
	for index := range machine.Operands {
		machine.Operands[index].Reg = railmach.VReg(index + 1)
		machine.VRegs[index+1].Type = railmach.TypeI64
	}
	machine.VRegs[1].Type = railmach.TypeV128
	machine.Insts[2].Result = 15
	machine.VRegs[15].Type = railmach.TypeV128
	if got := nativeCallArgumentBytes(machine); got != 96 {
		t.Fatalf("call argument bytes = %d, want 96", got)
	}
}

func TestNativeBackendPlannerConsumesVerifiedGVNAliases(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0x00, 0x41, 0x07, 0x6a, 0x1a,
			0x20, 0x00, 0x41, 0x07, 0x6a,
			0x0b,
		}))),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	stack, err := railssa.BuildStackFunc(m, 0)
	if err != nil {
		t.Fatal(err)
	}
	target, err := corecompiler.HostTarget(corecompiler.TargetNative)
	if err != nil {
		t.Fatal(err)
	}
	var planner nativeBackendPlanner
	plan, err := planner.Plan(stack, target)
	if err != nil {
		t.Fatal(err)
	}
	elided := 0
	for _, value := range plan.Machine.VRegs {
		if value.Flags&railmach.VRegElided != 0 {
			elided++
		}
	}
	if elided < 2 {
		t.Fatalf("elided machine values = %d, want repeated constant and add", elided)
	}
}

func TestNativeIntegerConstantChecksExactDefinition(t *testing.T) {
	machine := &railmach.Func{
		Insts: []railmach.Inst{{Op: wasm.InstrI32Const, Aux: 0, Result: 1}},
		VRegs: []railmach.VRegData{{}, {Def: 3}},
	}
	plan := &nativeBackendPlan{Machine: machine}
	if value, ok := nativeIntegerConstant(plan, 1); !ok || value != 0 {
		t.Fatalf("constant = (%d, %v), want (0, true)", value, ok)
	}
	machine.Insts[0].Result = 0
	if _, ok := nativeIntegerConstant(plan, 1); ok {
		t.Fatal("accepted mismatched defining result")
	}
}

func TestNativeImmediateCombinationsFoldRepeatedRotateCounts(t *testing.T) {
	machine := &railmach.Func{
		Insts: []railmach.Inst{
			{Op: wasm.InstrI32Const, Aux: 12, Result: 1},
			{Op: wasm.InstrI32Rotr, Result: 3, OperandStart: 0, OperandCount: 2},
			{Op: wasm.InstrI32Rotr, Result: 4, OperandStart: 2, OperandCount: 2},
		},
		Operands: []railmach.Operand{{Reg: 2}, {Reg: 1}, {Reg: 3}, {Reg: 1}},
		VRegs:    make([]railmach.VRegData, 5),
	}
	machine.VRegs[1] = railmach.VRegData{Def: 3, Flags: railmach.VRegRematerializable}
	plan := &nativeBackendPlan{Machine: machine, Selection: &railmach.SelectionPlan{}}
	var producers nativeInstructionRelation
	var skipped nativeBitSet
	uses := make([]uint32, len(machine.VRegs))
	buildNativeImmediateCombinations(plan, &producers, &skipped, uses)
	producer1, ok1 := producers.get(1)
	producer2, ok2 := producers.get(2)
	if !ok1 || producer1 != 0 || !ok2 || producer2 != 0 || !skipped.has(0) || uses[1] != 2 {
		t.Fatalf("producers=%v skipped=%v uses=%v", producers, skipped, uses)
	}
	machine.Target = railmach.TargetARM64
	states := make([]uint32, len(machine.VRegs))
	applyNativeARM64ShiftImmediateRematerialization(machine, states)
	if machine.Operands[1].Flags&railmach.OperandColdRemat == 0 || machine.Operands[3].Flags&railmach.OperandColdRemat == 0 {
		t.Fatalf("rotate operands were not removed from allocation liveness: %#v", machine.Operands)
	}
	machine.Results = []railmach.VReg{1}
	machine.Operands[1].Flags &^= railmach.OperandColdRemat
	machine.Operands[3].Flags &^= railmach.OperandColdRemat
	applyNativeARM64ShiftImmediateRematerialization(machine, states)
	if machine.Operands[1].Flags&railmach.OperandColdRemat != 0 || machine.Operands[3].Flags&railmach.OperandColdRemat != 0 {
		t.Fatalf("escaping rotate constant was removed from liveness: %#v", machine.Operands)
	}
	buildNativeImmediateCombinations(plan, &producers, &skipped, uses)
	if skipped.has(0) {
		t.Fatal("function-result constant was elided")
	}
}

func TestNativeImmediateCombinationsFoldRepeatedVectorShiftCounts(t *testing.T) {
	machine := &railmach.Func{
		Target: railmach.TargetARM64,
		Insts: []railmach.Inst{
			{Op: wasm.InstrI32Const, Aux: 12, Result: 1},
			{Op: wasm.InstrI32x4ShrU, Result: 3, OperandStart: 0, OperandCount: 2},
			{Op: wasm.InstrI32x4Shl, Result: 4, OperandStart: 2, OperandCount: 2},
		},
		Operands: []railmach.Operand{{Reg: 2}, {Reg: 1}, {Reg: 3}, {Reg: 1}},
		VRegs:    make([]railmach.VRegData, 5),
	}
	machine.VRegs[1] = railmach.VRegData{Def: 3, Flags: railmach.VRegRematerializable}
	plan := &nativeBackendPlan{Machine: machine, Selection: &railmach.SelectionPlan{}}
	var producers nativeInstructionRelation
	var skipped nativeBitSet
	uses := make([]uint32, len(machine.VRegs))
	buildNativeImmediateCombinations(plan, &producers, &skipped, uses)
	producer1, ok1 := producers.get(1)
	producer2, ok2 := producers.get(2)
	if !ok1 || producer1 != 0 || !ok2 || producer2 != 0 || !skipped.has(0) || uses[1] != 2 {
		t.Fatalf("producers=%v skipped=%v uses=%v", producers, skipped, uses)
	}
}

func TestNativeImmediateCombinationsRejectStaleMultiplyAddRelation(t *testing.T) {
	machine := &railmach.Func{
		Target: railmach.TargetARM64,
		Insts: []railmach.Inst{
			{Op: wasm.InstrI32Const, Aux: 7, Result: 1},
			{Op: railmach.OpARM64I32Madd, Result: 5, OperandStart: 0, OperandCount: 3},
		},
		Operands: []railmach.Operand{{Reg: 2}, {Reg: 3}, {Reg: 1}},
		VRegs:    make([]railmach.VRegData, 6),
	}
	selection := &railmach.SelectionPlan{Combinations: []railmach.Combination{{Kind: railmach.CombineImmediate, Producer: 0, Consumer: 1}}}
	plan := &nativeBackendPlan{Machine: machine, Selection: selection}
	var producers nativeInstructionRelation
	var skipped nativeBitSet
	uses := make([]uint32, len(machine.VRegs))
	buildNativeImmediateCombinations(plan, &producers, &skipped, uses)
	if producers.has(1) || skipped.has(0) {
		t.Fatalf("stale multiply-add relation admitted: producers=%v skipped=%v", producers, skipped)
	}
}

func TestNativeImmediateCombinationRetainsEdgeTransferConstant(t *testing.T) {
	machine := &railmach.Func{
		Insts: []railmach.Inst{
			{Op: wasm.InstrI32Const, Aux: 7, Result: 1},
			{Op: wasm.InstrI32LtS, Result: 3, OperandStart: 0, OperandCount: 2},
		},
		Operands:  []railmach.Operand{{Reg: 2}, {Reg: 1}},
		VRegs:     make([]railmach.VRegData, 4),
		Transfers: []railmach.EdgeTransfer{{Src: 1, Dst: 3}},
	}
	selection := &railmach.SelectionPlan{Combinations: []railmach.Combination{{Kind: railmach.CombineImmediate, Producer: 0, Consumer: 1}}}
	plan := &nativeBackendPlan{Machine: machine, Selection: selection}
	var producers nativeInstructionRelation
	var skipped nativeBitSet
	uses := make([]uint32, len(machine.VRegs))
	buildNativeImmediateCombinations(plan, &producers, &skipped, uses)
	if skipped.has(0) || uses[1] != 2 {
		t.Fatalf("edge-transfer constant skipped=%v uses=%v producers=%v", skipped, uses, producers)
	}
}

func TestARM64RepeatedImmediateEligibility(t *testing.T) {
	for _, test := range []struct {
		kind  wasm.InstrKind
		value uint64
		want  bool
	}{
		{wasm.InstrI64And, 0x0000ffff0000ffff, true},
		{wasm.InstrI32Or, 0x00ff00ff, true},
		{wasm.InstrI64Xor, 0x0101010101010101, true},
		{wasm.InstrI64And, 0, false},
		{wasm.InstrI64Add, 0xff, true},
		{wasm.InstrI32Sub, 4096, true},
		{wasm.InstrI32Sub, 0xfff00000, true},
		{wasm.InstrI64Add, 1048576, true},
		{wasm.InstrI32Add, 4097, false},
	} {
		if got := arm64RepeatedImmediateEncodable(test.kind, test.value); got != test.want {
			t.Fatalf("%s %#x eligibility = %t, want %t", test.kind, test.value, got, test.want)
		}
	}
}

func TestARM64ExtendedAddSubImmediateRequiresHotBlock(t *testing.T) {
	machine := &railmach.Func{
		Target: railmach.TargetARM64,
		Insts: []railmach.Inst{
			{Op: wasm.InstrI32Const, Aux: 0xfff00000, Result: 1},
			{Op: wasm.InstrI32Sub, Result: 3, OperandStart: 0, OperandCount: 2},
		},
		Operands: []railmach.Operand{{Reg: 2}, {Reg: 1}},
		VRegs:    []railmach.VRegData{{}, {Def: 3, Flags: railmach.VRegRematerializable}, {}, {}},
		Blocks:   []railmach.Block{{InstStart: 0, InstCount: 2, Weight: 8}},
	}
	plan := &nativeBackendPlan{Machine: machine, Selection: &railmach.SelectionPlan{}}
	var producers nativeInstructionRelation
	var skipped nativeBitSet
	uses := make([]uint32, len(machine.VRegs))
	buildNativeImmediateCombinations(plan, &producers, &skipped, uses)
	buildNativeARM64LogicalImmediateCombinations(plan, &producers, &skipped, uses)
	if producers.has(1) {
		t.Fatal("extended immediate folded in a low-weight block")
	}
	machine.Blocks[0].Weight = 64
	buildNativeImmediateCombinations(plan, &producers, &skipped, uses)
	buildNativeARM64LogicalImmediateCombinations(plan, &producers, &skipped, uses)
	producer, ok := producers.get(1)
	if !ok || producer != 0 || !skipped.has(0) {
		t.Fatalf("hot extended immediate producers=%v skipped=%v", producers, skipped)
	}
}

func TestNativeEdgeConstantRematerializationRequiresPhysicalMoves(t *testing.T) {
	register := func(index uint16) railmach.Location {
		return railmach.Location{Kind: railmach.LocationRegister, Bank: railmach.BankGPR, Index: index}
	}
	machine := &railmach.Func{
		Insts:     []railmach.Inst{{Op: wasm.InstrI64Const, Aux: 7, Result: 1}},
		VRegs:     []railmach.VRegData{{}, {Type: railmach.TypeI64, Bank: railmach.BankGPR, Def: 3, Flags: railmach.VRegRematerializable}, {Type: railmach.TypeI64, Bank: railmach.BankGPR}},
		Transfers: []railmach.EdgeTransfer{{Src: 1, Dst: 2}},
	}
	plan := &nativeBackendPlan{
		Machine:    machine,
		Allocation: &railmach.GreedyAllocation{Allocation: railmach.Allocation{Locations: []railmach.Location{{}, register(0), register(1)}}},
		Exit:       &railmach.SSAExit{Moves: []railmach.PhysicalMove{{Src: register(0), Dst: register(1), Reg: 1, Kind: railmach.MoveCopy, Bank: railmach.BankGPR}}},
	}
	var skipped nativeBitSet
	skipped.prepare(1, true)
	buildNativeEdgeConstantRematerialization(plan, &skipped, []uint32{0, 1, 0})
	if !skipped.has(0) {
		t.Fatal("edge-only constant was not selected for direct rematerialization")
	}
	skipped.set(0, false)
	buildNativeEdgeConstantRematerialization(plan, &skipped, []uint32{0, 2, 0})
	if skipped.has(0) {
		t.Fatal("constant with a non-edge use was elided")
	}
	plan.Exit.Moves = nil
	buildNativeEdgeConstantRematerialization(plan, &skipped, []uint32{0, 1, 0})
	if skipped.has(0) {
		t.Fatal("coalesced edge constant lost its defining materialization")
	}
}

func TestNativeImmediatePlanSkipsFullyRematerializedDefinition(t *testing.T) {
	machine := &railmach.Func{
		Insts: []railmach.Inst{
			{Op: wasm.InstrI64Const, Aux: 0x12345678, Result: 1},
			{Op: wasm.InstrI64Add, Result: 3, OperandStart: 0, OperandCount: 2},
		},
		Operands: []railmach.Operand{{Reg: 2, Bank: railmach.BankGPR}, {Reg: 1, Bank: railmach.BankGPR}},
		VRegs: []railmach.VRegData{
			{},
			{Type: railmach.TypeI64, Bank: railmach.BankGPR, Def: 3, Flags: railmach.VRegRematerializable},
			{Type: railmach.TypeI64, Bank: railmach.BankGPR},
			{Type: railmach.TypeI64, Bank: railmach.BankGPR, Def: 9},
		},
	}
	plan := &nativeBackendPlan{
		Machine:   machine,
		Selection: &railmach.SelectionPlan{},
		Allocation: &railmach.GreedyAllocation{Allocation: railmach.Allocation{Locations: []railmach.Location{
			{},
			{Kind: railmach.LocationRematerialize, Bank: railmach.BankGPR},
			{Kind: railmach.LocationRegister, Bank: railmach.BankGPR},
			{Kind: railmach.LocationRegister, Bank: railmach.BankGPR, Index: 1},
		}}},
	}
	var producers nativeInstructionRelation
	var skipped nativeBitSet
	uses := make([]uint32, len(machine.VRegs))
	buildNativeImmediateCombinations(plan, &producers, &skipped, uses)
	if !skipped.has(0) {
		t.Fatal("fully rematerialized constant definition was emitted")
	}
}

func TestNativeScheduleScoreBoundsLargeLatencyPreference(t *testing.T) {
	pressure := railmach.ScheduleScore{Kind: railmach.ScheduleKindPressure, WeightedSpillDebt: 300}
	latency := railmach.ScheduleScore{Kind: railmach.ScheduleKindLatencyFusion, WeightedSpillDebt: 400}
	if !nativeScheduleScoreBetter(corecompiler.ObjectiveSpeed, railmach.TargetAMD64, 1024, false, latency, pressure) || nativeScheduleScoreBetter(corecompiler.ObjectiveSpeed, railmach.TargetAMD64, 1024, false, pressure, latency) {
		t.Fatal("bounded large-function latency preference was not stable across candidate order")
	}
	latency.WeightedSpillDebt++
	if nativeScheduleScoreBetter(corecompiler.ObjectiveSpeed, railmach.TargetAMD64, 1024, false, latency, pressure) {
		t.Fatal("latency schedule beyond the spill bound was preferred")
	}
	latency.WeightedSpillDebt = 400
	if nativeScheduleScoreBetter(corecompiler.ObjectiveSize, railmach.TargetAMD64, 1024, false, latency, pressure) || nativeScheduleScoreBetter(corecompiler.ObjectiveSpeed, railmach.TargetAMD64, 1023, false, latency, pressure) {
		t.Fatal("latency preference escaped the speed/large-function boundary")
	}
}

func TestNativeSegmentedAllocationRequiresStrictDebtWin(t *testing.T) {
	retained := railmach.ScheduleScore{WeightedSpillDebt: 10, PhysicalCopies: 2, CopyCycles: 1, FixedRepairs: 1}
	allocation := &railmach.GreedyAllocation{
		Allocation: railmach.Allocation{SpillSlots: 1, LiveSegmentRanges: []railmach.LiveSegmentRange{{Reg: 1, SegmentCount: 2}}},
		Metrics:    railmach.GreedyMetrics{PreservationCost: 2},
	}
	if !nativeSegmentedAllocationBetter(
		testScheduleScore(9, 2, 1, 1), retained, allocation,
		testGreedyMetrics(2), 1, 1,
	) {
		t.Fatal("strict segmented debt improvement was rejected")
	}
	for name, candidate := range map[string]railmach.ScheduleScore{
		"equal debt":      testScheduleScore(10, 2, 1, 1),
		"physical copies": testScheduleScore(9, 3, 1, 1),
		"copy cycles":     testScheduleScore(9, 2, 2, 1),
		"fixed repairs":   testScheduleScore(9, 2, 1, 2),
	} {
		if nativeSegmentedAllocationBetter(candidate, retained, allocation, testGreedyMetrics(2), 1, 1) {
			t.Fatalf("segmented candidate with %s was accepted", name)
		}
	}
	allocation.Metrics.PreservationCost = 3
	if nativeSegmentedAllocationBetter(testScheduleScore(9, 2, 1, 1), retained, allocation, testGreedyMetrics(2), 1, 1) {
		t.Fatal("segmented candidate with higher preservation cost was accepted")
	}
	allocation.Metrics.PreservationCost = 2
	allocation.SpillSlots = 2
	if nativeSegmentedAllocationBetter(testScheduleScore(9, 2, 1, 1), retained, allocation, testGreedyMetrics(2), 1, 1) {
		t.Fatal("segmented candidate with more spill slots was accepted")
	}
	allocation.SpillSlots = 1
	if nativeSegmentedAllocationBetter(testScheduleScore(9, 2, 1, 1), retained, allocation, testGreedyMetrics(2), 1, 2) {
		t.Fatal("segmented candidate below the minimum debt reduction was accepted")
	}
}

func TestNativeSegmentedLoopCandidatesRequireMeasuredPayoff(t *testing.T) {
	acyclic := &railmach.Func{Blocks: []railmach.Block{{}, {}}}
	loop := &railmach.Func{Blocks: []railmach.Block{{}, {Flags: railssa.BlockLoopHeader}}}
	if got := nativeSegmentedMinimumDebtReduction(acyclic); got != 1 {
		t.Fatalf("acyclic minimum debt reduction = %d", got)
	}
	if got := nativeSegmentedMinimumDebtReduction(loop); got != 64 {
		t.Fatalf("loop minimum debt reduction = %d", got)
	}
	if !nativeShouldTrySegmentedLiveness(acyclic, railmach.ScheduleScore{WeightedSpillDebt: 1}) {
		t.Fatal("acyclic segmented opportunity was rejected")
	}
	if nativeShouldTrySegmentedLiveness(loop, railmach.ScheduleScore{WeightedSpillDebt: 1023}) {
		t.Fatal("low-payoff loop segmented opportunity was admitted")
	}
	if !nativeShouldTrySegmentedLiveness(loop, railmach.ScheduleScore{WeightedSpillDebt: 1024}) {
		t.Fatal("measured loop segmented opportunity was rejected")
	}
}

func TestNativeARM64PrePostIndexAvoidsScalarFloatBankCrossing(t *testing.T) {
	machine := &railmach.Func{
		Insts: []railmach.Inst{
			{Result: 1, Op: wasm.InstrF64Load, OperandStart: 0, OperandCount: 1},
			{Op: wasm.InstrF64Store, OperandStart: 1, OperandCount: 2},
			{Result: 3, Op: wasm.InstrI64Load, OperandStart: 3, OperandCount: 1},
			{Op: wasm.InstrI64Store, OperandStart: 4, OperandCount: 2},
		},
		Operands: []railmach.Operand{
			{Reg: 2},
			{Reg: 2}, {Reg: 1},
			{Reg: 2},
			{Reg: 2}, {Reg: 3},
		},
		VRegs: []railmach.VRegData{
			{},
			{Type: railmach.TypeF64, Bank: railmach.BankFPR},
			{Type: railmach.TypeI32, Bank: railmach.BankGPR},
			{Type: railmach.TypeI64, Bank: railmach.BankGPR},
		},
	}
	for _, instruction := range []uint32{0, 1} {
		if nativeARM64PrePostIndexProfitable(machine, railmach.Rewrite{First: instruction, Kind: railmach.RewriteARM64PrePostIndex}) {
			t.Fatalf("floating-point memory instruction %d accepted writeback addressing", instruction)
		}
	}
	for _, instruction := range []uint32{2, 3} {
		if !nativeARM64PrePostIndexProfitable(machine, railmach.Rewrite{First: instruction, Kind: railmach.RewriteARM64PrePostIndex}) {
			t.Fatalf("integer memory instruction %d rejected writeback addressing", instruction)
		}
	}
	if nativeARM64PrePostIndexProfitable(machine, railmach.Rewrite{First: 4, Kind: railmach.RewriteARM64PrePostIndex}) {
		t.Fatal("out-of-range rewrite accepted")
	}
}

func TestNativeScheduleScorePrefersBoundedLoopInvariantMotionForSpeed(t *testing.T) {
	stable := railmach.ScheduleScore{Kind: railmach.ScheduleKindSourceStable, PhysicalCopies: 3}
	hoisted := railmach.ScheduleScore{Kind: railmach.ScheduleKindPressure, PhysicalCopies: 4, LoopInvariantOps: 1}
	if !nativeScheduleScoreBetter(corecompiler.ObjectiveSpeed, railmach.TargetARM64, 13, false, hoisted, stable) ||
		nativeScheduleScoreBetter(corecompiler.ObjectiveSpeed, railmach.TargetARM64, 13, false, stable, hoisted) {
		t.Fatal("bounded loop-invariant motion preference was not stable across candidate order")
	}
	hoisted.PhysicalCopies++
	if nativeScheduleScoreBetter(corecompiler.ObjectiveSpeed, railmach.TargetARM64, 13, false, hoisted, stable) {
		t.Fatal("loop-invariant motion with excess copies was preferred")
	}
	hoisted.PhysicalCopies--
	hoisted.WeightedSpillDebt = 1
	if nativeScheduleScoreBetter(corecompiler.ObjectiveSpeed, railmach.TargetARM64, 13, false, hoisted, stable) {
		t.Fatal("loop-invariant motion with added spill debt was preferred")
	}
	hoisted.WeightedSpillDebt = 0
	if nativeScheduleScoreBetter(corecompiler.ObjectiveSize, railmach.TargetARM64, 13, false, hoisted, stable) {
		t.Fatal("loop-invariant motion preference escaped the speed objective")
	}
}

func testScheduleScore(debt uint64, copies, cycles, repairs uint32) railmach.ScheduleScore {
	return railmach.ScheduleScore{WeightedSpillDebt: debt, PhysicalCopies: copies, CopyCycles: cycles, FixedRepairs: repairs}
}

func testGreedyMetrics(preservation uint64) railmach.GreedyMetrics {
	return railmach.GreedyMetrics{PreservationCost: preservation}
}

func TestNativeScheduleScorePrefersARM64FloatLatencyWithinSpillBound(t *testing.T) {
	pressure := railmach.ScheduleScore{Kind: railmach.ScheduleKindPressure, WeightedSpillDebt: 300, PhysicalCopies: 38}
	latency := railmach.ScheduleScore{Kind: railmach.ScheduleKindLatencyFusion, WeightedSpillDebt: 900, PhysicalCopies: 36}
	if !nativeScheduleScoreBetter(corecompiler.ObjectiveSpeed, railmach.TargetARM64, 386, true, latency, pressure) ||
		nativeScheduleScoreBetter(corecompiler.ObjectiveSpeed, railmach.TargetARM64, 386, true, pressure, latency) {
		t.Fatal("bounded ARM64 floating latency preference was not stable across candidate order")
	}
	latency.WeightedSpillDebt++
	if nativeScheduleScoreBetter(corecompiler.ObjectiveSpeed, railmach.TargetARM64, 386, true, latency, pressure) {
		t.Fatal("ARM64 floating latency schedule beyond the spill bound was preferred")
	}
	latency.WeightedSpillDebt = 900
	if nativeScheduleScoreBetter(corecompiler.ObjectiveSpeed, railmach.TargetAMD64, 386, true, latency, pressure) ||
		nativeScheduleScoreBetter(corecompiler.ObjectiveSpeed, railmach.TargetARM64, 386, false, latency, pressure) ||
		nativeScheduleScoreBetter(corecompiler.ObjectiveSpeed, railmach.TargetARM64, 1024, true, latency, pressure) {
		t.Fatal("ARM64 floating latency preference escaped its target, register-bank, or medium-function boundary")
	}
}

func TestRailMachAdmitsSupportedLoopFamilies(t *testing.T) {
	for _, test := range []struct {
		name         string
		instructions []wasm.InstrKind
		loopDepth    uint8
	}{
		{"established_arithmetic", []wasm.InstrKind{wasm.InstrI32Add}, 1},
		{"comparison", []wasm.InstrKind{wasm.InstrI32LtU}, 1},
		{"wide_recurrence", []wasm.InstrKind{wasm.InstrI64Add, wasm.InstrI32Sub}, 1},
		{"wide_multiply", []wasm.InstrKind{wasm.InstrI64Mul, wasm.InstrI64ShrU}, 1},
		{"wide_memory", []wasm.InstrKind{wasm.InstrI64Load, wasm.InstrI64Add, wasm.InstrI64Store}, 1},
		{"mutable_global", []wasm.InstrKind{wasm.InstrGlobalGet, wasm.InstrI64Add, wasm.InstrGlobalSet}, 1},
		{"masked_wrap", []wasm.InstrKind{wasm.InstrI32And, wasm.InstrI32WrapI64}, 1},
		{"reinterpret_roundtrip", []wasm.InstrKind{wasm.InstrI32And, wasm.InstrI32WrapI64, wasm.InstrI64ReinterpretF64, wasm.InstrF64ReinterpretI64}, 1},
		{"f64_memory_arithmetic", []wasm.InstrKind{wasm.InstrF64Load, wasm.InstrF64Mul, wasm.InstrF64Add, wasm.InstrF64Store}, 1},
		{"f64_sqrt_conversion", []wasm.InstrKind{wasm.InstrF64ConvertI32U, wasm.InstrF64Sqrt, wasm.InstrF64Add}, 1},
		{"saturating_conversion", []wasm.InstrKind{wasm.InstrF64Load, wasm.InstrI32TruncSatF64S}, 1},
		{"bulk_memory", []wasm.InstrKind{wasm.InstrF64Load, wasm.InstrMemoryFill}, 1},
		{"structured_if", []wasm.InstrKind{wasm.InstrI32LtU, wasm.InstrIf}, 1},
		{"nested_loop", []wasm.InstrKind{wasm.InstrI32LtU}, 2},
		{"nested_loop_with_call", []wasm.InstrKind{wasm.InstrI32LtU, wasm.InstrCall}, 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			stack := &railssa.StackFunc{MaxLoopDepth: test.loopDepth}
			for _, kind := range test.instructions {
				stack.Instrs = append(stack.Instrs, railssa.StackInstr{Kind: kind})
			}
			if !railMachCandidate(stack, false) {
				t.Fatal("supported loop family was not admitted to RailMach")
			}
		})
	}
}

func TestRailMachRejectionReasonIdentifiesFirstUnsupportedOperation(t *testing.T) {
	if reason := railMachRejectionReason(&railssa.StackFunc{Instrs: []railssa.StackInstr{{Kind: wasm.InstrI32Add}}}, false); reason != "" {
		t.Fatalf("supported operation reason = %q", reason)
	}
	stack := &railssa.StackFunc{Instrs: []railssa.StackInstr{
		{Kind: wasm.InstrI32Add},
		{Kind: wasm.InstrTableGet},
		{Kind: wasm.InstrTableSet},
	}}
	if reason := railMachRejectionReason(stack, false); reason != "unsupported-op:TableGet" {
		t.Fatalf("unsupported operation reason = %q", reason)
	}
}

func TestRailMachRetainsFastStructuredPathForGiantTrappingConversion(t *testing.T) {
	stack := &railssa.StackFunc{Instrs: make([]railssa.StackInstr, nativeGiantStructuredInstructions)}
	stack.Instrs[0].Kind = wasm.InstrI32TruncF64S
	if railMachCandidate(stack, false) {
		t.Fatal("giant trapping-conversion function entered the quality pipeline")
	}
	if reason := railMachRejectionReason(stack, false); reason != "fast-structured:giant-trapping-conversion" {
		t.Fatalf("giant-function reason = %q", reason)
	}
	stack.Instrs = stack.Instrs[:nativeGiantStructuredInstructions-1]
	if !railMachCandidate(stack, false) {
		t.Fatal("function below giant threshold missed the machine pipeline")
	}
}

func TestRailMachAdmitsScalarFunctionInSIMDModule(t *testing.T) {
	stack := &railssa.StackFunc{
		MaxLoopDepth: 1,
		Instrs:       []railssa.StackInstr{{Kind: wasm.InstrI32LtU}},
	}
	if !railMachCandidate(stack, true) {
		t.Fatal("scalar function in SIMD module did not remain a RailMach candidate")
	}
}

func TestRailMachAdmitsMixedSIMDBranchCastFunction(t *testing.T) {
	stack := &railssa.StackFunc{
		HasV128:       true,
		HasReferences: true,
		BranchCasts:   []railssa.BranchCastImmediate{{}},
	}
	if !railMachCandidate(stack, true) {
		t.Fatal("mixed SIMD/branch-cast function did not enter RailMach")
	}
}

func TestRailMachAdmitsQualifiedV128Boundaries(t *testing.T) {
	foundation := &railssa.StackFunc{
		HasV128:     true,
		ResultTypes: []wasm.ValType{wasm.V128},
		Instrs: []railssa.StackInstr{
			{Kind: wasm.InstrI32Const}, {Kind: wasm.InstrV128Load}, {Kind: wasm.InstrV128Const},
			{Kind: wasm.InstrV128Xor}, {Kind: wasm.InstrV128Store},
		},
	}
	if !railMachCandidate(foundation, true) {
		t.Fatal("internal v128 foundation function did not enter RailMach")
	}
	boundary := *foundation
	boundary.Params = []wasm.ValType{wasm.V128}
	boundary.Locals = []wasm.ValType{wasm.V128}
	boundary.Results = []wasm.ValType{wasm.V128}
	if !railMachCandidate(&boundary, true) {
		t.Fatal("single-result v128 boundary did not enter RailMach")
	}
	directCall := *foundation
	directCall.Instrs = append([]railssa.StackInstr(nil), foundation.Instrs...)
	directCall.Instrs = append(directCall.Instrs, railssa.StackInstr{Kind: wasm.InstrCall})
	if !railMachCandidate(&directCall, true) {
		t.Fatal("direct-call v128 function did not enter RailMach")
	}
	indirectCall := *foundation
	indirectCall.Instrs = append([]railssa.StackInstr(nil), foundation.Instrs...)
	indirectCall.Instrs = append(indirectCall.Instrs, railssa.StackInstr{Kind: wasm.InstrCallIndirect})
	if !railMachCandidate(&indirectCall, true) {
		t.Fatal("indirect-call v128 function did not enter RailMach")
	}
	global := *foundation
	global.Globals = []wasm.ValType{wasm.V128}
	if !railMachCandidate(&global, true) {
		t.Fatal("v128 global function did not enter RailMach")
	}
	mixedResults := *foundation
	mixedResults.Results = []wasm.ValType{wasm.I32, wasm.V128, wasm.I64}
	if !railMachCandidate(&mixedResults, true) {
		t.Fatal("mixed multi-result v128 function did not enter RailMach")
	}
	local := *foundation
	local.Locals = []wasm.ValType{wasm.V128}
	if !railMachCandidate(&local, true) {
		t.Fatal("declared v128 local did not enter RailMach")
	}
	unreachable := *foundation
	unreachable.Instrs = append([]railssa.StackInstr{{Kind: wasm.InstrUnreachable}}, foundation.Instrs...)
	if !railMachCandidate(&unreachable, true) {
		t.Fatal("v128 function with unreachable region did not enter RailMach")
	}
}

func TestRailMachAdmitsLargeMultiCallScalarFunctions(t *testing.T) {
	for _, test := range []struct {
		name        string
		simd        bool
		moduleFuncs int
		want        bool
	}{
		{name: "scalar", want: true},
		{name: "simd", simd: true, want: true},
		{name: "large_module", moduleFuncs: 257, want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			stack := &railssa.StackFunc{MaxLoopDepth: 1, HasV128: test.simd, Instrs: make([]railssa.StackInstr, 257)}
			if test.moduleFuncs != 0 {
				stack.Module = &wasm.Module{Code: make([]wasm.Func, test.moduleFuncs)}
			}
			stack.Instrs[0].Kind = wasm.InstrCall
			stack.Instrs[1].Kind = wasm.InstrCall
			stack.Instrs[2].Kind = wasm.InstrI32LtU
			if got := railMachCandidate(stack, test.simd); got != test.want {
				t.Fatalf("RailMach candidate = %v, want %v", got, test.want)
			}
		})
	}
}

func TestNativeCallClobbersTreatStructuredCalleeAsFullyClobbering(t *testing.T) {
	machine := &railmach.Func{Insts: []railmach.Inst{{Op: wasm.InstrCall, Aux: 1}}}
	config := railmach.DefaultGreedyConfig(railmach.TargetARM64)
	overrides := nativeCallClobberOverrides(machine, 0, make([]railmach.ABIContract, 2), nil, nil, 0, config)
	if len(overrides) != 1 {
		t.Fatalf("overrides = %#v", overrides)
	}
	wantGPR := callerRegisterMask(config.Linear.GPRs)
	wantFPR := callerRegisterMask(config.Linear.FPRs)
	if overrides[0].Instruction != 0 || overrides[0].GPR != wantGPR || overrides[0].FPR != wantFPR {
		t.Fatalf("override = %#v, want full masks %#x/%#x", overrides[0], wantGPR, wantFPR)
	}
}

func TestNativeBackendPlannerKeepsLoopInvariantLiveAcrossResultIf(t *testing.T) {
	body := []byte{
		0x02, 0x40, 0x03, 0x40,
		0x20, 0, 0x20, 1, 0x4e, 0x0d, 1,
		0x20, 0, 0x41, 1, 0x6a, 0x41, 8, 0x6c,
		0x20, 0, 0x41, 1, 0x6a, 0x20, 1, 0x46,
		0x04, 0x7f, 0x41, 0, 0x05,
		0x20, 0, 0x41, 2, 0x6a, 0x41, 8, 0x6c, 0x0b,
		0x36, 2, 4,
		0x20, 0, 0x41, 1, 0x6a, 0x21, 0, 0x0c, 0,
		0x0b, 0x0b, 0x41, 8, 0x28, 2, 4, 0x0b,
	}
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	stack, err := railssa.BuildStackFunc(m, 0)
	if err != nil {
		t.Fatal(err)
	}
	target, err := corecompiler.HostTarget(corecompiler.TargetNative)
	if err != nil {
		t.Fatal(err)
	}
	var planner nativeBackendPlanner
	plan, err := planner.Plan(stack, target)
	if err != nil {
		t.Fatal(err)
	}
	var invariant railmach.VReg
	for id, data := range plan.Machine.VRegs {
		if data.Flags&railmach.VRegInitial != 0 && data.InitialLocal == 1 {
			invariant = railmach.VReg(id)
			break
		}
	}
	if invariant == 0 {
		t.Fatal("loop invariant parameter has no machine value")
	}
	var interval railmach.LiveInterval
	for _, candidate := range plan.Allocation.Intervals {
		if candidate.Reg == invariant {
			interval = candidate
			break
		}
	}
	if interval.Reg == 0 {
		t.Fatal("loop invariant parameter has no live interval")
	}
	header := plan.Machine.Blocks[2].InstStart * 6
	backedge := (plan.Machine.Blocks[6].InstStart + plan.Machine.Blocks[6].InstCount) * 6
	if interval.Start > header || interval.End < backedge {
		t.Fatalf("loop invariant interval = %#v, header=%d backedge=%d", interval, header, backedge)
	}
	tampered := plan.Allocation.Allocation
	tampered.Intervals = append([]railmach.LiveInterval(nil), tampered.Intervals...)
	for index := range tampered.Intervals {
		if tampered.Intervals[index].Reg == invariant {
			tampered.Intervals[index].End = header
			break
		}
	}
	if err := railmach.VerifyAllocation(plan.Machine, &tampered, railmach.DefaultLinearQConfig(plan.Machine.Target)); err == nil {
		t.Fatal("allocation verifier accepted loop invariant ending before the backedge")
	}
}

func TestNativeBackendPlannerCommitsNoWrapAddressFold(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0x00, 0x41, 0xff, 0x01, 0x71,
			0x41, 0x04, 0x6a,
			0x28, 0x02, 0x08,
			0x0b,
		}))),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	stack, err := railssa.BuildStackFunc(m, 0)
	if err != nil {
		t.Fatal(err)
	}
	target, err := corecompiler.HostTarget(corecompiler.TargetNative)
	if err != nil {
		t.Fatal(err)
	}
	var planner nativeBackendPlanner
	plan, err := planner.Plan(stack, target)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Selection.AddressFolds) != 1 {
		t.Fatalf("address folds = %#v", plan.Selection.AddressFolds)
	}
	fold := plan.Selection.AddressFolds[0]
	if plan.Machine.Insts[fold.Consumer].Aux != 12 || plan.Machine.VRegs[plan.Machine.Insts[fold.Producer].Result].Flags&railmach.VRegElided == 0 {
		t.Fatalf("committed fold=%#v consumer=%#v", fold, plan.Machine.Insts[fold.Consumer])
	}
}

func TestNativeBackendPlannerRejectsWrappingAddressFold(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0x00, 0x41, 0x04, 0x6a,
			0x28, 0x02, 0x08,
			0x0b,
		}))),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	stack, err := railssa.BuildStackFunc(m, 0)
	if err != nil {
		t.Fatal(err)
	}
	target, err := corecompiler.HostTarget(corecompiler.TargetNative)
	if err != nil {
		t.Fatal(err)
	}
	var planner nativeBackendPlanner
	plan, err := planner.Plan(stack, target)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Selection.AddressFolds) != 0 {
		t.Fatalf("wrapping address fold committed: %#v", plan.Selection.AddressFolds)
	}
}

func TestNativeBackendPlannerReusesMaskedInductionEmissionFacts(t *testing.T) {
	fn, _ := maskedLoopMemoryEmissionTestFunc(t)
	target, err := corecompiler.HostTarget(corecompiler.TargetNative)
	if err != nil {
		t.Fatal(err)
	}
	var planner nativeBackendPlanner
	plan, err := planner.Plan(fn.Structured, target)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Emission == nil || plan.Emission.ElidedBoundsChecks() != 1 {
		t.Fatalf("native reused emission plan = %#v", plan.Emission)
	}
}

func TestNativeBackendPlannerConsumesProfileEdgeLayout(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x04, 0x7f, 0x41, 1, 0x05, 0x41, 2, 0x0b, 0x0b}))),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	stack, err := railssa.BuildStackFunc(m, 0)
	if err != nil {
		t.Fatal(err)
	}
	target, err := corecompiler.HostTarget(corecompiler.TargetNative)
	if err != nil {
		t.Fatal(err)
	}
	var baselinePlanner nativeBackendPlanner
	baseline, err := baselinePlanner.Plan(stack, target)
	if err != nil {
		t.Fatal(err)
	}
	var hotFalse compilerprofile.EdgeCount
	var falseBlock railssa.BlockID
	found := false
	for _, edge := range baseline.Machine.Edges {
		if edge.Kind != railssa.EdgeFalse {
			continue
		}
		from := baseline.CFG.Blocks[edge.From]
		to := baseline.CFG.Blocks[edge.To]
		hotFalse = compilerprofile.EdgeCount{
			Site:   compilerprofile.Site{Function: 0, Offset: stack.Instrs[from.InstStart+from.InstCount-1].Offset},
			Target: stack.Instrs[to.InstStart].Offset, Count: 100,
		}
		falseBlock, found = edge.To, true
		break
	}
	if !found {
		t.Fatal("conditional plan has no false edge")
	}
	observations := &compilerprofile.Module{Version: compilerprofile.Version, Source: compilerprofile.SourceStatic, Phase: compilerprofile.PhaseSteady, EdgeCounts: []compilerprofile.EdgeCount{hotFalse}}
	var planner nativeBackendPlanner
	plan, err := planner.PlanProfile(stack, target, 0, observations)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Layout == nil || len(plan.Layout.Order) < 2 || plan.Layout.Order[1] != falseBlock || plan.Specialize == nil {
		t.Fatalf("profile layout = %#v specialization=%#v", plan.Layout, plan.Specialize)
	}
}

func TestNativeBackendPlannerShrinkWrapsProfileColdCalleeSave(t *testing.T) {
	body := []byte{0x01, 0x0a, 0x7e, 0x20, 0x00, 0x04, 0x7e} // ten i64 locals; if (result i64)
	for local := byte(1); local <= 10; local++ {
		body = append(body, 0x42, local, 0x21, local)
	}
	for local := byte(1); local <= 10; local++ {
		body = append(body, 0x20, local)
	}
	for range 9 {
		body = append(body, 0x7c)
	}
	body = append(body, 0x05, 0x42, 0x00, 0x0b, 0x0b)
	code := append(wasmtest.ULEB(uint32(len(body))), body...)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(code)),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	stack, err := railssa.BuildStackFunc(m, 0)
	if err != nil {
		t.Fatal(err)
	}
	// Fix the target so this register-pressure fixture has the same callee-save
	// contract when the test suite itself runs on ARM64.
	target := corecompiler.Target{GOOS: "linux", GOARCH: "amd64", Mode: corecompiler.TargetCompatibility}
	var baselinePlanner nativeBackendPlanner
	baseline, err := baselinePlanner.Plan(stack, target)
	if err != nil {
		t.Fatal(err)
	}
	var edges []compilerprofile.EdgeCount
	for _, edge := range baseline.Machine.Edges {
		if edge.Kind != railssa.EdgeTrue && edge.Kind != railssa.EdgeFalse {
			continue
		}
		from, to := baseline.CFG.Blocks[edge.From], baseline.CFG.Blocks[edge.To]
		targetOffset := uint32(0)
		if int(to.InstStart) < len(stack.Instrs) {
			targetOffset = stack.Instrs[to.InstStart].Offset
		}
		count := uint64(0)
		if edge.Kind == railssa.EdgeFalse {
			count = 100
		}
		edges = append(edges, compilerprofile.EdgeCount{
			Site:   compilerprofile.Site{Function: 0, Offset: stack.Instrs[from.InstStart+from.InstCount-1].Offset},
			Target: targetOffset, Count: count,
		})
	}
	if len(edges) != 2 {
		t.Fatalf("conditional profile edges = %d, want 2", len(edges))
	}
	observations := &compilerprofile.Module{Version: compilerprofile.Version, Source: compilerprofile.SourceStatic, Phase: compilerprofile.PhaseSteady, EdgeCounts: edges}
	var planner nativeBackendPlanner
	plan, err := planner.PlanProfile(stack, target, 0, observations)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.CalleeSaves) == 0 {
		t.Fatalf("no profile-cold callee save was shrink-wrapped: ABI=%#v allocation=%#v", plan.ABI, plan.Allocation.Metrics)
	}
	if err := railmach.VerifyCalleeSaveRegions(plan.Machine, plan.Schedule, plan.Allocation, plan.ABI, plan.Frame, planner.coldBlocks, stack.Regions, plan.CalleeSaves); err != nil {
		t.Fatal(err)
	}
	tampered := append([]railmach.CalleeSaveRegion(nil), plan.CalleeSaves...)
	tampered[0].RestoreBefore = plan.Schedule.Order[plan.Schedule.BlockRanges[tampered[0].Block].Start]
	if err := railmach.VerifyCalleeSaveRegions(plan.Machine, plan.Schedule, plan.Allocation, plan.ABI, plan.Frame, planner.coldBlocks, stack.Regions, tampered); err == nil {
		t.Fatal("tampered early callee restore passed verification")
	}
}

func TestNativeBackendPlannerShrinkWrapsMultiBlockColdCalleeSave(t *testing.T) {
	pressure := func(body []byte, values byte) []byte {
		for value := byte(1); value <= values; value++ {
			body = append(body, 0x20, 0x01, 0x42, value, 0x7c)
		}
		for value := byte(1); value < values; value++ {
			body = append(body, 0x7c)
		}
		return body
	}
	body := []byte{0x20, 0x00, 0x04, 0x7e, 0x02, 0x40}
	body = pressure(body, 18)
	body = append(body, 0x21, 0x02, 0x0b)
	body = pressure(body, 17)
	body = append(body, 0x20, 0x02, 0x7c, 0x05, 0x42, 0x00, 0x0b, 0x0b)
	functionCode := append([]byte{0x01, 0x01, 0x7e}, body...)
	functionCode = append(wasmtest.ULEB(uint32(len(functionCode))), functionCode...)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I64}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(functionCode)),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	stack, err := railssa.BuildStackFunc(m, 0)
	if err != nil {
		t.Fatal(err)
	}
	target, err := corecompiler.HostTarget(corecompiler.TargetNative)
	if err != nil {
		t.Fatal(err)
	}
	var baselinePlanner nativeBackendPlanner
	baseline, err := baselinePlanner.Plan(stack, target)
	if err != nil {
		t.Fatal(err)
	}
	edges := make([]compilerprofile.EdgeCount, 0, len(baseline.Machine.Edges))
	for _, edge := range baseline.Machine.Edges {
		from, to := baseline.CFG.Blocks[edge.From], baseline.CFG.Blocks[edge.To]
		if from.InstCount == 0 {
			continue
		}
		targetOffset := uint32(0)
		if int(to.InstStart) < len(stack.Instrs) {
			targetOffset = stack.Instrs[to.InstStart].Offset
		}
		count := uint64(0)
		if edge.Kind == railssa.EdgeFalse {
			count = 100
		}
		edges = append(edges, compilerprofile.EdgeCount{
			Site:   compilerprofile.Site{Function: 0, Offset: stack.Instrs[from.InstStart+from.InstCount-1].Offset},
			Target: targetOffset, Count: count,
		})
	}
	observations := &compilerprofile.Module{Version: compilerprofile.Version, Source: compilerprofile.SourceStatic, Phase: compilerprofile.PhaseSteady, EdgeCounts: edges}
	var planner nativeBackendPlanner
	plan, err := planner.PlanProfile(stack, target, 0, observations)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, region := range plan.CalleeSaves {
		if region.Block != region.RestoreBlock {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("no multi-block cold callee save was shrink-wrapped: regions=%#v cold=%#v blocks=%#v edges=%#v ABI=%#v allocation=%#v", plan.CalleeSaves, planner.coldBlocks, plan.Machine.Blocks, plan.Machine.Edges, plan.ABI, plan.Allocation.Metrics)
	}
	if err := railmach.VerifyCalleeSaveRegions(plan.Machine, plan.Schedule, plan.Allocation, plan.ABI, plan.Frame, planner.coldBlocks, stack.Regions, plan.CalleeSaves); err != nil {
		t.Fatal(err)
	}
}
