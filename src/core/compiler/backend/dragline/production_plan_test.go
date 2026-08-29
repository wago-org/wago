package dragline

import (
	"slices"
	"testing"

	corecompiler "github.com/wago-org/wago/src/core/compiler"
	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railmach"
	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railssa"
	compilerprofile "github.com/wago-org/wago/src/core/compiler/profile"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
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
	machine.Target = railmach.TargetAMD64
	if nativeARM64CachesGlobals(machine) {
		t.Fatal("AMD64 function enabled the ARM64 descriptor cache")
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

func TestNativeBackendPlannerAllocatesOnlyRequiredPostRAScratch(t *testing.T) {
	var planner nativeBackendPlanner
	if !planner.preparePostRAScratch(railmach.TargetARM64, 64, []railmach.Rewrite{{Kind: railmach.RewriteARM64CompareBranch}}) {
		t.Fatal("ARM64 compare/branch was not recognized as realizable")
	}
	if len(planner.postRAFusionWith) != 64 || len(planner.postRAPairWith) != 0 || len(planner.postRASkip) != 0 || len(planner.postRAForwardFrom) != 0 || len(planner.postRAMemoryFrom) != 0 || len(planner.postRARepeatFirst) != 0 {
		t.Fatalf("compare/branch scratch = fusion:%d pair:%d skip:%d forward:%d memory:%d repeat:%d", len(planner.postRAFusionWith), len(planner.postRAPairWith), len(planner.postRASkip), len(planner.postRAForwardFrom), len(planner.postRAMemoryFrom), len(planner.postRARepeatFirst))
	}
	if !planner.preparePostRAScratch(railmach.TargetARM64, 32, []railmach.Rewrite{{Kind: railmach.RewriteARM64Pair}}) {
		t.Fatal("ARM64 pair was not recognized as realizable")
	}
	if len(planner.postRAPairWith) != 32 || len(planner.postRASkip) != 32 || len(planner.postRAFusionWith) != 0 {
		t.Fatalf("pair scratch = pair:%d skip:%d fusion:%d", len(planner.postRAPairWith), len(planner.postRASkip), len(planner.postRAFusionWith))
	}
	if planner.preparePostRAScratch(railmach.TargetARM64, 32, []railmach.Rewrite{{Kind: railmach.RewriteAMD64MemoryFold}}) {
		t.Fatal("cross-target rewrite allocated realization scratch")
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
	}
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
	plan := &nativeBackendPlan{Machine: machine, Selection: &railmach.SelectionPlan{}}
	producers := make([]uint32, len(machine.Insts))
	skipped := make([]bool, len(machine.Insts))
	uses := make([]uint32, len(machine.VRegs))
	buildNativeImmediateCombinations(plan, producers, skipped, uses)
	if producers[1] != 0 || producers[2] != 0 || !skipped[0] || uses[1] != 2 {
		t.Fatalf("producers=%v skipped=%v uses=%v", producers, skipped, uses)
	}
	machine.Target = railmach.TargetARM64
	applyNativeARM64ShiftImmediateRematerialization(machine)
	if machine.Operands[1].Flags&railmach.OperandColdRemat == 0 || machine.Operands[3].Flags&railmach.OperandColdRemat == 0 {
		t.Fatalf("rotate operands were not removed from allocation liveness: %#v", machine.Operands)
	}
	machine.Results = []railmach.VReg{1}
	buildNativeImmediateCombinations(plan, producers, skipped, uses)
	if skipped[0] {
		t.Fatal("function-result constant was elided")
	}
}

func TestARM64LogicalImmediateEligibility(t *testing.T) {
	for _, test := range []struct {
		kind  wasm.InstrKind
		value uint64
		want  bool
	}{
		{wasm.InstrI64And, 0x0000ffff0000ffff, true},
		{wasm.InstrI32Or, 0x00ff00ff, true},
		{wasm.InstrI64Xor, 0x0101010101010101, true},
		{wasm.InstrI64And, 0, false},
		{wasm.InstrI64Add, 0xff, false},
	} {
		if got := arm64LogicalImmediateEncodable(test.kind, test.value); got != test.want {
			t.Fatalf("%s %#x eligibility = %t, want %t", test.kind, test.value, got, test.want)
		}
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
	skipped := make([]bool, 1)
	buildNativeEdgeConstantRematerialization(plan, skipped, []uint32{0, 1, 0})
	if !skipped[0] {
		t.Fatal("edge-only constant was not selected for direct rematerialization")
	}
	skipped[0] = false
	buildNativeEdgeConstantRematerialization(plan, skipped, []uint32{0, 2, 0})
	if skipped[0] {
		t.Fatal("constant with a non-edge use was elided")
	}
	plan.Exit.Moves = nil
	buildNativeEdgeConstantRematerialization(plan, skipped, []uint32{0, 1, 0})
	if skipped[0] {
		t.Fatal("coalesced edge constant lost its defining materialization")
	}
}

func TestNativeScheduleScoreBoundsLargeLatencyPreference(t *testing.T) {
	pressure := railmach.ScheduleScore{Kind: railmach.ScheduleKindPressure, WeightedSpillDebt: 300}
	latency := railmach.ScheduleScore{Kind: railmach.ScheduleKindLatencyFusion, WeightedSpillDebt: 400}
	if !nativeScheduleScoreBetter(corecompiler.ObjectiveSpeed, 1024, latency, pressure) || nativeScheduleScoreBetter(corecompiler.ObjectiveSpeed, 1024, pressure, latency) {
		t.Fatal("bounded large-function latency preference was not stable across candidate order")
	}
	latency.WeightedSpillDebt++
	if nativeScheduleScoreBetter(corecompiler.ObjectiveSpeed, 1024, latency, pressure) {
		t.Fatal("latency schedule beyond the spill bound was preferred")
	}
	latency.WeightedSpillDebt = 400
	if nativeScheduleScoreBetter(corecompiler.ObjectiveSize, 1024, latency, pressure) || nativeScheduleScoreBetter(corecompiler.ObjectiveSpeed, 1023, latency, pressure) {
		t.Fatal("latency preference escaped the speed/large-function boundary")
	}
}

func TestRailMachLoopProfitabilityPolicy(t *testing.T) {
	for _, test := range []struct {
		name         string
		instructions []wasm.InstrKind
		loopDepth    uint8
		want         bool
	}{
		{"established_arithmetic", []wasm.InstrKind{wasm.InstrI32Add}, 1, false},
		{"comparison", []wasm.InstrKind{wasm.InstrI32LtU}, 1, true},
		{"wide_recurrence", []wasm.InstrKind{wasm.InstrI64Add, wasm.InstrI32Sub}, 1, true},
		{"wide_multiply", []wasm.InstrKind{wasm.InstrI64Mul, wasm.InstrI64ShrU}, 1, true},
		{"wide_memory", []wasm.InstrKind{wasm.InstrI64Load, wasm.InstrI64Add, wasm.InstrI64Store}, 1, true},
		{"mutable_global", []wasm.InstrKind{wasm.InstrGlobalGet, wasm.InstrI64Add, wasm.InstrGlobalSet}, 1, true},
		{"masked_wrap", []wasm.InstrKind{wasm.InstrI32And, wasm.InstrI32WrapI64}, 1, true},
		{"reinterpret_roundtrip", []wasm.InstrKind{wasm.InstrI32And, wasm.InstrI32WrapI64, wasm.InstrI64ReinterpretF64, wasm.InstrF64ReinterpretI64}, 1, false},
		{"f64_memory_arithmetic", []wasm.InstrKind{wasm.InstrF64Load, wasm.InstrF64Mul, wasm.InstrF64Add, wasm.InstrF64Store}, 1, true},
		{"f64_sqrt_conversion", []wasm.InstrKind{wasm.InstrF64ConvertI32U, wasm.InstrF64Sqrt, wasm.InstrF64Add}, 1, true},
		{"saturating_conversion", []wasm.InstrKind{wasm.InstrF64Load, wasm.InstrI32TruncSatF64S}, 1, true},
		{"bulk_memory", []wasm.InstrKind{wasm.InstrF64Load, wasm.InstrMemoryFill}, 1, true},
		{"structured_if", []wasm.InstrKind{wasm.InstrI32LtU, wasm.InstrIf}, 1, true},
		{"nested_loop", []wasm.InstrKind{wasm.InstrI32LtU}, 2, true},
		{"nested_loop_with_call", []wasm.InstrKind{wasm.InstrI32LtU, wasm.InstrCall}, 2, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			stack := &railssa.StackFunc{MaxLoopDepth: test.loopDepth}
			for _, kind := range test.instructions {
				stack.Instrs = append(stack.Instrs, railssa.StackInstr{Kind: kind})
			}
			if got := railMachCandidate(stack, false); got != test.want {
				t.Fatalf("RailMach candidate = %v, want %v", got, test.want)
			}
		})
	}
}

func TestRailMachLoopProfitabilityBoundsLargeMultiCallAdmission(t *testing.T) {
	for _, test := range []struct {
		name        string
		simd        bool
		moduleFuncs int
		want        bool
	}{
		{name: "scalar", want: true},
		{name: "simd", simd: true, want: false},
		{name: "large_module", moduleFuncs: 257, want: false},
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
	body := []byte{0x20, 0x00, 0x04, 0x7e}
	for value := byte(1); value <= 18; value++ {
		body = append(body, 0x20, 0x01, 0x42, value, 0x7c)
	}
	for range 17 {
		body = append(body, 0x7c)
	}
	body = append(body, 0x05, 0x42, 0x00, 0x0b, 0x0b)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I64}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
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
