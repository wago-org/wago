package railssa

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func TestPressureShapePlansSinkingAndRematerialization(t *testing.T) {
	m := scalarModule(nil, []wasm.ValType{wasm.I32}, []byte{
		0x41, 0x01,
		0x41, 0x02,
		0x41, 0x03,
		0x6a,
		0x1a,
		0x41, 0x04,
		0x6a,
		0x0b,
	})
	f, cfg, flow, semantic, metadata, simplified := buildSimplifyTest(t, m)
	plan, err := PressureShape(f, cfg, flow, semantic, metadata, simplified, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Remats) != 4 {
		t.Fatalf("rematerializations = %#v", plan.Remats)
	}
	if len(plan.Sinks) == 0 {
		t.Fatalf("sinks = %#v", plan.Sinks)
	}
	if plan.Blocks[0].PeakGPR < 2 {
		t.Fatalf("pressure = %#v", plan.Blocks[0])
	}
}

func TestPressureShapeReusesLinearScratch(t *testing.T) {
	m := scalarModule(nil, []wasm.ValType{wasm.I64}, []byte{
		0x42, 0x01,
		0x42, 0x02,
		0x7c,
		0x0b,
	})
	f, cfg, flow, semantic, metadata, simplified := buildSimplifyTest(t, m)
	var plan PressurePlan
	if _, err := PressureShape(f, cfg, flow, semantic, metadata, simplified, &plan); err != nil {
		t.Fatal(err)
	}
	var shapeErr error
	allocs := testing.AllocsPerRun(10, func() {
		_, shapeErr = PressureShape(f, cfg, flow, semantic, metadata, simplified, &plan)
	})
	if shapeErr != nil {
		t.Fatal(shapeErr)
	}
	if allocs != 0 {
		t.Fatalf("warm pressure planning allocations = %g, want 0", allocs)
	}
}

func TestPressureShapePlansPureLoopInvariantWithoutRaisingPeak(t *testing.T) {
	m := scalarModule([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, []byte{
		0x03, 0x40,
		0x20, 0x00,
		0x20, 0x01,
		0x6a,
		0x20, 0x00,
		0x73,
		0x1a,
		0x20, 0x01,
		0x0d, 0x00,
		0x0b,
		0x41, 0x00,
		0x0b,
	})
	f, cfg, flow, semantic, metadata, simplified := buildSimplifyTest(t, m)
	plan, err := PressureShape(f, cfg, flow, semantic, metadata, simplified, nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, move := range plan.LICM {
		found = found || semantic.Insts[move.Instruction].Op == wasm.InstrI32Add
	}
	if !found {
		t.Fatalf("LICM moves = %#v", plan.LICM)
	}
}

func TestPressureShapeDoesNotHoistLoopParameterUse(t *testing.T) {
	m := scalarModule([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, []byte{
		0x41, 0x00,
		0x21, 0x01,
		0x03, 0x40,
		0x20, 0x01,
		0x41, 0x04,
		0x6a,
		0x1a,
		0x20, 0x01,
		0x41, 0x01,
		0x6a,
		0x22, 0x01,
		0x20, 0x00,
		0x49,
		0x0d, 0x00,
		0x0b,
		0x41, 0x00,
		0x0b,
	})
	f, cfg, flow, semantic, metadata, simplified := buildSimplifyTest(t, m)
	plan, err := PressureShape(f, cfg, flow, semantic, metadata, simplified, nil)
	if err != nil {
		t.Fatal(err)
	}
	loopParameterUse := ^uint32(0)
	for instructionID, instruction := range semantic.Insts {
		if instruction.Op != wasm.InstrI32Add {
			continue
		}
		for _, operand := range semantic.Operands(uint32(instructionID)) {
			operand = resolveAlias(simplified.Aliases, operand)
			value := flow.Values[operand]
			if value.Kind == FlowValueBlockParam && cfg.Blocks[value.Block].Flags&BlockLoopHeader != 0 {
				loopParameterUse = uint32(instructionID)
			}
		}
	}
	if loopParameterUse == ^uint32(0) {
		t.Fatal("fixture produced no addition using a loop parameter")
	}
	for _, move := range plan.LICM {
		if move.Instruction == loopParameterUse {
			t.Fatalf("LICM move %#v hoists a loop-parameter use", move)
		}
	}
}

func TestPressureShapeSeparatesRematerializableColdUse(t *testing.T) {
	typeSec := wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})))
	funcSec := wasmtest.Section(3, wasmtest.Vec([]byte{0}))
	body := []byte{
		0x41, 0x07, 0x21, 0x01,
		0x02, 0x40,
		0x03, 0x40,
		0x20, 0x01, 0x41, 0x01, 0x6a, 0x1a,
		0x20, 0x00, 0x0d, 0x00,
		0x0b, 0x0b,
		0x20, 0x01, 0x41, 0x02, 0x6a,
		0x0b,
	}
	function := append([]byte{0x01, 0x01, 0x7f}, body...)
	code := append(wasmtest.ULEB(uint32(len(function))), function...)
	m, err := wasm.DecodeModule(wasmtest.Module(typeSec, funcSec, wasmtest.Section(10, wasmtest.Vec(code))))
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	f, cfg, flow, semantic, metadata, simplified := buildSimplifyTest(t, m)
	plan, err := PressureShape(f, cfg, flow, semantic, metadata, simplified, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.ColdUses) == 0 {
		t.Fatalf("cold uses = %#v; block weights = %#v", plan.ColdUses, cfg.Blocks)
	}
}
