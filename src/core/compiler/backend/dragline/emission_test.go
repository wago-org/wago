package dragline

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railssa"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func constantMemoryEmissionTestFunc(t *testing.T) (*railssa.Func, *railssa.EmissionPlan) {
	t.Helper()
	typeSec := wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})))
	funcSec := wasmtest.Section(3, wasmtest.Vec([]byte{0}))
	memorySec := wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01}))
	body := []byte{0x02, 0x7f, 0x41, 0x00, 0x28, 0x02, 0x00, 0x0b, 0x0b}
	codeSec := wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body)))
	m, err := wasm.DecodeModule(wasmtest.Module(typeSec, funcSec, memorySec, codeSec))
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
	var planner railssa.EmissionPlanner
	plan, err := planner.Plan(stack)
	if err != nil {
		t.Fatal(err)
	}
	return &railssa.Func{Index: 0, Params: stack.Params, Results: stack.Results, Stack: stack}, plan
}

func maskedMemoryEmissionTestFunc(t *testing.T) (*railssa.Func, *railssa.EmissionPlan) {
	t.Helper()
	typeSec := wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})))
	funcSec := wasmtest.Section(3, wasmtest.Vec([]byte{0}))
	memorySec := wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01}))
	body := []byte{0x20, 0x00, 0x41}
	body = append(body, wasmtest.SLEB32(65528)...)
	body = append(body, 0x71, 0x28, 0x02, 0x00, 0x0b)
	codeSec := wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body)))
	m, err := wasm.DecodeModule(wasmtest.Module(typeSec, funcSec, memorySec, codeSec))
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
	if !railssa.NeedsEmissionPlan(stack) {
		t.Fatal("masked address did not request emission planning")
	}
	var planner railssa.EmissionPlanner
	plan, err := planner.Plan(stack)
	if err != nil {
		t.Fatal(err)
	}
	if plan.ElidedBoundsChecks() != 1 {
		t.Fatalf("masked address elisions = %d", plan.ElidedBoundsChecks())
	}
	return &railssa.Func{Index: 0, Params: stack.Params, Results: stack.Results, Stack: stack}, plan
}

func maskedLoopMemoryEmissionTestFunc(t *testing.T) (*railssa.Func, *railssa.EmissionPlan) {
	t.Helper()
	typeSec := wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})))
	funcSec := wasmtest.Section(3, wasmtest.Vec([]byte{0}))
	memorySec := wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01}))
	body := []byte{
		0x02, 0x40,
		0x03, 0x40,
		0x20, 0x01,
		0x28, 0x02, 0x00,
		0x1a,
		0x20, 0x01,
		0x41, 0x08,
		0x6a,
		0x41,
	}
	body = append(body, wasmtest.SLEB32(65535)...)
	body = append(body,
		0x71,
		0x21, 0x01,
		0x20, 0x00,
		0x0d, 0x00,
		0x0b,
		0x0b,
		0x41, 0x00,
		0x0b,
	)
	function := append([]byte{0x01, 0x01, 0x7f}, body...)
	code := append(wasmtest.ULEB(uint32(len(function))), function...)
	m, err := wasm.DecodeModule(wasmtest.Module(typeSec, funcSec, memorySec, wasmtest.Section(10, wasmtest.Vec(code))))
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
	var planner railssa.EmissionPlanner
	plan, err := planner.Plan(stack)
	if err != nil {
		t.Fatal(err)
	}
	if plan.ElidedBoundsChecks() != 1 {
		t.Fatalf("masked loop elisions = %d", plan.ElidedBoundsChecks())
	}
	return &railssa.Func{Index: 0, Params: stack.Params, Results: stack.Results, Stack: stack, Structured: stack}, plan
}
