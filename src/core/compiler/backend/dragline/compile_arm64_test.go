//go:build arm64

package dragline

import (
	"bytes"
	"crypto/sha256"
	"math"
	"testing"

	corecompiler "github.com/wago-org/wago/src/core/compiler"
	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railmach"
	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railssa"
	compilerprofile "github.com/wago-org/wago/src/core/compiler/profile"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/encoder/arm64"
	"github.com/wago-org/wago/tests/wasmtest"
)

func TestARM64BoundsImmediateHelpers(t *testing.T) {
	var a arm64.Asm
	emitARM64BoundsEnd(&a, arm64.X3, 8)
	if len(a.B) != 4 {
		t.Fatalf("small bounds end emitted %d bytes, want 4", len(a.B))
	}
	a.B = a.B[:0]
	if !emitARM64BoundsLimit(&a, arm64.X4, arm64.X5, 8, 64<<10) || len(a.B) != 4 {
		t.Fatalf("small bounds limit emitted %d bytes", len(a.B))
	}
	a.B = a.B[:0]
	if emitARM64BoundsLimit(&a, arm64.X4, arm64.X5, 64<<10, 32<<10) || len(a.B) != 0 {
		t.Fatalf("underflowing bounds limit was emitted: %x", a.B)
	}
}

func TestCompilerARM64MOPSBulkMemoryIsFeatureGated(t *testing.T) {
	typeSec := wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, nil)))
	funcSec := wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0)))
	memorySec := wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01}))
	body := func(subopcode byte) []byte {
		instructions := []byte{
			0x20, 0x00,
			0x20, 0x01,
			0x20, 0x02,
			0xfc, subopcode,
		}
		if subopcode == 10 {
			instructions = append(instructions, 0x00, 0x00)
		} else {
			instructions = append(instructions, 0x00)
		}
		instructions = append(instructions, 0x0b)
		function := append([]byte{0x00}, instructions...)
		return append(wasmtest.ULEB(uint32(len(function))), function...)
	}
	source := wasmtest.Module(typeSec, funcSec, memorySec, wasmtest.Section(10, wasmtest.Vec(body(10), body(11))))
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	target := corecompiler.Target{GOOS: "linux", GOARCH: "arm64", Mode: corecompiler.TargetExplicit, CPUModel: "test-mops", TuningModel: "generic-arm64"}
	target.FeatureBits[0] = uint64(1) << corecompiler.TargetFeatureARM64MOPS
	cache := corecompiler.NewFunctionArtifactCache(1 << 20)
	compiler := Compiler{FunctionCache: cache}
	withMOPS, err := compiler.Compile(corecompiler.Input{Module: m, Source: source, Target: target})
	if err != nil {
		t.Fatal(err)
	}
	copySequence := []byte{0x40, 0x04, 0x01, 0x1d, 0x40, 0x04, 0x41, 0x1d, 0x40, 0x04, 0x81, 0x1d}
	setSequence := []byte{0x40, 0x04, 0xc1, 0x19, 0x40, 0x44, 0xc1, 0x19, 0x40, 0x84, 0xc1, 0x19}
	if !bytes.Contains(withMOPS.Code, copySequence) || !bytes.Contains(withMOPS.Code, setSequence) {
		t.Fatalf("MOPS code misses copy or set sequence: %x", withMOPS.Code)
	}
	if !withMOPS.RequiresARM64MOPS {
		t.Fatal("MOPS output omitted its runtime ISA requirement")
	}
	target.FeatureBits = [4]uint64{}
	withoutMOPS, err := (Compiler{}).Compile(corecompiler.Input{Module: m, Source: source, Target: target})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(withoutMOPS.Code, copySequence) || bytes.Contains(withoutMOPS.Code, setSequence) {
		t.Fatal("compatibility feature set emitted MOPS")
	}
	if withoutMOPS.RequiresARM64MOPS {
		t.Fatal("baseline output requires MOPS")
	}
	if len(withMOPS.Code) >= len(withoutMOPS.Code) {
		t.Fatalf("MOPS code size = %d, baseline = %d", len(withMOPS.Code), len(withoutMOPS.Code))
	}
	target.FeatureBits[0] = uint64(1) << corecompiler.TargetFeatureARM64MOPS
	tinyProfile := &compilerprofile.Module{
		Version: compilerprofile.Version, ModuleHash: sha256.Sum256(source), Source: compilerprofile.SourceStatic, Phase: compilerprofile.PhaseSteady,
		MemOpSizes: []compilerprofile.ValueHistogram{
			{Site: compilerprofile.Site{Function: 0, Offset: 6}, Buckets: []compilerprofile.ValueBucket{{Low: 0, High: 64, Count: 100}}},
			{Site: compilerprofile.Site{Function: 1, Offset: 6}, Buckets: []compilerprofile.ValueBucket{{Low: 0, High: 64, Count: 100}}},
		},
	}
	profiled, err := compiler.Compile(corecompiler.Input{Module: m, Source: source, Target: target, Profile: tinyProfile})
	if err != nil {
		t.Fatal(err)
	}
	if profiled.RequiresARM64MOPS || bytes.Contains(profiled.Code, copySequence) || bytes.Contains(profiled.Code, setSequence) {
		t.Fatal("tiny-dominated profile selected MOPS")
	}
	warm, err := compiler.Compile(corecompiler.Input{Module: m, Source: source, Target: target})
	if err != nil {
		t.Fatal(err)
	}
	if !warm.RequiresARM64MOPS || !bytes.Equal(warm.Code, withMOPS.Code) {
		t.Fatal("warm function artifacts lost MOPS code or its requirement")
	}
}

func TestCompilerARM64RailMachFinalizesBulkMemoryAndSaturatingConversion(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.F32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0x00, // local.get destination
			0x20, 0x01, // local.get fill byte
			0x20, 0x02, // local.get length
			0xfc, 0x0b, 0x00, // memory.fill 0
			0x20, 0x03, // local.get float
			0xfc, 0x00, // i32.trunc_sat_f32_s
			0x0b,
		}))),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	target, err := corecompiler.HostTarget(corecompiler.TargetNative)
	if err != nil {
		t.Fatal(err)
	}
	var metrics Metrics
	if _, err := (Compiler{Metrics: &metrics}).Compile(corecompiler.Input{Module: m, Source: source, Target: target}); err != nil {
		t.Fatal(err)
	}
	if len(metrics.Functions) != 1 || !metrics.Functions[0].RailMachFinalized {
		t.Fatalf("bulk-memory/saturating-conversion finalization = %#v", metrics.Functions)
	}
}

func TestCompilerARM64SignalsBoundsElideScalarChecks(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x28, 0x02, 0x00, 0x0b}))),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	target, err := corecompiler.HostTarget(corecompiler.TargetNative)
	if err != nil {
		t.Fatal(err)
	}
	explicit, err := (Compiler{}).Compile(corecompiler.Input{Module: m, Source: source, Target: target, Bounds: corecompiler.BoundsExplicit})
	if err != nil {
		t.Fatal(err)
	}
	signals, err := (Compiler{}).Compile(corecompiler.Input{Module: m, Source: source, Target: target, Bounds: corecompiler.BoundsSignals})
	if err != nil {
		t.Fatal(err)
	}
	if len(signals.Code) >= len(explicit.Code) {
		t.Fatalf("signals/explicit native bytes = %d/%d, want signals smaller", len(signals.Code), len(explicit.Code))
	}
}

func TestCompilerNativeARM64RealizesNZCVPhysicalRename(t *testing.T) {
	locals := append(wasmtest.ULEB(2), byte(0x7f))
	body := append(wasmtest.Vec(locals), []byte{
		0x20, 0x00, // local.get 0
		0x20, 0x01, // local.get 1
		0x48,       // i32.lt_s
		0x21, 0x02, // local.set condition
		0x41, 0x07, // i32.const 7: MOV preserves NZCV
		0x21, 0x03, // local.set retained value
		0x20, 0x02, // local.get condition
		0x04, 0x7f, // if (result i32)
		0x41, 0x01, // then 1
		0x05,       // else
		0x41, 0x00, // else 0
		0x0b,       // end if
		0x20, 0x03, // retained constant
		0x6a, // i32.add
		0x0b,
	}...)
	code := append(wasmtest.ULEB(uint32(len(body))), body...)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(code)),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	target, err := corecompiler.HostTarget(corecompiler.TargetNative)
	if err != nil {
		t.Fatal(err)
	}
	var stackScratch railssa.StackFunc
	fn, err := buildCompilerFunc(m, 0, &stackScratch)
	if err != nil {
		t.Fatal(err)
	}
	var planner nativeBackendPlanner
	plan, err := planner.Plan(fn.Structured, target)
	if err != nil {
		t.Fatal(err)
	}
	// Current schedulers correctly move the independent constant ahead of the
	// comparison. Force the constrained legal order that this repair exists for,
	// then rebuild every schedule-dependent product before finalization.
	schedule := *plan.Schedule
	schedule.Order = append([]uint32(nil), plan.Schedule.Order...)
	if len(schedule.Order) < 3 || schedule.Order[0] != 1 || schedule.Order[1] != 0 || schedule.Order[2] != 2 {
		t.Fatalf("unexpected source-stable compare schedule: %#v", schedule.Order)
	}
	schedule.Order[0], schedule.Order[1] = schedule.Order[1], schedule.Order[0]
	allocation, err := railmach.AllocateGreedyPForSchedule(plan.Machine, &schedule, railmach.DefaultGreedyConfig(railmach.TargetARM64), nil)
	if err != nil {
		t.Fatal(err)
	}
	exit, err := railmach.LateSSAExit(plan.Machine, &allocation.Allocation, nil)
	if err != nil {
		t.Fatal(err)
	}
	postRA, err := railmach.PlanPostRA(railmach.TargetARM64, plan.Machine, plan.Selection, &schedule, allocation, exit, nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, rewrite := range postRA.Rewrites {
		found = found || rewrite.Kind == railmach.RewritePhysicalRename && rewrite.First == 0 && rewrite.Second == 2
	}
	if !found {
		t.Fatalf("forced post-RA rewrites = %#v", postRA.Rewrites)
	}
	forced := *plan
	forced.Schedule, forced.Allocation, forced.Exit, forced.PostRA = &schedule, allocation, exit, postRA
	forced.PostRAFusionWith = make([]uint32, len(plan.Machine.Insts))
	forced.PostRAFusionWith[0], forced.PostRAFusionWith[2] = 3, 1
	var relocs []arm64CallReloc
	var metrics FunctionMetrics
	optimized, _, ok, err := emitARM64RailMach(fn, &forced, false, nil, &relocs, &metrics, nil)
	if err != nil || !ok {
		t.Fatalf("optimized NZCV finalization = ok %t, err %v", ok, err)
	}
	baseline := forced
	clearPostRAEmissionRewrites(&baseline)
	relocs = relocs[:0]
	checked, _, ok, err := emitARM64RailMach(fn, &baseline, false, nil, &relocs, nil, nil)
	if err != nil || !ok {
		t.Fatalf("baseline NZCV finalization = ok %t, err %v", ok, err)
	}
	if metrics.PostRARewrites != 1 || len(optimized) >= len(checked) {
		t.Fatalf("NZCV realization = rewrites %d optimized %d baseline %d", metrics.PostRARewrites, len(optimized), len(checked))
	}
}

func TestARM64RailMachLeaSPLargeFrameOffset(t *testing.T) {
	var a arm64.Asm
	if !arm64RailMachLeaSP(&a, arm64.X8, 0x1234) || len(a.B) != 8 {
		t.Fatalf("large SP-relative address = %x", a.B)
	}
	if arm64RailMachLeaSP(&a, arm64.X8, 0x1000000) {
		t.Fatal("out-of-range SP-relative address accepted")
	}
}

func TestARM64StructuredRegisterModesKeepShallowOperandStackInRegisters(t *testing.T) {
	operandStack, full := arm64StructuredRegisterModes(false, false, false, len(arm64StackLocalRegisters)+1, 0, len(arm64OperandStackRegisters))
	if !operandStack || full {
		t.Fatalf("register modes = operand stack %t, full %t; want true, false", operandStack, full)
	}
	operandStack, full = arm64StructuredRegisterModes(false, false, false, len(arm64StackLocalRegisters), 8, len(arm64OperandStackRegisters))
	if !operandStack || !full {
		t.Fatalf("register modes = operand stack %t, full %t; want true, true", operandStack, full)
	}
	operandStack, full = arm64StructuredRegisterModes(true, false, false, len(arm64MixedScalarLocalRegisters), 0, arm64SIMDOperandStackRegisters)
	if !operandStack || full {
		t.Fatalf("mixed SIMD register modes = operand stack %t, full %t; want true, false", operandStack, full)
	}
	operandStack, _ = arm64StructuredRegisterModes(true, false, false, 0, 0, arm64SIMDOperandStackRegisters+1)
	if operandStack {
		t.Fatal("deep mixed SIMD operand stack was admitted to scalar registers")
	}
}

func TestARM64FloatBinaryPairRecognizesMatchingWidths(t *testing.T) {
	typ, f64, ok := arm64FloatBinaryPair(wasm.InstrF64Mul, wasm.InstrF64Add)
	if !ok || !f64 || typ != wasm.F64 {
		t.Fatalf("f64 mul/add pair = type %s, f64 %t, ok %t", typ, f64, ok)
	}
	if _, _, ok := arm64FloatBinaryPair(wasm.InstrF32Mul, wasm.InstrF64Add); ok {
		t.Fatal("mixed-width float pair accepted")
	}
}

func TestARM64RailMachCachesHighestCostFloatConstants(t *testing.T) {
	plan := &nativeBackendPlan{
		Machine: &railmach.Func{
			Insts: []railmach.Inst{
				{Op: wasm.InstrF64Const, Aux: 0x3f847ae147ae147b},
				{Op: wasm.InstrF64Const, Aux: 0},
				{Op: wasm.InstrF64Const, Aux: 0x3fe0000000000000},
				{Op: wasm.InstrF32Const, Aux: 1},
			},
			Blocks: []railmach.Block{{Weight: 8}, {Weight: 16}, {Weight: 4}, {Weight: 1}},
		},
		Schedule: &railmach.Schedule{BlockOf: []railssa.BlockID{0, 1, 2, 3}},
	}
	cached, count := arm64RailMachCachedFloatConstants(plan)
	if count != 3 {
		t.Fatalf("cached constant count = %d, want 3: %#v", count, cached)
	}
	if cached[0].kind != wasm.InstrF64Const || cached[0].bits != 0x3f847ae147ae147b {
		t.Fatalf("highest-cost constant = %#v", cached[0])
	}
	if cached[1].bits != 0 || cached[2].bits != 0x3fe0000000000000 {
		t.Fatalf("remaining cached constants = %#v", cached[1:])
	}
	plan.ABI.HasCall = true
	if _, count := arm64RailMachCachedFloatConstants(plan); count != 0 {
		t.Fatalf("call-making function cached %d constants", count)
	}
	plan.ABI.HasCall = false
	plan.Machine.Insts[0].Result = 1
	plan.Machine.VRegs = []railmach.VRegData{{}, {Def: 3, Type: railmach.TypeF64, Bank: railmach.BankFPR}}
	plan.Allocation = &railmach.GreedyAllocation{Allocation: railmach.Allocation{
		Locations:            []railmach.Location{{}, {Kind: railmach.LocationRegister, Bank: railmach.BankFPR}},
		InstructionPositions: []uint32{0, 1, 2, 3},
	}}
	if physical, ok := arm64RailMachCachedFloatValue(plan, 1, cached, count); !ok || physical != 24 {
		t.Fatalf("cached SSA constant = (%d, %v), want (24, true)", physical, ok)
	}
	plan.Machine.Transfers = []railmach.EdgeTransfer{{Src: 1}}
	if _, ok := arm64RailMachCachedFloatValue(plan, 1, cached, count); ok {
		t.Fatal("edge-transferred constant bypassed its allocated location")
	}
}

func TestARM64RailMachRenamesFinalEdgeMultiply(t *testing.T) {
	f := &railmach.Func{
		Insts:    []railmach.Inst{{Op: wasm.InstrF64Mul, Result: 3, OperandStart: 0, OperandCount: 2}},
		Operands: []railmach.Operand{{Reg: 1, Bank: railmach.BankFPR}, {Reg: 2, Bank: railmach.BankFPR}},
		VRegs: []railmach.VRegData{
			{},
			{Type: railmach.TypeF64, Bank: railmach.BankFPR, Flags: railmach.VRegBlockParam},
			{Type: railmach.TypeF64, Bank: railmach.BankFPR, Flags: railmach.VRegInitial},
			{Def: 3, Type: railmach.TypeF64, Bank: railmach.BankFPR},
			{Type: railmach.TypeF64, Bank: railmach.BankFPR, Flags: railmach.VRegBlockParam},
		},
		Blocks:    []railmach.Block{{InstCount: 1}, {}},
		Edges:     []railmach.Edge{{From: 0, To: 1}},
		Transfers: []railmach.EdgeTransfer{{Src: 3, Dst: 4, Edge: 0}},
	}
	plan := &nativeBackendPlan{
		Machine: f,
		Schedule: &railmach.Schedule{
			Order:       []uint32{0},
			BlockRanges: []railmach.MoveRange{{Count: 1}, {Start: 1}},
			BlockOf:     []railssa.BlockID{0},
		},
		Allocation: &railmach.GreedyAllocation{Allocation: railmach.Allocation{
			Locations: []railmach.Location{
				{},
				{Kind: railmach.LocationRegister, Bank: railmach.BankFPR, Index: 5},
				{Kind: railmach.LocationRegister, Bank: railmach.BankFPR, Index: 7},
				{Kind: railmach.LocationRegister, Bank: railmach.BankFPR, Index: 6},
				{Kind: railmach.LocationRegister, Bank: railmach.BankFPR, Index: 5},
			},
			InstructionPositions: []uint32{0},
		}},
		Exit: &railmach.SSAExit{
			Moves: []railmach.PhysicalMove{{
				Src:       railmach.Location{Kind: railmach.LocationRegister, Bank: railmach.BankFPR, Index: 6},
				Dst:       railmach.Location{Kind: railmach.LocationRegister, Bank: railmach.BankFPR, Index: 5},
				Reg:       3,
				Edge:      0,
				Kind:      railmach.MoveCopy,
				Placement: railmach.PlacePredecessorEnd,
				Bank:      railmach.BankFPR,
			}},
			EdgeMoves: []railmach.MoveRange{{Count: 1}},
		},
	}
	rename := arm64RailMachEdgeResultRename(plan, 0)
	if !rename.valid || rename.instruction != 0 || rename.edge != 0 || rename.move != 0 || rename.destination.Index != 5 {
		t.Fatalf("edge result rename = %#v", rename)
	}
	plan.Exit.Moves = append(plan.Exit.Moves, railmach.PhysicalMove{Src: rename.destination, Dst: railmach.Location{Kind: railmach.LocationRegister, Bank: railmach.BankFPR, Index: 4}})
	plan.Exit.EdgeMoves[0].Count++
	if unsafe := arm64RailMachEdgeResultRename(plan, 0); unsafe.valid {
		t.Fatalf("rename clobbered another edge source: %#v", unsafe)
	}
}

func TestARM64RailMachPromotesTrapFreeMutableGlobal(t *testing.T) {
	plan := &nativeBackendPlan{
		Stack: &railssa.StackFunc{Globals: []wasm.ValType{wasm.I64}},
		Machine: &railmach.Func{
			Insts: []railmach.Inst{
				{Op: wasm.InstrGlobalGet, Result: 1},
				{Op: wasm.InstrI64Const, Result: 2},
				{Op: wasm.InstrI64Add, Result: 3, OperandStart: 0, OperandCount: 2},
				{Op: wasm.InstrGlobalSet, OperandStart: 2, OperandCount: 1},
			},
			Operands: []railmach.Operand{{Reg: 1, Bank: railmach.BankGPR}, {Reg: 2, Bank: railmach.BankGPR}, {Reg: 3, Bank: railmach.BankGPR}},
			VRegs: []railmach.VRegData{
				{},
				{Def: 3, Type: railmach.TypeI64, Bank: railmach.BankGPR},
				{Def: 9, Type: railmach.TypeI64, Bank: railmach.BankGPR},
				{Def: 15, Type: railmach.TypeI64, Bank: railmach.BankGPR},
			},
		},
		Allocation: &railmach.GreedyAllocation{Allocation: railmach.Allocation{Locations: make([]railmach.Location, 4)}},
	}
	promoted := arm64RailMachPromotedGlobal(plan)
	if !promoted.valid || promoted.index != 0 || promoted.typ != wasm.I64 {
		t.Fatalf("promoted global = %#v", promoted)
	}
	if !arm64RailMachPromotedGlobalValue(plan, 1, promoted) || !arm64RailMachPromotedGlobalValue(plan, 3, promoted) {
		t.Fatal("global load or committed arithmetic result did not name the promoted register")
	}
	plan.Machine.Results = []railmach.VReg{1}
	if arm64RailMachPromotedGlobalValue(plan, 1, promoted) {
		t.Fatal("function result bypassed its allocated location")
	}
	plan.Machine.Results = nil
	plan.Machine.Insts = append(plan.Machine.Insts, railmach.Inst{Op: wasm.InstrI64Load})
	if unsafe := arm64RailMachPromotedGlobal(plan); unsafe.valid {
		t.Fatalf("trapping function promoted global: %#v", unsafe)
	}
}

func TestARM64RailMachSoleConsumerRejectsLaterUses(t *testing.T) {
	machine := &railmach.Func{
		Insts: []railmach.Inst{
			{Op: wasm.InstrI32Const, Result: 1},
			{Op: wasm.InstrI32Add, Result: 2, OperandCount: 1},
		},
		Operands: []railmach.Operand{{Reg: 1}},
		VRegs:    make([]railmach.VRegData, 3),
	}
	plan := &nativeBackendPlan{Machine: machine}
	if !arm64RailMachSoleConsumer(plan, 1, 1) {
		t.Fatal("single consumer was not recognized")
	}
	machine.Results = []railmach.VReg{1}
	if arm64RailMachSoleConsumer(plan, 1, 1) {
		t.Fatal("function result was treated as a sole instruction consumer")
	}
}

func TestARM64RailMachI32SpillUsesOneMemoryOperation(t *testing.T) {
	plan := &nativeBackendPlan{
		Machine: &railmach.Func{VRegs: []railmach.VRegData{{}, {Type: railmach.TypeI32, Bank: railmach.BankGPR}}},
		Allocation: &railmach.GreedyAllocation{Allocation: railmach.Allocation{
			Locations:  []railmach.Location{{}, {Kind: railmach.LocationSpill, Bank: railmach.BankGPR}},
			SpillSlots: 1,
		}},
	}
	var a arm64.Asm
	if _, err := arm64RailMachReadLocation(&a, plan, 1, plan.Allocation.Locations[1], arm64.X13, 0); err != nil {
		t.Fatal(err)
	}
	if got := a.Len(); got != 4 {
		t.Fatalf("i32 spill load bytes = %d, want 4", got)
	}
	a.B = a.B[:0]
	if err := arm64RailMachWriteLocation(&a, plan, 1, plan.Allocation.Locations[1], arm64.X13); err != nil {
		t.Fatal(err)
	}
	if got := a.Len(); got != 4 {
		t.Fatalf("i32 spill store bytes = %d, want 4", got)
	}
}

func TestARM64RailMachReusesDominatingMemoryCheckInBlock(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0x00, // local.get address for store
			0x20, 0x00, // local.get address for load
			0x28, 0x02, 0x00, // i32.load
			0x41, 0x01, 0x6a, // i32.const 1; i32.add
			0x36, 0x02, 0x00, // i32.store to the same address
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
	target, err := corecompiler.HostTarget(corecompiler.TargetNative)
	if err != nil {
		t.Fatal(err)
	}
	var stackScratch railssa.StackFunc
	fn, err := buildCompilerFunc(m, 0, &stackScratch)
	if err != nil {
		t.Fatal(err)
	}
	var planner nativeBackendPlanner
	plan, err := planner.Plan(fn.Structured, target)
	if err != nil {
		t.Fatal(err)
	}
	var metadata functionEmissionMetadata
	if _, _, ok, err := emitARM64RailMach(fn, plan, false, nil, nil, nil, &metadata); err != nil || !ok {
		t.Fatalf("RailMach finalization = ok %t, err %v", ok, err)
	}
	if len(metadata.Traps) != 1 || metadata.Traps[0].Code != 3 {
		t.Fatalf("same-address memory traps = %#v, want one dominating bounds trap", metadata.Traps)
	}
}

func TestARM64RailMachDefersUnreachableTrapsPastHotReturn(t *testing.T) {
	locals := append(wasmtest.ULEB(3), byte(0x7e))
	body := append(wasmtest.Vec(locals), []byte{
		0x42, 0x00, 0x21, 0x01, // a = 0
		0x42, 0x01, 0x21, 0x02, // b = 1
		0x02, 0x40, // block
		0x03, 0x40, // loop
		0x20, 0x00, 0x45, 0x0d, 0x01, // break when n == 0
		0x20, 0x01, 0x20, 0x02, 0x7c, 0x21, 0x03, // t = a + b
		0x20, 0x02, 0x21, 0x01, // a = b
		0x20, 0x03, 0x21, 0x02, // b = t
		0x20, 0x00, 0x41, 0x01, 0x6b, 0x21, 0x00, // n--
		0x0c, 0x00, 0x0b, 0x0b, // continue; end loop/block
		0x20, 0x01, 0x0b,
	}...)
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
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	target, err := corecompiler.HostTarget(corecompiler.TargetNative)
	if err != nil {
		t.Fatal(err)
	}
	var stackScratch railssa.StackFunc
	fn, err := buildCompilerFunc(m, 0, &stackScratch)
	if err != nil {
		t.Fatal(err)
	}
	var planner nativeBackendPlanner
	plan, err := planner.Plan(fn.Structured, target)
	if err != nil {
		t.Fatal(err)
	}
	var metadata functionEmissionMetadata
	codeBytes, _, ok, err := emitARM64RailMach(fn, plan, false, nil, nil, nil, &metadata)
	if err != nil || !ok {
		t.Fatalf("RailMach finalization = ok %t, err %v", ok, err)
	}
	firstTrap := len(codeBytes)
	for _, trap := range metadata.Traps {
		if trap.Code == 1 {
			firstTrap = min(firstTrap, int(trap.Offset))
		}
	}
	if firstTrap < 4 || !bytes.Equal(codeBytes[firstTrap-4:firstTrap], []byte{0xc0, 0x03, 0x5f, 0xd6}) {
		t.Fatalf("first unreachable trap offset = %d; hot return does not precede cold traps", firstTrap)
	}
}

func TestARM64RailMachImmediateDoesNotMaterializeFoldedOperand(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0x00, // local.get 0
			0x42, 0x18, // i64.const 24
			0x88, 0x0b, // i64.shr_u
		}))),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	target, err := corecompiler.HostTarget(corecompiler.TargetNative)
	if err != nil {
		t.Fatal(err)
	}
	var stackScratch railssa.StackFunc
	fn, err := buildCompilerFunc(m, 0, &stackScratch)
	if err != nil {
		t.Fatal(err)
	}
	var planner nativeBackendPlanner
	plan, err := planner.Plan(fn.Structured, target)
	if err != nil {
		t.Fatal(err)
	}
	optimized, _, ok, err := emitARM64RailMach(fn, plan, false, nil, nil, nil, nil)
	if err != nil || !ok {
		t.Fatalf("optimized RailMach finalization = ok %t, err %v", ok, err)
	}
	baseline := *plan
	baseline.ImmediateProducer = make([]uint32, len(plan.ImmediateProducer))
	for index := range baseline.ImmediateProducer {
		baseline.ImmediateProducer[index] = ^uint32(0)
	}
	baseline.ImmediateSkip = make([]bool, len(plan.ImmediateSkip))
	unfolded, _, ok, err := emitARM64RailMach(fn, &baseline, false, nil, nil, nil, nil)
	if err != nil || !ok {
		t.Fatalf("unfolded RailMach finalization = ok %t, err %v", ok, err)
	}
	if len(optimized)+4 > len(unfolded) {
		t.Fatalf("immediate/native bytes = %d/%d, want folded operand materialization removed", len(optimized), len(unfolded))
	}
}

func TestARM64RailMachBranchesDirectlyToBrTableCases(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x02, 0x40, 0x02, 0x40, // two nested blocks
			0x20, 0x00, 0x0e, 0x02, 0x00, 0x01, 0x01, // br_table 0 1 1
			0x0b, 0x41, 0x0a, 0x0f, // case 0: return 10
			0x0b, 0x41, 0x14, 0x0b, // case 1/default: 20
		}))),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	target, err := corecompiler.HostTarget(corecompiler.TargetNative)
	if err != nil {
		t.Fatal(err)
	}
	var stackScratch railssa.StackFunc
	fn, err := buildCompilerFunc(m, 0, &stackScratch)
	if err != nil {
		t.Fatal(err)
	}
	var planner nativeBackendPlanner
	plan, err := planner.Plan(fn.Structured, target)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, ok, err := emitARM64RailMach(fn, plan, false, nil, nil, nil, nil); err != nil || !ok {
		t.Fatalf("RailMach finalization = ok %t, err %v", ok, err)
	}
	if len(plan.ConditionalPatches) != 2 {
		t.Fatalf("br_table direct conditional patches = %d, want 2", len(plan.ConditionalPatches))
	}
}

func TestARM64RailMachSelfCallUsesCanonicalArgumentVector(t *testing.T) {
	plan := &nativeBackendPlan{Stack: &railssa.StackFunc{FunctionIndex: 3, ImportedFuncs: 1}}
	if arm64RailMachDirectCallNeedsRegisterArguments(plan, railmach.Inst{Op: wasm.InstrCall, Aux: 3}) {
		t.Fatal("self-recursive RailMach call redundantly requested structured argument registers")
	}
	if !arm64RailMachDirectCallNeedsRegisterArguments(plan, railmach.Inst{Op: wasm.InstrCall, Aux: 4}) {
		t.Fatal("unproven local callee omitted structured argument registers")
	}
	if !arm64RailMachDirectCallNeedsRegisterArguments(plan, railmach.Inst{Op: wasm.InstrCall, Aux: 0}) {
		t.Fatal("imported callee omitted argument registers")
	}
}

func TestARM64RailMachHostAdapterKeepsArgumentsInCanonicalVector(t *testing.T) {
	adapterBytes := func(params []wasm.ValType) int {
		t.Helper()
		source := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, []wasm.ValType{wasm.I32}))),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0x01, 0x0b}))),
		)
		m, err := wasm.DecodeModule(source)
		if err != nil {
			t.Fatal(err)
		}
		if err := wasm.ValidateModule(m); err != nil {
			t.Fatal(err)
		}
		target, err := corecompiler.HostTarget(corecompiler.TargetNative)
		if err != nil {
			t.Fatal(err)
		}
		var stackScratch railssa.StackFunc
		fn, err := buildCompilerFunc(m, 0, &stackScratch)
		if err != nil {
			t.Fatal(err)
		}
		var planner nativeBackendPlanner
		plan, err := planner.Plan(fn.Structured, target)
		if err != nil {
			t.Fatal(err)
		}
		_, internalOffset, ok, err := emitARM64RailMach(fn, plan, false, nil, nil, nil, nil)
		if err != nil || !ok {
			t.Fatalf("RailMach finalization = ok %t, err %v", ok, err)
		}
		return internalOffset
	}
	withoutParams := adapterBytes(nil)
	withParams := adapterBytes([]wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64})
	if withParams != withoutParams {
		t.Fatalf("host adapter bytes with/without parameters = %d/%d; canonical X8 vector should make them equal", withParams, withoutParams)
	}
}

func TestARM64RealizesPreIndexLinearMemory(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x28, 0x02, 0x07, 0x0b}))),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	target, err := corecompiler.HostTarget(corecompiler.TargetNative)
	if err != nil {
		t.Fatal(err)
	}
	var metrics Metrics
	if _, err := (Compiler{Metrics: &metrics}).Compile(corecompiler.Input{Module: m, Source: source, Target: target}); err != nil {
		t.Fatal(err)
	}
	if len(metrics.Functions) != 1 || !metrics.Functions[0].RailMachFinalized || metrics.Functions[0].PostRARewrites == 0 || metrics.Functions[0].PostRAByteSavings <= 0 {
		t.Fatalf("ARM64 pre-index finalization = %#v", metrics.Functions)
	}
}

func TestARM64RealizesPostIndexMemoryChain(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0x00, 0x2d, 0x00, 0xac, 0x02, 0x1a, // i32.load8_u offset=300; drop
			0x20, 0x00, 0x2f, 0x01, 0xad, 0x02, // i32.load16_u offset=301
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
	target, err := corecompiler.HostTarget(corecompiler.TargetNative)
	if err != nil {
		t.Fatal(err)
	}
	var metrics Metrics
	if _, err := (Compiler{Metrics: &metrics}).Compile(corecompiler.Input{Module: m, Source: source, Target: target}); err != nil {
		t.Fatal(err)
	}
	if len(metrics.Functions) != 1 || !metrics.Functions[0].RailMachFinalized || metrics.Functions[0].PostRARewrites == 0 || metrics.Functions[0].PostRAByteSavings <= 0 {
		t.Fatalf("ARM64 post-index finalization = %#v", metrics.Functions)
	}
}

func TestARM64RealizesFloatingMemoryPair(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.F32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0x00, 0x2a, 0x02, 0x00, // f32.load offset=0
			0x20, 0x00, 0x2a, 0x02, 0x04, // f32.load offset=4
			0x92, 0x0b, // f32.add
		}))),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	target, err := corecompiler.HostTarget(corecompiler.TargetNative)
	if err != nil {
		t.Fatal(err)
	}
	var stackScratch railssa.StackFunc
	fn, err := buildCompilerFunc(m, 0, &stackScratch)
	if err != nil {
		t.Fatal(err)
	}
	var planner nativeBackendPlanner
	plan, err := planner.Plan(fn.Structured, target)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, encoded := range plan.PostRAPairWith {
		found = found || encoded != 0
	}
	if !found {
		t.Fatalf("floating pair was not realized: %#v", plan.PostRA.Rewrites)
	}
	var metrics FunctionMetrics
	var relocs []arm64CallReloc
	optimized, _, ok, err := emitARM64RailMach(fn, plan, false, nil, &relocs, &metrics, nil)
	if err != nil || !ok {
		t.Fatalf("floating pair finalization = ok %t, err %v", ok, err)
	}
	baseline := *plan
	clearPostRAEmissionRewrites(&baseline)
	relocs = relocs[:0]
	checked, _, ok, err := emitARM64RailMach(fn, &baseline, false, nil, &relocs, nil, nil)
	if err != nil || !ok {
		t.Fatalf("floating pair baseline = ok %t, err %v", ok, err)
	}
	if metrics.PostRARewrites != 1 || len(optimized) >= len(checked) {
		t.Fatalf("floating pair realization = rewrites %d optimized %d baseline %d", metrics.PostRARewrites, len(optimized), len(checked))
	}
}

func TestARM64RecognizesCanonicalCountedLoop(t *testing.T) {
	moduleBytes := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x02, 0x40, 0x03, 0x40,
			0x20, 0, 0x45, 0x0d, 1,
			0x20, 0, 0x41, 1, 0x6b, 0x21, 0, 0x0c, 0,
			0x0b, 0x0b, 0x0b,
		}))),
	)
	m, err := wasm.DecodeModule(moduleBytes)
	if err != nil {
		t.Fatal(err)
	}
	f, err := railssa.BuildStackFunc(m, 0)
	if err != nil {
		t.Fatal(err)
	}
	if tail, ok := arm64CountedLoopTail(f.Instrs, 2, 0); !ok || tail != 5 {
		t.Fatalf("counted loop tail = %d, %v; want 5, true", tail, ok)
	}
}

func TestARM64PowerRotationTablesCoverBothDirections(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, right := range []bool{false, true} {
			for exponent := uint32(0); exponent <= 10; exponent++ {
				wantA, wantB := referencePowerRotation(wide, right, exponent)
				gotA, gotB := arm64PowerRotationResult(wide, right, exponent)
				if gotA != wantA || gotB != wantB {
					t.Fatalf("wide=%t right=%t exponent=%d got=(%#x,%#x) want=(%#x,%#x)", wide, right, exponent, gotA, gotB, wantA, wantB)
				}
			}
		}
	}
}

func referencePowerRotation(wide, right bool, exponent uint32) (uint64, uint64) {
	bits := uint64(32)
	mask := uint64(math.MaxUint32)
	if wide {
		bits, mask = 64, math.MaxUint64
	}
	a, b := uint64(1), uint64(1)<<exponent
	rotate := func(value, shift uint64) uint64 {
		shift &= bits - 1
		if right {
			return (value>>shift | value<<((bits-shift)&(bits-1))) & mask
		}
		return (value<<shift | value>>((bits-shift)&(bits-1))) & mask
	}
	for range uint64(16) << exponent {
		a = rotate(a, b)
		b = rotate(b, a)
	}
	return a, b
}
