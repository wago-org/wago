package railmach

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestMeasureScheduleFreedom(t *testing.T) {
	f := &Func{
		Target: TargetARM64,
		VRegs:  []VRegData{{}},
		Insts: []Inst{
			{Op: wasm.InstrNop, Source: 0},
			{Op: wasm.InstrNop, Source: 1},
			{Op: wasm.InstrNop, Source: 2},
			{Op: wasm.InstrReturn, Source: 3},
		},
		Blocks: []Block{{InstCount: 4}},
	}
	selection := &SelectionPlan{Selections: []Selection{
		{Cost: SelectCost{Latency: 2}},
		{Cost: SelectCost{Latency: 1}},
		{Cost: SelectCost{Latency: 3}},
		{Cost: SelectCost{Latency: 4}},
	}}
	dag := &DependencyDAG{
		Offsets: []uint32{0, 0, 0, 2, 3},
		Dependencies: []Dependency{
			{Instruction: 0, Kind: DependencyData},
			{Instruction: 1, Kind: DependencyData},
			{Instruction: 2, Kind: DependencyData},
		},
	}
	metrics, err := MeasureScheduleFreedom(f, selection, dag)
	if err != nil {
		t.Fatal(err)
	}
	if metrics.ReadySteps != 4 || metrics.ReadyWidthTotal != 5 || metrics.ReadyWidthMax != 2 || metrics.CriticalPathCost != 9 {
		t.Fatalf("schedule freedom = %#v", metrics)
	}
}

func TestMeasureScheduleFreedomDoesNotEnqueueSuccessorBlocksTwice(t *testing.T) {
	f := &Func{
		Target: TargetARM64,
		VRegs:  []VRegData{{}},
		Insts: []Inst{
			{Op: wasm.InstrNop, Source: 0},
			{Op: wasm.InstrNop, Source: 1},
			{Op: wasm.InstrNop, Source: 2},
			{Op: wasm.InstrReturn, Source: 3},
		},
		Blocks: []Block{{InstCount: 2}, {InstStart: 2, InstCount: 2}},
	}
	selection := &SelectionPlan{Selections: make([]Selection, 4)}
	dag := &DependencyDAG{
		Offsets: []uint32{0, 0, 0, 1, 2},
		Dependencies: []Dependency{
			{Instruction: 0, Kind: DependencyEffect},
			{Instruction: 2, Kind: DependencyData},
		},
	}
	metrics, err := MeasureScheduleFreedom(f, selection, dag)
	if err != nil {
		t.Fatal(err)
	}
	if metrics.ReadySteps != 4 || metrics.ReadyWidthTotal != 5 || metrics.ReadyWidthMax != 2 {
		t.Fatalf("cross-block schedule freedom = %#v", metrics)
	}
}
