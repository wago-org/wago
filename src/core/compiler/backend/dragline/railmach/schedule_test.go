package railmach

import (
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railssa"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestHasScheduleAlternativesRequiresRealOrderingFreedom(t *testing.T) {
	f := &Func{Insts: make([]Inst, 3), Blocks: []Block{{InstCount: 3}}}
	serial := &DependencyDAG{
		Offsets:      []uint32{0, 0, 1, 2},
		Dependencies: []Dependency{{Instruction: 0, Kind: DependencyData}, {Instruction: 1, Kind: DependencyData}},
	}
	if HasScheduleAlternatives(f, serial, nil) {
		t.Fatal("direct predecessor chain reported schedule alternatives")
	}
	parallel := &DependencyDAG{Offsets: []uint32{0, 0, 0, 0}}
	if !HasScheduleAlternatives(f, parallel, nil) {
		t.Fatal("independent instructions reported a unique schedule")
	}
	if !HasScheduleAlternatives(f, serial, &railssa.PressurePlan{LICM: []railssa.LICMMove{{}}}) {
		t.Fatal("cross-block LICM opportunity reported a unique schedule")
	}
	if !HasScheduleAlternatives(f, &DependencyDAG{}, nil) {
		t.Fatal("malformed dependency graph suppressed alternate schedules")
	}
}

func buildScheduleTest(t *testing.T, target Target, m *wasm.Module) (*Func, *SelectionPlan, *railssa.Metadata, *DependencyDAG) {
	t.Helper()
	stack, err := railssa.BuildStackFunc(m, 0)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := railssa.BuildCFG(stack, nil)
	if err != nil {
		t.Fatal(err)
	}
	locals, err := railssa.BuildLocalSSA(stack, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	flow, err := railssa.BuildValueFlow(stack, cfg, locals, nil)
	if err != nil {
		t.Fatal(err)
	}
	semantic, err := railssa.BuildSemanticFunc(stack, cfg, flow, nil)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := railssa.BuildMetadata(stack, nil)
	if err != nil {
		t.Fatal(err)
	}
	simplified, err := railssa.SparseSimplify(stack, cfg, flow, semantic, metadata, railssa.DefaultSimplifyConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := SelectOrder(target, flow, semantic, simplified, nil)
	if err != nil {
		t.Fatal(err)
	}
	machine, err := Build(target, cfg, flow, semantic, nil)
	if err != nil {
		t.Fatal(err)
	}
	dag, err := BuildDependencyDAG(machine, selection, metadata, nil)
	if err != nil {
		t.Fatal(err)
	}
	return machine, selection, metadata, dag
}

func TestDependencyDAGAndScheduleCandidates(t *testing.T) {
	m := machineModule(nil, []wasm.ValType{wasm.I64}, []byte{
		0x42, 0x01,
		0x42, 0x02,
		0x7c,
		0x42, 0x03,
		0x42, 0x04,
		0x7c,
		0x7c,
		0x0b,
	})
	f, selection, _, dag := buildScheduleTest(t, TargetARM64, m)
	if len(dag.Dependencies) == 0 {
		t.Fatal("dependency DAG is empty")
	}
	if len(dag.SuccessorOffsets) != len(f.Insts)+1 || len(dag.Successors) != len(dag.Dependencies) {
		t.Fatalf("dependency reverse graph = offsets:%d successors:%d, want %d and %d", len(dag.SuccessorOffsets), len(dag.Successors), len(f.Insts)+1, len(dag.Dependencies))
	}
	for _, kind := range []ScheduleKind{ScheduleKindSourceStable, ScheduleKindLatencyFusion, ScheduleKindPressure} {
		schedule, err := BuildSchedule(f, selection, dag, kind, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(schedule.Order) != len(f.Insts) {
			t.Fatalf("%d order = %v", kind, schedule.Order)
		}
	}
	source, err := BuildSchedule(f, selection, dag, ScheduleKindSourceStable, nil)
	if err != nil {
		t.Fatal(err)
	}
	for index, instruction := range source.Order {
		if int(instruction) != index {
			t.Fatalf("source-stable order = %v", source.Order)
		}
	}
}

func TestDependencyDAGSortedCombinationFastPathMatchesFallback(t *testing.T) {
	f := &Func{
		Target: TargetARM64,
		Insts: []Inst{
			{Op: wasm.InstrNop, Source: 0},
			{Op: wasm.InstrNop, Source: 1},
			{Op: wasm.InstrNop, Source: 2},
			{Op: wasm.InstrNop, Source: 3},
		},
		VRegs:  []VRegData{{}},
		Blocks: []Block{{InstCount: 4}},
	}
	metadata := &railssa.Metadata{Instructions: make([]railssa.InstructionMetadata, 4)}
	sorted := &SelectionPlan{
		Selections: make([]Selection, 4),
		Combinations: []Combination{
			{Producer: 0, Consumer: 2, Kind: CombineImmediate},
			{Producer: 1, Consumer: 3, Kind: CombineAddress},
		},
	}
	fast, err := BuildDependencyDAG(f, sorted, metadata, nil)
	if err != nil {
		t.Fatal(err)
	}
	unsorted := &SelectionPlan{
		Selections: sorted.Selections,
		Combinations: []Combination{
			sorted.Combinations[1],
			sorted.Combinations[0],
		},
	}
	fallback, err := BuildDependencyDAG(f, unsorted, metadata, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(fast.Offsets, fallback.Offsets) || !slices.Equal(fast.Dependencies, fallback.Dependencies) || !slices.Equal(fast.SuccessorOffsets, fallback.SuccessorOffsets) || !slices.Equal(fast.Successors, fallback.Successors) {
		t.Fatalf("sorted combination DAG differs from fallback:\nfast=%#v\nfallback=%#v", fast, fallback)
	}
}

func TestScheduleReadyFrontierMatchesDependencyScan(t *testing.T) {
	m := machineModule(nil, []wasm.ValType{wasm.I64}, []byte{
		0x42, 0x01,
		0x42, 0x02,
		0x7c,
		0x42, 0x03,
		0x42, 0x04,
		0x7c,
		0x7c,
		0x0b,
	})
	f, selection, _, dag := buildScheduleTest(t, TargetARM64, m)
	scanned := *dag
	scanned.SuccessorOffsets = nil
	scanned.Successors = nil
	pressure := &railssa.PressurePlan{Blocks: []railssa.BlockPressure{{PeakGPR: uint16(DefaultLinearQConfig(TargetARM64).GPRs)}}}
	for _, kind := range []ScheduleKind{ScheduleKindSourceStable, ScheduleKindLatencyFusion, ScheduleKindPressure} {
		want, err := BuildScheduleWithPressure(f, selection, &scanned, kind, pressure, nil)
		if err != nil {
			t.Fatal(err)
		}
		got, err := BuildScheduleWithPressure(f, selection, dag, kind, pressure, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(got.Order, want.Order) {
			t.Fatalf("%d ready-frontier order = %v, dependency-scan order = %v", kind, got.Order, want.Order)
		}
		got.readyCandidates = append(got.readyCandidates, ^uint32(0))
		got, err = BuildScheduleWithPressure(f, selection, dag, kind, pressure, got)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(got.Order, want.Order) {
			t.Fatalf("%d reused ready-frontier order = %v, dependency-scan order = %v", kind, got.Order, want.Order)
		}
	}
}

func TestLatencyScheduleScansDynamicLastUsePriorities(t *testing.T) {
	f := &Func{
		Target: TargetARM64,
		Insts: []Inst{
			{OperandStart: 0, OperandCount: 1, Result: 3, Op: wasm.InstrI64Add},
			{OperandStart: 1, OperandCount: 1, Result: 4, Op: wasm.InstrI64Mul},
			{OperandStart: 2, OperandCount: 2, Result: 5, Op: wasm.InstrI64Mul},
		},
		Operands: []Operand{
			{Reg: 1, Bank: BankGPR},
			{Reg: 2, Bank: BankGPR},
			{Reg: 4, Bank: BankGPR}, {Reg: 2, Bank: BankGPR},
		},
		VRegs:  make([]VRegData, 6),
		Blocks: []Block{{InstCount: 3}},
	}
	selection := &SelectionPlan{Selections: []Selection{
		{Cost: SelectCost{Latency: 1, ResourceCost: 1}},
		{Cost: SelectCost{Latency: 2, ResourceCost: 4}},
		{Cost: SelectCost{Latency: 1, ResourceCost: 4}},
	}}
	dag := &DependencyDAG{
		Offsets:      []uint32{0, 0, 0, 1},
		Dependencies: []Dependency{{Instruction: 1, Kind: DependencyData}},
	}
	pressure := &railssa.PressurePlan{Blocks: []railssa.BlockPressure{{PeakGPR: uint16(DefaultLinearQConfig(TargetARM64).GPRs)}}}
	schedule, err := BuildScheduleWithPressure(f, selection, dag, ScheduleKindLatencyFusion, pressure, nil)
	if err != nil {
		t.Fatal(err)
	}
	if first := schedule.Order[0]; first != 0 {
		t.Fatalf("first instruction = %d, want last-use instruction 0; order=%v", first, schedule.Order)
	}
	pressure.Blocks[0].PeakGPR--
	schedule, err = BuildScheduleWithPressure(f, selection, dag, ScheduleKindLatencyFusion, pressure, schedule)
	if err != nil {
		t.Fatal(err)
	}
	if first := schedule.Order[0]; first != 1 {
		t.Fatalf("low-pressure first instruction = %d, want critical-path instruction 1; order=%v", first, schedule.Order)
	}
	for index := range schedule.criticalHeight {
		schedule.criticalHeight[index] = 1000
	}
	schedule, err = BuildScheduleWithPressure(f, selection, dag, ScheduleKindLatencyFusion, pressure, schedule)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := schedule.criticalHeight, []uint64{1, 3, 1}; !slices.Equal(got, want) {
		t.Fatalf("reused critical heights = %v, want %v", got, want)
	}
}

func TestAMD64ScheduleInstructionLatencyUsesOpcodeCosts(t *testing.T) {
	tests := []struct {
		op   wasm.InstrKind
		want uint16
	}{
		{wasm.InstrI32Add, 1},
		{wasm.InstrI32Mul, 3},
		{wasm.InstrI64Load, 4},
		{wasm.InstrF64ConvertI64S, 5},
		{wasm.InstrF64Div, 14},
		{wasm.InstrI64RemU, 16},
		{wasm.InstrF64Sqrt, 18},
	}
	for _, test := range tests {
		if got := scheduleInstructionLatency(TargetAMD64, test.op, 1); got != test.want {
			t.Errorf("%s latency = %d, want %d", test.op, got, test.want)
		}
	}
	if got := scheduleInstructionLatency(TargetARM64, wasm.InstrF64Sqrt, 2); got != 2 {
		t.Fatalf("ARM64 latency = %d, want selection fallback 2", got)
	}
}

func TestDependencyDAGUsesRefinedHeapAliases(t *testing.T) {
	f := &Func{
		Target: TargetARM64,
		Insts:  []Inst{{Op: wasm.InstrNop, Source: 0}, {Op: wasm.InstrNop, Source: 1}, {Op: wasm.InstrNop, Source: 2}},
		VRegs:  []VRegData{{}}, Blocks: []Block{{InstCount: 3}},
	}
	selection := &SelectionPlan{Selections: make([]Selection, 3)}
	metadata := &railssa.Metadata{Instructions: []railssa.InstructionMetadata{
		{Reads: railssa.HeapLinearMemory},
		{Reads: railssa.HeapGlobal, Flags: railssa.EffectCall},
		{Reads: railssa.HeapLinearMemory},
	}}
	dag, err := BuildDependencyDAG(f, selection, metadata, nil)
	if err != nil {
		t.Fatal(err)
	}
	if hasScheduleDependency(dag, 1, 0, DependencyEffect) {
		t.Fatal("global-only host call was serialized behind disjoint linear memory")
	}
	if !hasScheduleDependency(dag, 2, 0, DependencyEffect) {
		t.Fatal("same-heap linear memory accesses lost their dependency")
	}

	metadata.Instructions[1].Flags |= railssa.EffectMayReenter
	dag, err = BuildDependencyDAG(f, selection, metadata, dag)
	if err != nil {
		t.Fatal(err)
	}
	if !hasScheduleDependency(dag, 1, 0, DependencyEffect) || !hasScheduleDependency(dag, 2, 1, DependencyEffect) {
		t.Fatalf("reentrant host barrier dependencies = %#v", dag.Dependencies)
	}
}

func hasScheduleDependency(dag *DependencyDAG, instruction, predecessor uint32, kind DependencyKind) bool {
	for _, dependency := range dag.Dependencies[dag.Offsets[instruction]:dag.Offsets[instruction+1]] {
		if dependency.Instruction == predecessor && dependency.Kind&kind != 0 {
			return true
		}
	}
	return false
}

func TestPressureSinkWithElidedConsumerIsIgnored(t *testing.T) {
	f := &Func{
		Insts:  []Inst{{Op: wasm.InstrI32Const, Result: 1}, {Op: wasm.InstrReturn}},
		VRegs:  []VRegData{{}, {Bank: BankGPR, Type: TypeI32}},
		Blocks: []Block{{InstCount: 2}},
	}
	sink := railssa.SinkMove{Instruction: 0, Before: 1, Block: 0}
	if !pressureSinkInvalidatedByElision(f, sink, []uint32{0, 0}) {
		t.Fatal("zero-use machine result retained a stale pressure sink")
	}
}

func TestDecideRetryIsBounded(t *testing.T) {
	allocation := &GreedyAllocation{Metrics: GreedyMetrics{WeightedDebt: 300}}
	if decision := DecideRetry(0, allocation, CopyDebt{}); !decision.Retry || decision.Reason != 1 {
		t.Fatalf("first decision = %#v", decision)
	}
	if decision := DecideRetry(1, allocation, CopyDebt{Cycles: 99}); decision.Retry {
		t.Fatalf("second decision exceeds attempt bound: %#v", decision)
	}
	if decision := DecideRetry(0, &GreedyAllocation{Metrics: GreedyMetrics{WeightedDebt: 4}}, CopyDebt{}); decision.Retry {
		t.Fatalf("low debt requested retry: %#v", decision)
	}
	large := &GreedyAllocation{Allocation: Allocation{Intervals: make([]LiveInterval, 1024)}, Metrics: GreedyMetrics{WeightedDebt: 20}}
	if decision := DecideRetry(0, large, CopyDebt{}); decision.Retry {
		t.Fatalf("sparse large-function debt requested retry: %#v", decision)
	}
}

func TestScheduleCommitsIntegerCompareBranchFusion(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0x00, 0x41, 0x07, 0x48,
			0x41, 0x00, 0x28, 0x02, 0x00, 0x1a,
			0x04, 0x7f, 0x41, 0x01, 0x05, 0x41, 0x02, 0x0b, 0x0b,
		}))),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	for _, target := range []Target{TargetAMD64, TargetARM64} {
		t.Run(target.String(), func(t *testing.T) {
			f, selection, _, dag := buildScheduleTest(t, target, m)
			schedule, err := BuildSchedule(f, selection, dag, ScheduleKindSourceStable, nil)
			if err != nil {
				t.Fatal(err)
			}
			if schedule.CommittedFusions == 0 {
				t.Fatalf("schedule did not commit selected fusion: %v", schedule.Order)
			}
			position := make([]uint32, len(f.Insts))
			for index, instruction := range schedule.Order {
				position[instruction] = uint32(index)
			}
			compare, load, control := ^uint32(0), ^uint32(0), ^uint32(0)
			for id, instruction := range f.Insts {
				switch instruction.Op {
				case wasm.InstrI32LtS:
					compare = uint32(id)
				case wasm.InstrI32Load:
					load = uint32(id)
				case wasm.InstrIf:
					control = uint32(id)
				}
			}
			if compare == ^uint32(0) || load == ^uint32(0) || control == ^uint32(0) || position[load] >= position[compare] || position[control] != position[compare]+1 {
				t.Fatalf("fusion order = %v (load=%d compare=%d control=%d)", schedule.Order, load, compare, control)
			}
		})
	}
}

func TestPressureScheduleCommitsCheapSinkAdjacentToConsumer(t *testing.T) {
	m := machineModule(nil, []wasm.ValType{wasm.I64}, []byte{
		0x42, 0x01,
		0x42, 0x02,
		0x42, 0x03,
		0x7c,
		0x7c,
		0x0b,
	})
	f, selection, _, dag := buildScheduleTest(t, TargetARM64, m)
	pressure := &railssa.PressurePlan{Sinks: []railssa.SinkMove{{Instruction: 0, Before: 4, Block: 0}}}
	schedule, err := BuildScheduleWithPressure(f, selection, dag, ScheduleKindPressure, pressure, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := schedule.Order, []uint32{1, 2, 3, 0, 4}; !slices.Equal(got, want) {
		t.Fatalf("pressure order = %v, want %v", got, want)
	}
}

func TestDropUncommittedMemoryPairs(t *testing.T) {
	f := &Func{Insts: []Inst{
		{Op: wasm.InstrI32Load},
		{Op: wasm.InstrNop},
		{Op: wasm.InstrI32Load},
	}}
	const none = ^uint32(0)
	for _, test := range []struct {
		name      string
		order     []uint32
		committed uint32
		consumer  uint32
		source    uint32
	}{
		{name: "adjacent", order: []uint32{0, 2, 1}, committed: 1, consumer: 2, source: 0},
		{name: "separated", order: []uint32{0, 1, 2}, committed: 0, consumer: none, source: none},
	} {
		t.Run(test.name, func(t *testing.T) {
			schedule := &Schedule{
				Order:            slices.Clone(test.order),
				CommittedFusions: 1,
				fusionBefore:     []uint32{2, none, none},
				fusionSource:     []uint32{none, none, 0},
			}
			dropUncommittedMemoryPairs(f, schedule)
			if schedule.CommittedFusions != test.committed || schedule.fusionBefore[0] != test.consumer {
				t.Fatalf("committed=%d fusion=%v, want committed=%d consumer=%d", schedule.CommittedFusions, schedule.fusionBefore, test.committed, test.consumer)
			}
			if got := schedule.fusionSource[2]; got != test.source {
				t.Fatalf("fusion source = %d, want %d", got, test.source)
			}
		})
	}
}

func TestHoistARM64AdjacentLoadAddresses(t *testing.T) {
	const none = ^uint32(0)
	f := &Func{
		Target: TargetARM64,
		Insts: []Inst{
			{Op: wasm.InstrI32Load, Aux: 8, Result: 3, OperandStart: 0, OperandCount: 1},
			{Op: wasm.InstrI32Add, Result: 4, OperandStart: 1, OperandCount: 2},
			{Op: wasm.InstrI32Load, Aux: 24, Result: 5, OperandStart: 3, OperandCount: 1},
		},
		Operands: []Operand{{Reg: 1}, {Reg: 2}, {Reg: 6}, {Reg: 4}},
		VRegs:    make([]VRegData, 7),
	}
	dag := &DependencyDAG{
		Offsets: []uint32{0, 0, 0, 2},
		Dependencies: []Dependency{
			{Instruction: 0, Kind: DependencyTrap},
			{Instruction: 1, Kind: DependencyData},
		},
	}
	schedule := &Schedule{
		Order:          []uint32{0, 1, 2},
		BlockRanges:    []MoveRange{{Count: 3}},
		uses:           make([]uint32, 7),
		fusionBefore:   []uint32{none, none, none},
		fusionSource:   []uint32{none, none, none},
		sinkBefore:     []uint32{none, none, none},
		sinkProducer:   []uint32{none, none, none},
		lateBefore:     []uint32{none, none, none},
		lateProducer:   []uint32{none, none, none},
		verifyPosition: make([]uint32, 3),
	}
	schedule.uses[4] = 1
	if committed := hoistARM64AdjacentLoadAddresses(f, dag, schedule); committed != 1 || !slices.Equal(schedule.Order, []uint32{1, 0, 2}) {
		t.Fatalf("committed=%d order=%v", committed, schedule.Order)
	}

	schedule.Order = []uint32{0, 1, 2}
	dag.Offsets = []uint32{0, 0, 1, 3}
	dag.Dependencies = []Dependency{{Instruction: 0, Kind: DependencyData}, {Instruction: 0, Kind: DependencyTrap}, {Instruction: 1, Kind: DependencyData}}
	if committed := hoistARM64AdjacentLoadAddresses(f, dag, schedule); committed != 0 || !slices.Equal(schedule.Order, []uint32{0, 1, 2}) {
		t.Fatalf("dependent committed=%d order=%v", committed, schedule.Order)
	}

	f = &Func{
		Target: TargetARM64,
		Insts: []Inst{
			{Op: wasm.InstrF64Load, Result: 3, OperandStart: 0, OperandCount: 1},
			{Op: wasm.InstrI32Const, Result: 4},
			{Op: wasm.InstrI32Add, Result: 5, OperandStart: 1, OperandCount: 2},
			{Op: wasm.InstrF64Load, Result: 6, OperandStart: 3, OperandCount: 1},
		},
		Operands: []Operand{{Reg: 1}, {Reg: 2}, {Reg: 4}, {Reg: 5}},
		VRegs:    make([]VRegData, 7),
	}
	dag = &DependencyDAG{
		Offsets: []uint32{0, 0, 0, 1, 3},
		Dependencies: []Dependency{
			{Instruction: 1, Kind: DependencyData},
			{Instruction: 0, Kind: DependencyTrap},
			{Instruction: 2, Kind: DependencyData},
		},
	}
	schedule = &Schedule{
		Order:          []uint32{0, 1, 2, 3},
		BlockRanges:    []MoveRange{{Count: 4}},
		uses:           make([]uint32, 7),
		fusionBefore:   []uint32{none, none, none, none},
		fusionSource:   []uint32{none, none, none, none},
		sinkBefore:     []uint32{none, none, none, none},
		sinkProducer:   []uint32{none, none, none, none},
		lateBefore:     []uint32{none, none, none, none},
		lateProducer:   []uint32{none, none, none, none},
		verifyPosition: make([]uint32, 4),
	}
	schedule.uses[5] = 1
	if committed := hoistARM64AdjacentLoadAddresses(f, dag, schedule); committed != 1 || !slices.Equal(schedule.Order, []uint32{1, 2, 0, 3}) {
		t.Fatalf("constant committed=%d order=%v", committed, schedule.Order)
	}
}
