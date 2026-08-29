package dragline

import (
	"crypto/sha256"
	"encoding/binary"
	"math"
	"runtime"
	"testing"

	corecompiler "github.com/wago-org/wago/src/core/compiler"
	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railmach"
	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railssa"
	compilerprofile "github.com/wago-org/wago/src/core/compiler/profile"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func TestCompilerReportsPerFunctionMetricsAndPeakLiveBytes(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x6a, 0x0b}),
			wasmtest.Code([]byte{0x02, 0x7f, 0x41, 0, 0x28, 2, 0, 0x0b, 0x0b}),
		)),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	target := corecompiler.Target{GOOS: runtime.GOOS, GOARCH: runtime.GOARCH}
	var metrics Metrics
	output, err := (Compiler{Metrics: &metrics}).Compile(corecompiler.Input{Module: m, Source: source, Target: target})
	if err != nil {
		t.Fatal(err)
	}
	if metrics.Version != MetricsVersion || metrics.TargetFingerprint != target.Fingerprint() {
		t.Fatalf("metric identity = version %d fingerprint %x", metrics.Version, metrics.TargetFingerprint)
	}
	if len(metrics.Functions) != 2 {
		t.Fatalf("function rows = %d, want 2", len(metrics.Functions))
	}
	if metrics.Functions[0].RailSSAInstructions == 0 || metrics.Functions[0].StackInstructions != 0 {
		t.Fatalf("straight row = %#v", metrics.Functions[0])
	}
	if metrics.Functions[1].StackInstructions == 0 || metrics.Functions[1].RailSSAInstructions == 0 || metrics.Functions[1].SemanticArguments == 0 || metrics.Functions[1].BoundsChecksElided != 1 || metrics.Functions[1].ProofQueries != 1 {
		t.Fatalf("structured row = %#v", metrics.Functions[1])
	}
	for i, row := range metrics.Functions {
		if row.Function != uint32(i) || row.BodyBytes == 0 || row.NativeBytes == 0 || row.PeakLiveBytes == 0 {
			t.Fatalf("incomplete row %d: %#v", i, row)
		}
	}
	if row := metrics.Functions[0]; row.liveBaseBytes == 0 || row.PeakLiveBytes < row.liveBaseBytes {
		t.Fatalf("RailMach row does not retain its planner base: %#v", row)
	}
	if row := metrics.Functions[0]; row.RailSSARetainedBytes == 0 || row.RailMachRetainedBytes == 0 || row.RailSSARetainedBytes+row.RailMachRetainedBytes+row.NativePlannerRetainedBytes >= row.PeakLiveBytes {
		t.Fatalf("RailMach retained-capacity attribution is incomplete: %#v", row)
	}
	if metrics.NativeBytes != uint64(len(output.Code)) || metrics.PeakLiveBytes < metrics.Functions[0].PeakLiveBytes || metrics.PeakLiveBytes < metrics.Functions[1].PeakLiveBytes {
		t.Fatalf("module metrics = %#v, native output = %d", metrics, len(output.Code))
	}
}

func TestRecordNativePlanMetricsKeepsRailSSAAndRailMachDistinct(t *testing.T) {
	plan := &nativeBackendPlan{
		Semantic:        &railssa.SemanticFunc{Insts: make([]railssa.SemanticInst, 11), Args: make([]railssa.FlowValueID, 9)},
		Machine:         &railmach.Func{Insts: make([]railmach.Inst, 2)},
		Selection:       &railmach.SelectionPlan{Combinations: make([]railmach.Combination, 3)},
		DAG:             &railmach.DependencyDAG{Dependencies: make([]railmach.Dependency, 8)},
		Allocation:      &railmach.GreedyAllocation{Allocation: railmach.Allocation{Intervals: make([]railmach.LiveInterval, 4)}, Fragments: make([]railmach.AllocationFragment, 2)},
		Exit:            &railmach.SSAExit{Debt: railmach.CopyDebt{Physical: 7, Coalesced: 5, Rematerialized: 2}},
		Simplified:      &railssa.SimplifyResult{},
		BackendAttempts: 2,
	}
	metrics := FunctionMetrics{}
	recordNativePlanMetrics(&metrics, plan)
	if metrics.RailSSAInstructions != 11 || metrics.SemanticArguments != 9 || metrics.RailMachInstructions != 2 {
		t.Fatalf("IR metrics = RailSSA:%d args:%d RailMach:%d", metrics.RailSSAInstructions, metrics.SemanticArguments, metrics.RailMachInstructions)
	}
	if metrics.ScheduleCandidates != 6 || metrics.SelectionCombinations != 3 || metrics.Dependencies != 8 || metrics.LiveSegments != 6 {
		t.Fatalf("quality-search metrics = candidates:%d combinations:%d dependencies:%d segments:%d", metrics.ScheduleCandidates, metrics.SelectionCombinations, metrics.Dependencies, metrics.LiveSegments)
	}
	if metrics.PhysicalCopies != 7 || metrics.CoalescedCopies != 5 || metrics.CopyRematerializations != 2 {
		t.Fatalf("copy metrics = physical:%d coalesced:%d rematerialized:%d", metrics.PhysicalCopies, metrics.CoalescedCopies, metrics.CopyRematerializations)
	}
}

func TestRecordSpecializationMetricsIncludesGCFacts(t *testing.T) {
	plan := &railssa.SpecializationPlan{Entries: []railssa.Specialization{
		{Kind: railssa.SpecializeHostEffects},
		{Kind: railssa.SpecializeExactGCType},
		{Kind: railssa.SpecializeFreshObject},
	}}
	var metrics FunctionMetrics
	recordSpecializationMetrics(&metrics, plan)
	if metrics.HostEffectSpecializations != 1 || metrics.ExactGCTypeSpecializations != 1 || metrics.FreshObjectSpecializations != 1 {
		t.Fatalf("specialization metrics = %#v", metrics)
	}
}

func TestCompilerMetricsResetBetweenCompiles(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}))),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	var metrics Metrics
	compiler := Compiler{Metrics: &metrics}
	input := corecompiler.Input{Module: m, Source: source, Target: corecompiler.Target{GOOS: runtime.GOOS, GOARCH: runtime.GOARCH}}
	if _, err := compiler.Compile(input); err != nil {
		t.Fatal(err)
	}
	metrics.Functions = append(metrics.Functions, FunctionMetrics{Function: 99})
	if _, err := compiler.Compile(input); err != nil {
		t.Fatal(err)
	}
	if len(metrics.Functions) != 1 || metrics.Functions[0].Function != 0 {
		t.Fatalf("metrics accumulated across compiles: %#v", metrics.Functions)
	}
}

func TestCompilerTargetModesIdentifyRailMachFinalization(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I64}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x42, 7, 0x7c, 0x20, 1, 0x42, 3, 0x7d, 0x84, 0x0b}))),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []corecompiler.TargetMode{corecompiler.TargetCompatibility, corecompiler.TargetNative} {
		t.Run(mode.String(), func(t *testing.T) {
			target, err := corecompiler.HostTarget(mode)
			if err != nil {
				t.Fatal(err)
			}
			var metrics Metrics
			_, err = (Compiler{Metrics: &metrics}).Compile(corecompiler.Input{Module: m, Source: source, Target: target})
			if err != nil {
				t.Fatal(err)
			}
			if len(metrics.Functions) != 1 || !metrics.Functions[0].RailMachFinalized || metrics.Functions[0].ScheduleKind == 0 || metrics.Functions[0].RailSSAInstructions == 0 || metrics.Functions[0].RailMachInstructions == 0 || metrics.Functions[0].ScheduleCandidates != 3 || metrics.Functions[0].LiveSegments == 0 || metrics.Functions[0].ImmediateFolds != 2 || runtime.GOARCH == "amd64" && metrics.Functions[0].PostRARewrites == 0 {
				t.Fatalf("%s metrics = %#v", mode, metrics.Functions)
			}
		})
	}
}

func TestCompilerNativeRailMachBranchCastEdgeRefinement(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x00},
			wasmtest.FuncType([]wasm.ValType{wasm.AnyRef}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.EqRef}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x02, 0x02,
			0x20, 0x00,
			0xfb, 0x18, 0x03, 0x00, 0x6e, 0x6d,
			0x00,
			0x0b,
			0x1a, 0x41, 0x01,
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
	if plan == nil || len(plan.CFG.Refinements) != 2 {
		t.Fatalf("branch-cast refinements = %#v", func() any {
			if plan == nil {
				return nil
			}
			return plan.CFG.Refinements
		}())
	}
	flow := &planner.flow
	branchValue := flow.InstructionValues[2]
	if branchValue == 0 || flow.Values[branchValue].Type != wasm.I32 {
		t.Fatalf("branch-cast condition = v%d %#v", branchValue, flow.Values)
	}
	refinedParams := 0
	for _, param := range flow.Params {
		if flow.Values[param.Value].Type.Kind() == wasm.ValRef {
			refinedParams++
		}
	}
	if refinedParams < 2 {
		t.Fatalf("branch-cast refined params = %d; params=%#v", refinedParams, flow.Params)
	}
	var metrics Metrics
	if _, err := (Compiler{Metrics: &metrics}).Compile(corecompiler.Input{Module: m, Source: source, Target: target}); err != nil {
		t.Fatal(err)
	}
	if len(metrics.Functions) != 1 || !metrics.Functions[0].RailMachFinalized {
		t.Fatalf("branch-cast finalization = %#v", metrics.Functions)
	}
}

func TestCompilerProductionGCAllocationFactsReachSpecialization(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x00},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xfb, 0x01, 0x00, 0x1a, 0x41, 0x00, 0x0b}))),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	target, err := corecompiler.HostTarget(corecompiler.TargetNative)
	if err != nil {
		t.Fatal(err)
	}
	stack, err := railssa.BuildStackFunc(m, 0)
	if err != nil {
		t.Fatal(err)
	}
	var planner nativeBackendPlanner
	plan, err := planner.Plan(stack, target)
	if err != nil {
		t.Fatal(err)
	}
	dead := 0
	for _, reservation := range plan.DeadGCReservations {
		if reservation {
			dead++
		}
	}
	if dead != 1 {
		t.Fatalf("dead GC reservations = %d; plan=%v", dead, plan.DeadGCReservations)
	}
	var metrics Metrics
	if _, err := (Compiler{Metrics: &metrics}).Compile(corecompiler.Input{Module: m, Source: source, Target: target}); err != nil {
		t.Fatal(err)
	}
	if len(metrics.Functions) != 1 || metrics.Functions[0].ExactGCTypeSpecializations != 1 || metrics.Functions[0].FreshObjectSpecializations != 1 {
		t.Fatalf("production GC specializations = %#v", metrics.Functions)
	}
}

func TestCompilerPlansCheckedDeadGCConstructorFamilies(t *testing.T) {
	passive := append([]byte{0x01}, append(wasmtest.ULEB(3), []byte("abc")...)...)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7e, 0x01}, // (struct (field (mut i64)))
			[]byte{0x5e, 0x7e, 0x01},       // (array (mut i64))
			[]byte{0x5e, 0x78, 0x01},       // (array (mut i8))
			[]byte{0x5e, 0x63, 0x00, 0x01}, // (array (mut (ref null 0)))
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(
			wasmtest.ULEB(4), wasmtest.ULEB(4), wasmtest.ULEB(4), wasmtest.ULEB(4), wasmtest.ULEB(4), wasmtest.ULEB(4),
		)),
		wasmtest.Section(12, wasmtest.ULEB(1)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x42, 0x2a, 0xfb, 0x00, 0x00, 0x1a, 0x41, 0x07, 0x0b}),
			wasmtest.Code([]byte{0x41, 0x02, 0xfb, 0x07, 0x01, 0x1a, 0x41, 0x07, 0x0b}),
			wasmtest.Code([]byte{0x42, 0x2a, 0x41, 0x02, 0xfb, 0x06, 0x01, 0x1a, 0x41, 0x07, 0x0b}),
			wasmtest.Code([]byte{0x42, 0x01, 0x42, 0x02, 0xfb, 0x08, 0x01, 0x02, 0x1a, 0x41, 0x07, 0x0b}),
			wasmtest.Code([]byte{0x41, 0x00, 0x41, 0x02, 0xfb, 0x09, 0x02, 0x00, 0x1a, 0x41, 0x07, 0x0b}),
			wasmtest.Code([]byte{0xd0, 0x00, 0x41, 0x02, 0xfb, 0x06, 0x03, 0x1a, 0x41, 0x07, 0x0b}),
		)),
		wasmtest.Section(11, wasmtest.Vec(passive)),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	target, err := corecompiler.HostTarget(corecompiler.TargetNative)
	if err != nil {
		t.Fatal(err)
	}
	for function := 0; function < 6; function++ {
		stack, err := railssa.BuildStackFunc(m, function)
		if err != nil {
			t.Fatalf("function %d: %v", function, err)
		}
		var planner nativeBackendPlanner
		plan, err := planner.Plan(stack, target)
		if err != nil {
			t.Fatalf("function %d: %v", function, err)
		}
		dead := 0
		for _, reservation := range plan.DeadGCReservations {
			if reservation {
				dead++
			}
		}
		want := 1
		if function == 5 {
			want = 0
		}
		if dead != want {
			t.Fatalf("function %d dead GC reservations = %d, want %d; plan=%v", function, dead, want, plan.DeadGCReservations)
		}
	}
}

func TestCompilerPlansProvenNoBarrierGCStores(t *testing.T) {
	structType := []byte{0x5f}
	structType = append(structType, wasmtest.Vec([]byte{0x6d, 0x01})...)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			structType,
			[]byte{0x5e, 0x6d, 0x01},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0xd0, 0x6d, 0xfb, 0x00, 0x00,
			0x41, 0x07, 0xfb, 0x1c, 0xfb, 0x05, 0x00, 0x00,
			0x41, 0x01, 0xfb, 0x07, 0x01,
			0x41, 0x00, 0xd0, 0x6d, 0xfb, 0x0e, 0x01,
			0xd0, 0x6d, 0xfb, 0x00, 0x00,
			0xd0, 0x6d, 0xfb, 0x00, 0x00, 0xfb, 0x05, 0x00, 0x00,
			0x41, 0x02, 0x0b,
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
	stack, err := railssa.BuildStackFunc(m, 0)
	if err != nil {
		t.Fatal(err)
	}
	var planner nativeBackendPlanner
	plan, err := planner.Plan(stack, target)
	if err != nil {
		t.Fatal(err)
	}
	proved := 0
	for _, noBarrier := range plan.NoBarrierGCStores {
		if noBarrier {
			proved++
		}
	}
	if proved != 2 {
		t.Fatalf("proven no-barrier GC stores = %d, want 2; plan=%v", proved, plan.NoBarrierGCStores)
	}
}

func TestCompilerNativeRailMachTargetConstraintBoundary(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I64}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x86, 0x0b}))),
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
		t.Fatalf("%s shift finalization = %#v", runtime.GOARCH, metrics.Functions)
	}
}

func TestCompilerNativeRailMachFixedShiftRepairFinalization(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I64, wasm.I64}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x7c, 0x20, 2, 0x86, 0x0b}))),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	target, err := corecompiler.HostTarget(corecompiler.TargetNative)
	if err != nil {
		t.Fatal(err)
	}
	stack, err := railssa.BuildStackFunc(m, 0)
	if err != nil {
		t.Fatal(err)
	}
	var planner nativeBackendPlanner
	plan, err := planner.Plan(stack, target)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOARCH == "amd64" {
		hasRepair := false
		for _, moveRange := range plan.Exit.FixedMoves {
			hasRepair = hasRepair || moveRange.Count != 0
		}
		if !hasRepair {
			t.Fatal("expected an AMD64 fixed shift-count repair")
		}
	}
	var metrics Metrics
	if _, err := (Compiler{Metrics: &metrics}).Compile(corecompiler.Input{Module: m, Source: source, Target: target}); err != nil {
		t.Fatal(err)
	}
	if len(metrics.Functions) != 1 || !metrics.Functions[0].RailMachFinalized {
		t.Fatalf("%s fixed shift finalization = %#v", runtime.GOARCH, metrics.Functions)
	}
}

func TestCompilerNativeRailMachDivisionFinalization(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I64}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x7f, 0x0b}))),
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
		t.Fatalf("%s division finalization = %#v", runtime.GOARCH, metrics.Functions)
	}
}

func TestCompilerNativeRailMachFixedDivisionRepairFinalization(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I64, wasm.I64}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 2, 0x20, 1, 0x7f, 0x0b}))),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	target, err := corecompiler.HostTarget(corecompiler.TargetNative)
	if err != nil {
		t.Fatal(err)
	}
	stack, err := railssa.BuildStackFunc(m, 0)
	if err != nil {
		t.Fatal(err)
	}
	var planner nativeBackendPlanner
	plan, err := planner.Plan(stack, target)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOARCH == "amd64" {
		hasRepair := false
		for _, moveRange := range plan.Exit.FixedMoves {
			hasRepair = hasRepair || moveRange.Count != 0
		}
		if !hasRepair {
			t.Fatal("expected an AMD64 fixed division-input repair")
		}
	}
	var metrics Metrics
	if _, err := (Compiler{Metrics: &metrics}).Compile(corecompiler.Input{Module: m, Source: source, Target: target}); err != nil {
		t.Fatal(err)
	}
	if len(metrics.Functions) != 1 || !metrics.Functions[0].RailMachFinalized {
		t.Fatalf("%s fixed division finalization = %#v", runtime.GOARCH, metrics.Functions)
	}
}

func TestCompilerNativeRailMachCallLiveFinalization(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0x20, 0, 0x10, 1, 0x7c, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x42, 1, 0x7c, 0x0b}),
		)),
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
	if len(metrics.Functions) != 2 || !metrics.Functions[0].RailMachFinalized || !metrics.Functions[1].RailMachFinalized {
		t.Fatalf("%s call-live finalization = %#v", runtime.GOARCH, metrics.Functions)
	}
	if metrics.Functions[0].IPRARefinedCalls != 1 || metrics.Functions[0].PreservationCost != 0 {
		t.Fatalf("%s exact callee contract was not used: %#v", runtime.GOARCH, metrics.Functions[0])
	}
}

func TestCompilerNativeRecursiveSCCRemainsConservative(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x10, 1, 0x0b}),
			wasmtest.Code([]byte{0x10, 0, 0x0b}),
		)),
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
	if len(metrics.Functions) != 2 || metrics.Functions[0].IPRARefinedCalls != 0 || metrics.Functions[1].IPRARefinedCalls != 0 {
		t.Fatalf("recursive SCC used a partial contract: %#v", metrics.Functions)
	}
}

func TestCompilerNativeHotRecursiveSCCUsesCompleteContracts(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x10, 1, 0x0b}),
			wasmtest.Code([]byte{0x10, 0, 0x0b}),
		)),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	target, err := corecompiler.HostTarget(corecompiler.TargetNative)
	if err != nil {
		t.Fatal(err)
	}
	observations := &compilerprofile.Module{Version: compilerprofile.Version, Source: compilerprofile.SourceStatic, Phase: compilerprofile.PhaseSteady, FunctionCounts: []uint64{100, 1}}
	var metrics Metrics
	if _, err := (Compiler{Metrics: &metrics}).Compile(corecompiler.Input{Module: m, Source: source, Target: target, Profile: observations}); err != nil {
		t.Fatal(err)
	}
	if len(metrics.Functions) != 2 || metrics.Functions[0].IPRARefinedCalls != 1 || metrics.Functions[1].IPRARefinedCalls != 1 {
		t.Fatalf("hot recursive SCC did not use complete contracts: %#v", metrics.Functions)
	}
}

func TestCompilerNativeHotMixedEmitterSCCRemainsConservative(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x03, 0x40, 0x03, 0x40, 0x0b, 0x0b, 0x10, 1, 0x0b}),
			wasmtest.Code([]byte{0x10, 0, 0x0b}),
		)),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	target, err := corecompiler.HostTarget(corecompiler.TargetNative)
	if err != nil {
		t.Fatal(err)
	}
	observations := &compilerprofile.Module{Version: compilerprofile.Version, Source: compilerprofile.SourceStatic, Phase: compilerprofile.PhaseSteady, FunctionCounts: []uint64{100, 100}}
	var metrics Metrics
	if _, err := (Compiler{Metrics: &metrics}).Compile(corecompiler.Input{Module: m, Source: source, Target: target, Profile: observations}); err != nil {
		t.Fatal(err)
	}
	if len(metrics.Functions) != 2 || metrics.Functions[0].IPRARefinedCalls != 0 || metrics.Functions[1].IPRARefinedCalls != 0 {
		t.Fatalf("mixed-emitter SCC published a partial contract: %#v", metrics.Functions)
	}
}

func TestCompilerNativeRailMachFloatFinalization(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.F64, wasm.F64}, []wasm.ValType{wasm.F64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0xa2, 0x0b}))),
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
	if len(metrics.Functions) != 1 || !metrics.Functions[0].RailMachFinalized || metrics.Functions[0].ClobberFPR == 0 {
		t.Fatalf("%s float finalization = %#v", runtime.GOARCH, metrics.Functions)
	}
}

func TestCompilerNativeRailMachARM64PairFinalization(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x29, 3, 0, 0x20, 0, 0x29, 3, 8, 0x7c, 0x0b}))),
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
		t.Fatalf("%s pair finalization = %#v", runtime.GOARCH, metrics.Functions)
	}
	if runtime.GOARCH == "arm64" && (metrics.Functions[0].PostRARewrites == 0 || metrics.Functions[0].PostRAByteSavings <= 0) {
		t.Fatalf("ARM64 pair did not report an exact byte saving: %#v", metrics.Functions[0])
	}
}

func TestCompilerNativeRailMachRejectsUnalignedARM64Pair(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x29, 3, 20, 0x20, 0, 0x29, 3, 28, 0x7c, 0x0b}))),
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
		t.Fatalf("%s unaligned pair finalization = %#v", runtime.GOARCH, metrics.Functions)
	}
	if runtime.GOARCH == "arm64" && (metrics.Functions[0].PostRARewrites != 2 || metrics.Functions[0].PostRAByteSavings <= 0) {
		// Both accesses independently use legal pre-index forms. A realized pair
		// would add a third rewrite, and remains illegal at this alignment.
		t.Fatalf("ARM64 unaligned pair/pre-index realization = %#v", metrics.Functions[0])
	}
}

func TestCompilerNativeRailMachStoreLoadForwardFinalization(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I64}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x37, 3, 0, 0x20, 0, 0x29, 3, 0, 0x0b}))),
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
		t.Fatalf("%s store-load finalization = %#v", runtime.GOARCH, metrics.Functions)
	}
	if metrics.Functions[0].PostRARewrites == 0 {
		t.Fatalf("%s store-load forwarding was planned but not emitted: %#v", runtime.GOARCH, metrics.Functions[0])
	}
}

func TestCompilerNativeRailMachConsumesDischargedDivisorCheck(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0x00, 0x41, 0x03, 0x6d, 0x0b,
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
	if len(metrics.Functions) != 1 || !metrics.Functions[0].RailMachFinalized || metrics.Functions[0].ObligationsElided == 0 {
		t.Fatalf("%s discharged divisor check = %#v", runtime.GOARCH, metrics.Functions)
	}
}

func TestCompilerNativeAMD64EmitsImmediateSubLEA(t *testing.T) {
	if runtime.GOARCH != "amd64" {
		t.Skip("AMD64 post-RA realization")
	}
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0x00, 0x42, 0x07, 0x7d, 0x0b,
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
	if len(metrics.Functions) != 1 || !metrics.Functions[0].RailMachFinalized || metrics.Functions[0].PostRARewrites == 0 {
		t.Fatalf("AMD64 immediate subtraction LEA = %#v", metrics.Functions)
	}
}

func TestCompilerNativeAMD64ReportsMemoryFold(t *testing.T) {
	if runtime.GOARCH != "amd64" {
		t.Skip("AMD64 post-RA realization")
	}
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0x01, 0x20, 0x00, 0x28, 0x02, 0x00, 0x6a, 0x0b,
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
	if len(metrics.Functions) != 1 || metrics.Functions[0].MemoryFolds != 1 || metrics.Functions[0].PostRARewrites == 0 {
		t.Fatalf("AMD64 memory-fold metrics = %#v", metrics.Functions)
	}
}

func TestCompilerNativeRailMachSpillFinalization(t *testing.T) {
	params := make([]wasm.ValType, 21)
	for index := range params {
		params[index] = wasm.I64
	}
	body := make([]byte, 0, len(params)*3)
	for index := range params {
		body = append(body, 0x20)
		body = append(body, wasmtest.ULEB(uint32(index))...)
	}
	for range len(params) - 1 {
		body = append(body, 0x7c)
	}
	body = append(body, 0x0b)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	target, err := corecompiler.HostTarget(corecompiler.TargetNative)
	if err != nil {
		t.Fatal(err)
	}
	stack, err := railssa.BuildStackFunc(m, 0)
	if err != nil {
		t.Fatal(err)
	}
	var planner nativeBackendPlanner
	plan, err := planner.Plan(stack, target)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Allocation.SpillSlots == 0 {
		t.Fatalf("high-pressure %s function did not force a spill", runtime.GOARCH)
	}
	if plan.BackendAttempts != railmach.MaxBackendAttempts {
		t.Fatalf("high-pressure %s backend attempts = %d, want %d (debt=%d copies=%d cycles=%d)", runtime.GOARCH, plan.BackendAttempts, railmach.MaxBackendAttempts, plan.Allocation.Metrics.WeightedDebt, plan.Exit.Debt.Physical, plan.Exit.Debt.Cycles)
	}
	var metrics Metrics
	if _, err := (Compiler{Metrics: &metrics}).Compile(corecompiler.Input{Module: m, Source: source, Target: target}); err != nil {
		t.Fatal(err)
	}
	if len(metrics.Functions) != 1 || !metrics.Functions[0].RailMachFinalized || metrics.Functions[0].FrameBytes == 0 || metrics.Functions[0].AllocationStage != 4 || metrics.Functions[0].SpillSlots == 0 || metrics.Functions[0].BackendAttempts != railmach.MaxBackendAttempts {
		t.Fatalf("%s spill finalization = %#v", runtime.GOARCH, metrics.Functions)
	}
}

func TestCompilerNativeRailMachSpillEdgeFinalization(t *testing.T) {
	params := make([]wasm.ValType, 22)
	for index := range 21 {
		params[index] = wasm.I64
	}
	params[21] = wasm.I32
	body := []byte{0x20, 21, 0x04, 0x40}
	appendUpdates := func(delta byte) {
		for index := range 21 {
			body = append(body, 0x20, byte(index), 0x42, delta, 0x7c, 0x21, byte(index))
		}
	}
	appendUpdates(1)
	body = append(body, 0x05)
	appendUpdates(2)
	body = append(body, 0x0b)
	for index := range 21 {
		body = append(body, 0x20, byte(index))
	}
	for range 20 {
		body = append(body, 0x7c)
	}
	body = append(body, 0x0b)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	target, err := corecompiler.HostTarget(corecompiler.TargetNative)
	if err != nil {
		t.Fatal(err)
	}
	stack, err := railssa.BuildStackFunc(m, 0)
	if err != nil {
		t.Fatal(err)
	}
	var planner nativeBackendPlanner
	plan, err := planner.Plan(stack, target)
	if err != nil {
		t.Fatal(err)
	}
	spillMove := false
	for _, move := range plan.Exit.Moves {
		spillMove = spillMove || move.Src.Kind == railmach.LocationSpill || move.Dst.Kind == railmach.LocationSpill
	}
	if plan.Allocation.SpillSlots == 0 || !spillMove {
		t.Fatalf("%s high-pressure merge lacks spill edge: slots=%d moves=%#v", runtime.GOARCH, plan.Allocation.SpillSlots, plan.Exit.Moves)
	}
	var metrics Metrics
	if _, err := (Compiler{Metrics: &metrics}).Compile(corecompiler.Input{Module: m, Source: source, Target: target}); err != nil {
		t.Fatal(err)
	}
	if len(metrics.Functions) != 1 || !metrics.Functions[0].RailMachFinalized {
		t.Fatalf("%s spill-edge finalization = %#v", runtime.GOARCH, metrics.Functions)
	}
}

func TestCompilerNativeRailMachRematerializationFinalization(t *testing.T) {
	body := make([]byte, 0, 28*10)
	for index := range 28 {
		body = append(body, 0x44)
		var bits [8]byte
		binary.LittleEndian.PutUint64(bits[:], math.Float64bits(float64(index+1)))
		body = append(body, bits[:]...)
	}
	for range 27 {
		body = append(body, 0xa0)
	}
	body = append(body, 0x0b)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.F64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	target, err := corecompiler.HostTarget(corecompiler.TargetNative)
	if err != nil {
		t.Fatal(err)
	}
	stack, err := railssa.BuildStackFunc(m, 0)
	if err != nil {
		t.Fatal(err)
	}
	var planner nativeBackendPlanner
	plan, err := planner.Plan(stack, target)
	if err != nil {
		t.Fatal(err)
	}
	rematerialized := 0
	for _, location := range plan.Allocation.Locations {
		if location.Kind == railmach.LocationRematerialize {
			rematerialized++
		}
	}
	if rematerialized == 0 {
		t.Fatalf("high-pressure %s function did not rematerialize", runtime.GOARCH)
	}
	var metrics Metrics
	if _, err := (Compiler{Metrics: &metrics}).Compile(corecompiler.Input{Module: m, Source: source, Target: target}); err != nil {
		t.Fatal(err)
	}
	if len(metrics.Functions) != 1 || !metrics.Functions[0].RailMachFinalized {
		t.Fatalf("%s rematerialization finalization = %#v", runtime.GOARCH, metrics.Functions)
	}
}

func TestCompilerNativeRailMachControlFinalization(t *testing.T) {
	target, err := corecompiler.HostTarget(corecompiler.TargetNative)
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string][]byte{
		"if":       {0x20, 0, 0x04, 0x7f, 0x41, 7, 0x05, 0x41, 9, 0x0b, 0x0b},
		"cmp-if":   {0x20, 0, 0x41, 7, 0x48, 0x04, 0x7f, 0x41, 7, 0x05, 0x41, 9, 0x0b, 0x0b},
		"br_table": {0x02, 0x40, 0x02, 0x40, 0x20, 0, 0x0e, 2, 0, 1, 1, 0x0b, 0x41, 10, 0x0f, 0x0b, 0x41, 20, 0x0b},
	} {
		t.Run(name, func(t *testing.T) {
			source := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
			)
			m, err := wasm.DecodeModule(source)
			if err != nil {
				t.Fatal(err)
			}
			var metrics Metrics
			if _, err := (Compiler{Metrics: &metrics}).Compile(corecompiler.Input{Module: m, Source: source, Target: target}); err != nil {
				t.Fatal(err)
			}
			if len(metrics.Functions) != 1 || !metrics.Functions[0].RailMachFinalized {
				t.Fatalf("%s control finalization = %#v", runtime.GOARCH, metrics.Functions)
			}
			if name == "cmp-if" && metrics.Functions[0].PostRARewrites == 0 {
				t.Fatalf("%s compare/branch fusion was planned but not emitted: %#v", runtime.GOARCH, metrics.Functions[0])
			}
		})
	}
}

func TestCompilerNativeRailMachNestedLoopAndLoopIfFinalization(t *testing.T) {
	target, err := corecompiler.HostTarget(corecompiler.TargetNative)
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string][]byte{
		"loop_if": {
			0x02, 0x40, 0x03, 0x40,
			0x20, 0, 0x45, 0x0d, 1,
			0x20, 0, 0x45, 0x04, 0x40, 0x00, 0x0b,
			0x20, 0, 0x41, 1, 0x6b, 0x21, 0, 0x0c, 0,
			0x0b, 0x0b, 0x20, 0, 0x0b,
		},
		"nested_loop": {
			0x02, 0x40, 0x03, 0x40, 0x02, 0x40, 0x03, 0x40,
			0x20, 0, 0x45, 0x0d, 1,
			0x20, 0, 0x41, 1, 0x6b, 0x21, 0, 0x0c, 0,
			0x0b, 0x0b,
			0x20, 0, 0x45, 0x0d, 1, 0x0c, 0,
			0x0b, 0x0b, 0x20, 0, 0x0b,
		},
	} {
		t.Run(name, func(t *testing.T) {
			source := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
			)
			m, err := wasm.DecodeModule(source)
			if err != nil {
				t.Fatal(err)
			}
			var metrics Metrics
			if _, err := (Compiler{Metrics: &metrics}).Compile(corecompiler.Input{Module: m, Source: source, Target: target}); err != nil {
				t.Fatal(err)
			}
			if len(metrics.Functions) != 1 || !metrics.Functions[0].RailMachFinalized {
				t.Fatalf("%s RailMach finalization = %#v", runtime.GOARCH, metrics.Functions)
			}
		})
	}
}

func TestCompilerNativeRailMachDirectCallFinalization(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.F64}, []wasm.ValType{wasm.F64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x10, 0, 0x0b}),
		)),
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
	if len(metrics.Functions) != 2 || !metrics.Functions[0].RailMachFinalized || !metrics.Functions[1].RailMachFinalized || metrics.Functions[1].Relocations != 1 {
		t.Fatalf("%s direct-call finalization = %#v", runtime.GOARCH, metrics.Functions)
	}
}

func TestCompilerNativeRailMachIndirectCallFinalization(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x11, 0, 0, 0x0b}),
		)),
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
	if len(metrics.Functions) != 2 || !metrics.Functions[0].RailMachFinalized || !metrics.Functions[1].RailMachFinalized {
		t.Fatalf("%s indirect-call finalization = %#v", runtime.GOARCH, metrics.Functions)
	}
}

func TestCompilerNativeRailMachProfiledIndirectCallFinalization(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x11, 0, 0, 0x0b}),
		)),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	target, err := corecompiler.HostTarget(corecompiler.TargetNative)
	if err != nil {
		t.Fatal(err)
	}
	observations := &compilerprofile.Module{
		Version: compilerprofile.Version, ModuleHash: sha256.Sum256(source), Source: compilerprofile.SourceStatic, Phase: compilerprofile.PhaseSteady,
		CallTargets: []compilerprofile.TargetHistogram{{Site: compilerprofile.Site{Function: 1, Offset: 4}, Targets: []compilerprofile.TargetCount{{Function: 0, Count: 10}}}},
	}
	var metrics Metrics
	_, err = (Compiler{Metrics: &metrics}).Compile(corecompiler.Input{Module: m, Source: source, Target: target, Profile: observations})
	if err != nil {
		t.Fatal(err)
	}
	if len(metrics.Functions) != 2 || metrics.Functions[1].GuardedIndirectCalls != 1 || metrics.Functions[1].Relocations != 1 {
		t.Fatalf("%s profiled indirect-call finalization = %#v", runtime.GOARCH, metrics.Functions)
	}
}

func TestCompilerNativeRailMachIndirectFloatCallLiveFinalization(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.F64}, []wasm.ValType{wasm.F64}),
			wasmtest.FuncType([]wasm.ValType{wasm.F64, wasm.I32}, []wasm.ValType{wasm.F64}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x20, 0, 0x20, 1, 0x11, 0, 0, 0xa0, 0x0b}),
		)),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	target, err := corecompiler.HostTarget(corecompiler.TargetNative)
	if err != nil {
		t.Fatal(err)
	}
	stack, err := railssa.BuildStackFunc(m, 1)
	if err != nil {
		t.Fatal(err)
	}
	var planner nativeBackendPlanner
	plan, err := planner.Plan(stack, target)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOARCH == "amd64" && plan.ExternalCallFPRs == 0 {
		t.Fatal("AMD64 indirect float call-live mask is empty")
	}
	var metrics Metrics
	if _, err := (Compiler{Metrics: &metrics}).Compile(corecompiler.Input{Module: m, Source: source, Target: target}); err != nil {
		t.Fatal(err)
	}
	if len(metrics.Functions) != 2 || !metrics.Functions[0].RailMachFinalized || !metrics.Functions[1].RailMachFinalized || metrics.Functions[1].FrameBytes == 0 {
		t.Fatalf("%s indirect float call-live finalization = %#v", runtime.GOARCH, metrics.Functions)
	}
}

func TestCompilerNativeRailMachImportedCallFinalization(t *testing.T) {
	importEntry := append(wasmtest.Name("env"), wasmtest.Name("add")...)
	importEntry = append(importEntry, 0)
	importEntry = append(importEntry, wasmtest.ULEB(0)...)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(importEntry)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 20, 0x41, 22, 0x10, 0, 0x0b}))),
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
	if _, err := (Compiler{Metrics: &metrics}).Compile(corecompiler.Input{
		Module: m, Source: source, Target: target,
		HostEffects: []corecompiler.HostEffectBinding{{Declared: true, Contract: corecompiler.HostEffectContract{Reads: corecompiler.HostHeapGlobal}}},
	}); err != nil {
		t.Fatal(err)
	}
	if len(metrics.Functions) != 1 || !metrics.Functions[0].RailMachFinalized || metrics.Functions[0].Relocations != 0 || metrics.Functions[0].HostEffectSpecializations != 1 {
		t.Fatalf("%s imported-call finalization = %#v", runtime.GOARCH, metrics.Functions)
	}
}

func TestCompilerNativeRailMachImportedFloatCallLiveFinalization(t *testing.T) {
	importEntry := append(wasmtest.Name("env"), wasmtest.Name("step")...)
	importEntry = append(importEntry, 0)
	importEntry = append(importEntry, wasmtest.ULEB(0)...)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.F64}, []wasm.ValType{wasm.F64}))),
		wasmtest.Section(2, wasmtest.Vec(importEntry)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x20, 0, 0x10, 0, 0xa0, 0x0b}))),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	target, err := corecompiler.HostTarget(corecompiler.TargetNative)
	if err != nil {
		t.Fatal(err)
	}
	stack, err := railssa.BuildStackFunc(m, 0)
	if err != nil {
		t.Fatal(err)
	}
	var planner nativeBackendPlanner
	plan, err := planner.Plan(stack, target)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOARCH == "amd64" && (plan.ExternalCallFPRs == 0 || plan.Frame.TotalBytes == 0) {
		t.Fatalf("AMD64 external float preservation = mask %#x frame %d", plan.ExternalCallFPRs, plan.Frame.TotalBytes)
	}
	var metrics Metrics
	if _, err := (Compiler{Metrics: &metrics}).Compile(corecompiler.Input{Module: m, Source: source, Target: target}); err != nil {
		t.Fatal(err)
	}
	if len(metrics.Functions) != 1 || !metrics.Functions[0].RailMachFinalized || metrics.Functions[0].FrameBytes == 0 {
		t.Fatalf("%s imported float call-live finalization = %#v", runtime.GOARCH, metrics.Functions)
	}
}
