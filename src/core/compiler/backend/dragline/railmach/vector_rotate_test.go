package railmach

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railssa"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func arm64VectorRotateFixture() *Func {
	return &Func{
		Target: TargetARM64,
		VRegs: []VRegData{
			{},
			{Type: TypeV128, Bank: BankFPR, Flags: VRegInitial},
			{Def: 3, Type: TypeI32, Bank: BankGPR, Flags: VRegRematerializable},
			{Def: 9, Type: TypeV128, Bank: BankFPR},
			{Def: 15, Type: TypeI32, Bank: BankGPR, Flags: VRegRematerializable},
			{Def: 21, Type: TypeV128, Bank: BankFPR},
			{Def: 27, Type: TypeV128, Bank: BankFPR},
		},
		Operands: []Operand{
			{Reg: 1, Fixed: NoFixedReg, Bank: BankFPR, Flags: OperandUse},
			{Reg: 2, Fixed: NoFixedReg, Bank: BankGPR, Flags: OperandUse},
			{Reg: 1, Fixed: NoFixedReg, Bank: BankFPR, Flags: OperandUse},
			{Reg: 4, Fixed: NoFixedReg, Bank: BankGPR, Flags: OperandUse},
			{Reg: 3, Fixed: NoFixedReg, Bank: BankFPR, Flags: OperandUse},
			{Reg: 5, Fixed: NoFixedReg, Bank: BankFPR, Flags: OperandUse},
		},
		Insts: []Inst{
			{Op: wasm.InstrI32Const, Aux: 12, Result: 2},
			{Op: wasm.InstrI32x4ShrU, OperandStart: 0, OperandCount: 2, Result: 3},
			{Op: wasm.InstrI32Const, Aux: 20, Result: 4},
			{Op: wasm.InstrI32x4Shl, OperandStart: 2, OperandCount: 2, Result: 5},
			{Op: wasm.InstrV128Or, OperandStart: 4, OperandCount: 2, Result: 6},
		},
		Blocks: []Block{{InstCount: 5}},
		SIMD: []railssa.SemanticSIMDImmediate{
			{Instruction: 1}, {Instruction: 3}, {Instruction: 4},
		},
		Results: []VReg{6},
	}
}

func TestSelectARM64VectorRotates(t *testing.T) {
	f := arm64VectorRotateFixture()
	selection := new(SelectionPlan)
	selected, err := SelectARM64VectorRotates(f, selection, make([]uint32, len(f.VRegs)))
	if err != nil {
		t.Fatal(err)
	}
	if selected != 1 || f.Insts[4].Op != OpARM64I32x4RotrImmediate || f.Insts[4].Aux != 12 {
		t.Fatalf("selected rotate = %#v, count %d", f.Insts[4], selected)
	}
	operands := f.InstructionOperands(4)
	if len(operands) != 1 || operands[0].Reg != 1 {
		t.Fatalf("selected operands = %#v, want source v1", operands)
	}
	if f.VRegs[3].Flags&VRegElided == 0 || f.VRegs[5].Flags&VRegElided == 0 {
		t.Fatalf("shift producers were not elided: insts=%#v vregs=%#v", f.Insts, f.VRegs)
	}
	if len(selection.Combinations) != 2 || selection.Combinations[0].Consumer != 4 || selection.Combinations[1].Consumer != 4 {
		t.Fatalf("rotate dependencies = %#v, want both shifts feeding instruction 4", selection.Combinations)
	}
	if got := SemanticOpcode(f.Insts[4].Op); got != wasm.InstrV128Or {
		t.Fatalf("selected semantic opcode = %s, want v128.or", got)
	}
}

func TestSelectARM64VectorRotatesRejectsNonComplementaryShifts(t *testing.T) {
	f := arm64VectorRotateFixture()
	f.Insts[2].Aux = 19
	selection := new(SelectionPlan)
	selected, err := SelectARM64VectorRotates(f, selection, make([]uint32, len(f.VRegs)))
	if err != nil {
		t.Fatal(err)
	}
	if selected != 0 || f.Insts[4].Op != wasm.InstrV128Or || f.VRegs[3].Flags&VRegElided != 0 || f.VRegs[5].Flags&VRegElided != 0 {
		t.Fatalf("non-complementary shifts were folded: inst=%#v vregs=%#v", f.Insts[4], f.VRegs)
	}
}

func TestSelectARM64VectorRotatesPreservesSharedShift(t *testing.T) {
	f := arm64VectorRotateFixture()
	f.Results = append(f.Results, 3)
	selection := new(SelectionPlan)
	selected, err := SelectARM64VectorRotates(f, selection, make([]uint32, len(f.VRegs)))
	if err != nil {
		t.Fatal(err)
	}
	if selected != 0 || f.Insts[4].Op != wasm.InstrV128Or || f.VRegs[3].Flags&VRegElided != 0 || f.VRegs[5].Flags&VRegElided != 0 {
		t.Fatalf("shared shift was folded: inst=%#v vregs=%#v", f.Insts[4], f.VRegs)
	}
}
