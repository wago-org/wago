package railmach

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railspec"
	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railssa"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func buildSelectionTest(t *testing.T, target Target, m *wasm.Module) (*railssa.SemanticFunc, *SelectionPlan) {
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
	plan, err := SelectOrder(target, flow, semantic, simplified, nil)
	if err != nil {
		t.Fatal(err)
	}
	return semantic, plan
}

func TestSelectOrderChoosesTargetImmediates(t *testing.T) {
	m := machineModule([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}, []byte{
		0x20, 0x00,
		0x42, 0x07,
		0x7c,
		0x0b,
	})
	semantic, amd := buildSelectionTest(t, TargetAMD64, m)
	add := semantic.InstructionMap[2] - 1
	if amd.Selections[add].Rule != railspec.RuleAMD64Imm32 || amd.OperandForms(add)[1] != FormImmediate {
		t.Fatalf("AMD64 selection=%#v forms=%#v", amd.Selections[add], amd.OperandForms(add))
	}
	_, arm := buildSelectionTest(t, TargetARM64, m)
	if arm.Selections[add].Rule != railspec.RuleARM64Imm12 || arm.OperandForms(add)[1] != FormImmediate {
		t.Fatalf("ARM64 selection=%#v forms=%#v", arm.Selections[add], arm.OperandForms(add))
	}
}

func TestSelectOrderCombinesCompareBranch(t *testing.T) {
	m := machineModule([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, []byte{
		0x20, 0x00,
		0x20, 0x01,
		0x46,
		0x04, 0x7f,
		0x41, 0x01,
		0x05,
		0x41, 0x02,
		0x0b,
		0x0b,
	})
	semantic, plan := buildSelectionTest(t, TargetARM64, m)
	comparison := semantic.InstructionMap[2] - 1
	if plan.Selections[comparison].Rule != railspec.RuleCompareBranchFlags || plan.Selections[comparison].ResultForm != FormFlags {
		t.Fatalf("comparison selection = %#v", plan.Selections[comparison])
	}
	found := false
	for _, combination := range plan.Combinations {
		found = found || combination.Kind == CombineCompareBranch && combination.Producer == comparison
	}
	if !found {
		t.Fatalf("combinations = %#v", plan.Combinations)
	}
}

func TestSelectOrderCombinesARM64ImmediateCompareBranch(t *testing.T) {
	m := machineModule([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, []byte{
		0x20, 0x00,
		0x41, 0xe4, 0x00,
		0x4e,
		0x04, 0x7f,
		0x41, 0x01,
		0x05,
		0x41, 0x02,
		0x0b,
		0x0b,
	})
	semantic, plan := buildSelectionTest(t, TargetARM64, m)
	comparison := semantic.InstructionMap[2] - 1
	if plan.Selections[comparison].Rule != railspec.RuleCompareBranchFlags || plan.Selections[comparison].ResultForm != FormFlags || plan.OperandForms(comparison)[1] != FormImmediate {
		t.Fatalf("immediate comparison selection = %#v forms=%#v", plan.Selections[comparison], plan.OperandForms(comparison))
	}
	immediate, branch := false, false
	for _, combination := range plan.Combinations {
		immediate = immediate || combination.Kind == CombineImmediate && combination.Consumer == comparison
		branch = branch || combination.Kind == CombineCompareBranch && combination.Producer == comparison
	}
	if !immediate || !branch {
		t.Fatalf("immediate/branch combinations = %#v", plan.Combinations)
	}
}

func TestSelectOrderCombinesEqzBranch(t *testing.T) {
	m := machineModule([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, []byte{
		0x20, 0x00,
		0x45,
		0x04, 0x7f,
		0x41, 0x01,
		0x05,
		0x41, 0x02,
		0x0b,
		0x0b,
	})
	semantic, plan := buildSelectionTest(t, TargetARM64, m)
	comparison := semantic.InstructionMap[1] - 1
	if plan.Selections[comparison].Rule != railspec.RuleCompareBranchFlags || plan.Selections[comparison].ResultForm != FormFlags {
		t.Fatalf("eqz selection = %#v", plan.Selections[comparison])
	}
}
