package dragline

import (
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railmach"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func TestCalleeFirstCompilationOrder(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x10, 2, 0x0b}), // 0 -> 2
			wasmtest.Code([]byte{0x0b}),          // unrelated
			wasmtest.Code([]byte{0x10, 3, 0x0b}), // 2 -> 3
			wasmtest.Code([]byte{0x10, 4, 0x0b}), // recursive SCC 3 <-> 4
			wasmtest.Code([]byte{0x10, 3, 0x0b}),
		)),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	plan := calleeFirstCompilationPlan(m)
	got := plan.Order
	want := []int{3, 4, 2, 0, 1}
	if !slices.Equal(got, want) {
		t.Fatalf("compilation order = %v, want %v", got, want)
	}
	if plan.Component[3] != plan.Component[4] || plan.Component[2] == plan.Component[3] {
		t.Fatalf("SCC components = %v", plan.Component)
	}
	if !plan.Recursive[3] || !plan.Recursive[4] || plan.Recursive[2] {
		t.Fatalf("recursive components = %v", plan.Recursive)
	}
}

func TestCalleeFirstCompilationPlanMarksSelfRecursion(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0, 0x0b}))),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	plan := calleeFirstCompilationPlan(m)
	if len(plan.Recursive) != 1 || !plan.Recursive[0] {
		t.Fatalf("self-recursive component = %v", plan.Recursive)
	}
}

func TestCalleeFirstCompilationPlanRecordsModuleSIMD(t *testing.T) {
	body := append([]byte{0xfd, 0x0c}, make([]byte, 16)...)
	body = append(body, 0x1a, 0x0b)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}), wasmtest.Code(body))),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if plan := calleeFirstCompilationPlan(m); !plan.HasV128 {
		t.Fatal("SIMD module was not recorded in compilation plan")
	}
}

func TestVerifyRecursiveContractClosureRejectsTampering(t *testing.T) {
	compilation := compilationPlan{Component: []int{0, 0}, Recursive: []bool{true, true}}
	seeds := []railmach.ABIContract{{Class: railmach.ABIGeneral, GPRClobbers: 1}, {Class: railmach.ABIGeneral, GPRClobbers: 2}}
	contracts := []railmach.ABIContract{{Class: railmach.ABIGeneral, GPRClobbers: 3}, {Class: railmach.ABIGeneral, GPRClobbers: 3}}
	candidates := []bool{true, true}
	config := railmach.DefaultGreedyConfig(railmach.TargetAMD64)
	if !verifyRecursiveContractClosure(compilation, 0, seeds, candidates, contracts, config) {
		t.Fatal("valid recursive contract closure rejected")
	}
	contracts[1].GPRClobbers = 2
	if verifyRecursiveContractClosure(compilation, 0, seeds, candidates, contracts, config) {
		t.Fatal("tampered recursive contract closure accepted")
	}
}

func TestRecursiveRefinementKeepsBetterCompletePlan(t *testing.T) {
	contract := railmach.ABIContract{GPRClobbers: 3}
	conservative := railmach.ScheduleScore{WeightedSpillDebt: 1}
	refined := &nativeBackendPlan{LocalABI: railmach.ABIContract{GPRClobbers: 1}, Score: railmach.ScheduleScore{WeightedSpillDebt: 0}}
	if !recursiveRefinementPreferred(refined, contract, conservative) {
		t.Fatal("better contained refinement rejected")
	}
	refined.Score.WeightedSpillDebt = 2
	if recursiveRefinementPreferred(refined, contract, conservative) {
		t.Fatal("worse refinement accepted")
	}
	refined.Score.WeightedSpillDebt = 0
	refined.LocalABI.GPRClobbers = 4
	if recursiveRefinementPreferred(refined, contract, conservative) {
		t.Fatal("refinement outside published contract accepted")
	}
}

func TestCalleeFirstCompilationOrderWithoutCallsIsSourceStable(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32, wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}), wasmtest.Code([]byte{0x0b}))),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if got := calleeFirstCompilationPlan(m).Order; !slices.Equal(got, []int{0, 1}) {
		t.Fatalf("fallback order = %v", got)
	}
}
