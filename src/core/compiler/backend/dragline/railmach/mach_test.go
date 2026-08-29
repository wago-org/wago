package railmach

import (
	"strings"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railssa"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func machineModule(params, results []wasm.ValType, body []byte) *wasm.Module {
	typeSec := wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, results)))
	funcSec := wasmtest.Section(3, wasmtest.Vec([]byte{0}))
	codeSec := wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body)))
	m, err := wasm.DecodeModule(wasmtest.Module(typeSec, funcSec, codeSec))
	if err != nil {
		panic(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		panic(err)
	}
	return m
}

func buildMachineTest(t *testing.T, target Target, m *wasm.Module) *Func {
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
	machine, err := Build(target, cfg, flow, semantic, nil)
	if err != nil {
		t.Fatal(err)
	}
	return machine
}

func TestDenseRecordSizes(t *testing.T) {
	if got := unsafe.Sizeof(Inst{}); got != 24 {
		t.Fatalf("Inst size = %d, want 24", got)
	}
	if got := unsafe.Sizeof(Operand{}); got != 12 {
		t.Fatalf("Operand size = %d, want 12", got)
	}
}

func TestBuildPreservesBlockArgumentsAndSourceOrder(t *testing.T) {
	m := machineModule([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, []byte{
		0x20, 0x00,
		0x04, 0x7f,
		0x41, 0x01,
		0x05,
		0x41, 0x02,
		0x0b,
		0x0b,
	})
	f := buildMachineTest(t, TargetARM64, m)
	if len(f.Transfers) != 2 {
		t.Fatalf("transfers = %#v", f.Transfers)
	}
	if err := ScheduleSourceStable(f); err != nil {
		t.Fatal(err)
	}
	dump := Dump(f)
	if !strings.Contains(dump, "target arm64") || !strings.Contains(dump, "edge") || !strings.Contains(dump, "I32Const") {
		t.Fatalf("dump:\n%s", dump)
	}
}

func TestBuildRetainsCrossBlockSimplificationDefinitions(t *testing.T) {
	m := machineModule([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, []byte{
		0x20, 0x00, 0x41, 0x07, 0x6a, 0x1a,
		0x20, 0x00,
		0x04, 0x7f,
		0x20, 0x00, 0x41, 0x07, 0x6a,
		0x05,
		0x41, 0x00,
		0x0b,
		0x0b,
	})
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
	machine, err := BuildWithSimplify(TargetARM64, cfg, flow, semantic, simplified, nil)
	if err != nil {
		t.Fatal(err)
	}
	var crossBlock int
	for value, alias := range simplified.Aliases {
		canonical := resolveMachineAlias(simplified.Aliases, alias)
		if value == 0 || canonical == railssa.FlowValueID(value) || flow.Values[value].Kind != railssa.FlowValueInstruction || flow.Values[canonical].Kind != railssa.FlowValueInstruction || flow.Values[value].Block == flow.Values[canonical].Block {
			continue
		}
		crossBlock++
		if machine.VRegs[value].Flags&VRegElided != 0 {
			t.Fatalf("cross-block alias v%d -> v%d was elided", value, canonical)
		}
	}
	if crossBlock == 0 {
		t.Fatal("fixture produced no cross-block instruction alias")
	}
}

func TestBuildElidesVerifiedTrivialBlockParameters(t *testing.T) {
	m := machineModule([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, []byte{
		0x02, 0x40,
		0x03, 0x40,
		0x20, 0x00,
		0x21, 0x00,
		0x20, 0x00,
		0x0d, 0x01,
		0x0c, 0x00,
		0x0b,
		0x0b,
		0x20, 0x00,
		0x0b,
	})
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
	machine, err := BuildWithSimplify(TargetARM64, cfg, flow, semantic, simplified, nil)
	if err != nil {
		t.Fatal(err)
	}
	elided := 0
	for value, record := range flow.Values {
		if record.Kind != railssa.FlowValueBlockParam || resolveMachineAlias(simplified.Aliases, railssa.FlowValueID(value)) == railssa.FlowValueID(value) {
			continue
		}
		elided++
		if machine.VRegs[value].Flags&VRegElided == 0 {
			t.Fatalf("trivial block parameter v%d was retained", value)
		}
		for _, transfer := range machine.Transfers {
			if transfer.Dst == VReg(value) {
				t.Fatalf("trivial block parameter v%d retains transfer %#v", value, transfer)
			}
		}
	}
	if elided == 0 {
		t.Fatal("fixture produced no trivial block parameter")
	}
}

func TestAMD64ShiftCountIsFixed(t *testing.T) {
	m := machineModule([]wasm.ValType{wasm.I64, wasm.I64}, []wasm.ValType{wasm.I64}, []byte{
		0x20, 0x00,
		0x20, 0x01,
		0x86,
		0x0b,
	})
	amd := buildMachineTest(t, TargetAMD64, m)
	if len(amd.Insts) != 1 {
		t.Fatalf("instructions = %#v", amd.Insts)
	}
	operands := amd.InstructionOperands(0)
	if len(operands) != 2 || operands[1].Flags&OperandFixed == 0 || operands[1].Fixed != 1 {
		t.Fatalf("AMD64 shift operands = %#v", operands)
	}
	arm := buildMachineTest(t, TargetARM64, m)
	if arm.InstructionOperands(0)[1].Flags&OperandFixed != 0 {
		t.Fatalf("ARM64 shift count unexpectedly fixed: %#v", arm.InstructionOperands(0)[1])
	}
}

func TestColdConstantUseDoesNotExtendAllocatedInterval(t *testing.T) {
	m := machineModule([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, []byte{
		0x41, 0x07,
		0x20, 0x00,
		0x6a,
		0x0b,
	})
	f := buildMachineTest(t, TargetARM64, m)
	if len(f.Insts) != 2 || f.Insts[0].Op != wasm.InstrI32Const || f.Insts[1].Op != wasm.InstrI32Add {
		t.Fatalf("instructions = %#v", f.Insts)
	}
	value := f.Insts[0].Result
	pressure := &railssa.PressurePlan{
		Remats:   []railssa.RematRecipe{{Value: railssa.FlowValueID(value), Aux: 7, Kind: railssa.RematConstant}},
		ColdUses: []railssa.ColdUse{{Value: railssa.FlowValueID(value), Instruction: 1, HotWeight: 8, ColdWeight: 1}},
	}
	committed, err := ApplyColdRematerialization(f, pressure, nil)
	if err != nil {
		t.Fatal(err)
	}
	if committed != 1 || f.InstructionOperands(1)[0].Flags&OperandColdRemat == 0 {
		t.Fatalf("committed=%d operands=%#v", committed, f.InstructionOperands(1))
	}
	allocation, err := AllocateLinearQ(f, LinearQConfig{GPRs: 2, FPRs: 1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, interval := range allocation.Intervals {
		if interval.Reg == value {
			t.Fatalf("cold-only constant retained interval %#v", interval)
		}
	}
}

func TestColdExtensionAndAffineUsesCommitWhenTargetLegal(t *testing.T) {
	extendModule := machineModule([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I64}, []byte{
		0x20, 0x00,
		0xac,
		0x42, 0x03,
		0x7c,
		0x0b,
	})
	extend := buildMachineTest(t, TargetARM64, extendModule)
	extended := extend.Insts[0].Result
	extendPlan := &railssa.PressurePlan{
		Remats:   []railssa.RematRecipe{{Value: railssa.FlowValueID(extended), Base: railssa.FlowValueID(extend.InstructionOperands(0)[0].Reg), Kind: railssa.RematExtend}},
		ColdUses: []railssa.ColdUse{{Value: railssa.FlowValueID(extended), Instruction: 2, HotWeight: 8, ColdWeight: 1}},
	}
	if committed, err := ApplyColdRematerialization(extend, extendPlan, nil); err != nil || committed != 1 || extend.InstructionOperands(2)[0].Flags&OperandColdRemat == 0 {
		t.Fatalf("extension committed=%d err=%v operands=%#v", committed, err, extend.InstructionOperands(2))
	}

	affineModule := machineModule([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}, []byte{
		0x20, 0x00,
		0x42, 0x03,
		0x7c,
		0x42, 0x02,
		0x7e,
		0x0b,
	})
	affine := buildMachineTest(t, TargetARM64, affineModule)
	affineValue := affine.Insts[1].Result
	affinePlan := &railssa.PressurePlan{
		Remats:   []railssa.RematRecipe{{Value: railssa.FlowValueID(affineValue), Base: railssa.FlowValueID(affine.InstructionOperands(1)[0].Reg), Aux: 3, Kind: railssa.RematAffine}},
		ColdUses: []railssa.ColdUse{{Value: railssa.FlowValueID(affineValue), Instruction: 3, HotWeight: 8, ColdWeight: 1}},
	}
	priced := &RematPlan{Decisions: []RematDecision{{Value: affineValue, Base: affine.InstructionOperands(1)[0].Reg, RecipeCost: 2, SpillCost: 20, Profitable: true}}}
	if committed, err := ApplyColdRematerialization(affine, affinePlan, priced); err != nil || committed != 1 || affine.InstructionOperands(3)[0].Flags&OperandColdRemat == 0 {
		t.Fatalf("affine committed=%d err=%v operands=%#v", committed, err, affine.InstructionOperands(3))
	}
}
