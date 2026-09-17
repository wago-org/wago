package railssa

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
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

func TestPressureShapePlansConstantFromLoopContinuation(t *testing.T) {
	m := scalarModule([]wasm.ValType{wasm.I32, wasm.I64}, []wasm.ValType{wasm.I64}, []byte{
		0x02, 0x40,
		0x03, 0x40,
		0x20, 0x00,
		0x45,
		0x0d, 0x01,
		0x20, 0x01,
		0x42, 0x80, 0x80, 0x04,
		0x7e,
		0x21, 0x01,
		0x20, 0x00,
		0x41, 0x01,
		0x6b,
		0x21, 0x00,
		0x0c, 0x00,
		0x0b,
		0x0b,
		0x20, 0x01,
		0x0b,
	})
	f, cfg, flow, semantic, metadata, simplified := buildSimplifyTest(t, m)
	plan, err := PressureShape(f, cfg, flow, semantic, metadata, simplified, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, move := range plan.LICM {
		if semantic.Insts[move.Instruction].Op == wasm.InstrI64Const && move.From != move.Loop {
			return
		}
	}
	t.Fatalf("LICM moves = %#v", plan.LICM)
}

func TestPressureShapePlansV128ConstantAsFPRLoopInvariant(t *testing.T) {
	body := []byte{
		0x03, 0x40,
		0x20, 0x01, // local.get 1
		0x04, 0x40, // if
		0x20, 0x00, // local.get 0
		0xfd, 0x0c,
	}
	body = append(body, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16)
	body = append(body,
		0xfd, 0x4e, // v128.and
		0xfd, 0x53, // v128.any_true
		0x0d, 0x01, // br_if 1
		0x0b,
		0x20, 0x01, // local.get 1
		0x0d, 0x00, // br_if 0
		0x0b,
		0x41, 0x00,
		0x0b,
	)
	m := scalarModule([]wasm.ValType{wasm.V128, wasm.I32}, []wasm.ValType{wasm.I32}, body)
	f, cfg, flow, semantic, metadata, simplified := buildSimplifyTest(t, m)
	plan, err := PressureShape(f, cfg, flow, semantic, metadata, simplified, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, move := range plan.LICM {
		instruction := semantic.Insts[move.Instruction]
		if instruction.Op == wasm.InstrV128Const {
			if plan.Blocks[move.From].PeakFPR == 0 || flow.Values[instruction.Result].Type != wasm.V128 {
				t.Fatalf("v128 LICM pressure = %#v, type = %v", plan.Blocks[move.From], flow.Values[instruction.Result].Type)
			}
			return
		}
	}
	t.Fatalf("LICM moves = %#v", plan.LICM)
}

func TestLICMNestedRegionAdmissionIsLimitedToVectorConstants(t *testing.T) {
	f := &StackFunc{Regions: []Region{
		{Parent: NoRegion, Kind: wasm.InstrLoop},
		{Parent: 0, Kind: wasm.InstrIf},
		{Parent: 1, Kind: wasm.InstrLoop},
	}}
	if !licmSourceRegionAllowed(1, 0, wasm.InstrV128Const, f) {
		t.Fatal("vector constant in a nested conditional was rejected")
	}
	if licmSourceRegionAllowed(1, 0, wasm.InstrI64Const, f) {
		t.Fatal("scalar work in a nested conditional was speculated")
	}
	if licmSourceRegionAllowed(2, 0, wasm.InstrV128Const, f) {
		t.Fatal("vector constant escaped its innermost loop")
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

func TestRetainAggregateColdUses(t *testing.T) {
	uses := []ColdUse{
		{Value: 2, Instruction: 7, HotWeight: 64, ColdWeight: 16},
		{Value: 1, Instruction: 9, HotWeight: 64, ColdWeight: 8},
		{Value: 2, Instruction: 3, HotWeight: 64, ColdWeight: 16},
		{Value: 1, Instruction: 5, HotWeight: 64, ColdWeight: 4},
	}
	got := retainAggregateColdUses(uses)
	if len(got) != 2 || got[0].Value != 1 || got[0].Instruction != 5 || got[1].Value != 1 || got[1].Instruction != 9 {
		t.Fatalf("retained cold uses = %#v", got)
	}
}
