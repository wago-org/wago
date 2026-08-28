package railmach

import (
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railssa"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

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
