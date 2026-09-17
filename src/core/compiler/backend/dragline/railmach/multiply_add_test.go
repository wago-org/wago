package railmach

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func newMultiplyAddFixture(t *testing.T, typ MachineType) (*Func, uint32, uint32) {
	t.Helper()
	mulOp, addOp := MOpcode(wasm.InstrI32Mul), MOpcode(wasm.InstrI32Add)
	if typ == TypeI64 {
		mulOp, addOp = wasm.InstrI64Mul, wasm.InstrI64Add
	}
	f := &Func{Target: TargetARM64, VRegs: []VRegData{{}}}
	for range 3 {
		f.VRegs = append(f.VRegs, VRegData{Type: typ, Bank: BankGPR, Flags: VRegInitial})
	}
	appendBinary := func(op MOpcode, lhs, rhs VReg) VReg {
		id := uint32(len(f.Insts))
		result := VReg(len(f.VRegs))
		f.VRegs = append(f.VRegs, VRegData{Def: id*6 + 3, Type: typ, Bank: BankGPR})
		f.Insts = append(f.Insts, Inst{Op: op, Result: result, Source: id + 1, OperandStart: uint32(len(f.Operands)), OperandCount: 2})
		f.Operands = append(f.Operands,
			Operand{Reg: lhs, Fixed: NoFixedReg, Bank: BankGPR, Flags: OperandUse},
			Operand{Reg: rhs, Fixed: NoFixedReg, Bank: BankGPR, Flags: OperandUse},
		)
		return result
	}
	product := appendBinary(mulOp, 1, 2)
	root := uint32(len(f.Insts))
	result := appendBinary(addOp, 3, product) // exercise commuted multiply input.
	f.Blocks = []Block{{InstCount: uint32(len(f.Insts)), Weight: 4}}
	f.Results = []VReg{result}
	if err := Verify(f); err != nil {
		t.Fatal(err)
	}
	return f, 0, root
}

func TestSelectARM64MultiplyAddsContractsPrivateProduct(t *testing.T) {
	for _, test := range []struct {
		typ  MachineType
		want MOpcode
	}{{TypeI32, OpARM64I32Madd}, {TypeI64, OpARM64I64Madd}} {
		f, product, root := newMultiplyAddFixture(t, test.typ)
		selected, err := SelectARM64MultiplyAdds(f, make([]uint32, len(f.VRegs)))
		if err != nil {
			t.Fatal(err)
		}
		if selected != 1 || f.Insts[root].Op != test.want {
			t.Fatalf("type %d selected=%d root=%#v", test.typ, selected, f.Insts[root])
		}
		operands := f.InstructionOperands(root)
		if len(operands) != 3 || operands[0].Reg != 1 || operands[1].Reg != 2 || operands[2].Reg != 3 {
			t.Fatalf("type %d multiply-add operands = %#v", test.typ, operands)
		}
		productResult := f.Insts[product].Result
		if f.Insts[product].OperandCount != 0 || f.VRegs[productResult].Flags&VRegElided == 0 {
			t.Fatalf("type %d product was not detached: inst=%#v value=%#v", test.typ, f.Insts[product], f.VRegs[productResult])
		}
		if err := Verify(f); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSelectARM64MultiplyAddsContractsRightSubtrahend(t *testing.T) {
	for _, test := range []struct {
		typ  MachineType
		want MOpcode
	}{{TypeI32, OpARM64I32Msub}, {TypeI64, OpARM64I64Msub}} {
		f, product, root := newMultiplyAddFixture(t, test.typ)
		if test.typ == TypeI64 {
			f.Insts[root].Op = wasm.InstrI64Sub
		} else {
			f.Insts[root].Op = wasm.InstrI32Sub
		}
		selected, err := SelectARM64MultiplyAdds(f, make([]uint32, len(f.VRegs)))
		if err != nil {
			t.Fatal(err)
		}
		if selected != 1 || f.Insts[root].Op != test.want {
			t.Fatalf("type %d selected=%d root=%#v", test.typ, selected, f.Insts[root])
		}
		operands := f.InstructionOperands(root)
		if len(operands) != 3 || operands[0].Reg != 1 || operands[1].Reg != 2 || operands[2].Reg != 3 {
			t.Fatalf("type %d multiply-subtract operands = %#v", test.typ, operands)
		}
		productResult := f.Insts[product].Result
		if f.Insts[product].OperandCount != 0 || f.VRegs[productResult].Flags&VRegElided == 0 {
			t.Fatalf("type %d product was not detached: inst=%#v value=%#v", test.typ, f.Insts[product], f.VRegs[productResult])
		}
		if err := Verify(f); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSelectARM64MultiplyAddsRejectsLeftSubtrahend(t *testing.T) {
	f, product, root := newMultiplyAddFixture(t, TypeI64)
	f.Insts[root].Op = wasm.InstrI64Sub
	operands := f.InstructionOperands(root)
	operands[0], operands[1] = operands[1], operands[0]
	selected, err := SelectARM64MultiplyAdds(f, make([]uint32, len(f.VRegs)))
	if err != nil || selected != 0 || f.Insts[product].OperandCount != 2 {
		t.Fatalf("left product selected=%d err=%v product=%#v", selected, err, f.Insts[product])
	}
}

func TestSelectARM64MultiplyAddsRejectsSharedProduct(t *testing.T) {
	f, product, _ := newMultiplyAddFixture(t, TypeI64)
	productResult := f.Insts[product].Result
	id := uint32(len(f.Insts))
	result := VReg(len(f.VRegs))
	f.VRegs = append(f.VRegs, VRegData{Def: id*6 + 3, Type: TypeI64, Bank: BankGPR})
	f.Insts = append(f.Insts, Inst{Op: wasm.InstrI64Xor, Result: result, Source: id + 1, OperandStart: uint32(len(f.Operands)), OperandCount: 2})
	f.Operands = append(f.Operands,
		Operand{Reg: productResult, Fixed: NoFixedReg, Bank: BankGPR, Flags: OperandUse},
		Operand{Reg: 1, Fixed: NoFixedReg, Bank: BankGPR, Flags: OperandUse},
	)
	f.Blocks[0].InstCount++
	f.Results[0] = result
	selected, err := SelectARM64MultiplyAdds(f, make([]uint32, len(f.VRegs)))
	if err != nil || selected != 0 {
		t.Fatalf("shared product selected=%d err=%v", selected, err)
	}
}
