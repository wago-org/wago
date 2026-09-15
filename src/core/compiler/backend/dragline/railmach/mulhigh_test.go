package railmach

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

type mulHighFixture struct {
	f          *Func
	shift      uint32
	sharedHigh VReg
	root       uint32
}

func newMulHighFixture(t *testing.T) mulHighFixture {
	t.Helper()
	f := &Func{
		Target: TargetARM64,
		VRegs: []VRegData{
			{},
			{Type: TypeI64, Bank: BankGPR, Flags: VRegInitial},
			{Type: TypeI64, Bank: BankGPR, Flags: VRegInitial},
		},
	}
	appendInst := func(op MOpcode, aux uint64, args ...VReg) VReg {
		id := uint32(len(f.Insts))
		result := VReg(len(f.VRegs))
		f.VRegs = append(f.VRegs, VRegData{Def: id*6 + 3, Type: TypeI64, Bank: BankGPR})
		if op == wasm.InstrI64Const {
			f.VRegs[result].Flags |= VRegRematerializable
		}
		instruction := Inst{Op: op, Aux: aux, Result: result, Source: id + 1, OperandStart: uint32(len(f.Operands)), OperandCount: uint16(len(args))}
		for _, arg := range args {
			f.Operands = append(f.Operands, Operand{Reg: arg, Fixed: NoFixedReg, Bank: BankGPR, Flags: OperandUse})
		}
		f.Insts = append(f.Insts, instruction)
		return result
	}
	constant := func(value uint64) VReg { return appendInst(wasm.InstrI64Const, value) }
	add := func(a, b VReg) VReg { return appendInst(wasm.InstrI64Add, 0, a, b) }
	mul := func(a, b VReg) VReg { return appendInst(wasm.InstrI64Mul, 0, a, b) }
	and := func(a, b VReg) VReg { return appendInst(wasm.InstrI64And, 0, a, b) }
	shr := func(a, b VReg) VReg { return appendInst(wasm.InstrI64ShrU, 0, a, b) }

	x, y := VReg(1), VReg(2)
	shift := uint32(len(f.Insts))
	c32 := constant(32)
	mask := constant(0xffffffff)
	xHigh := shr(x, c32)
	xLow := and(x, mask)
	yHigh := shr(y, c32)
	yLow := and(y, mask)
	lowProduct := mul(xLow, yLow)
	carryBase := add(mul(xHigh, yLow), shr(lowProduct, c32))
	upper := add(mul(yHigh, xHigh), shr(carryBase, c32))
	lowerCarry := shr(add(mul(xLow, yHigh), and(carryBase, mask)), c32)
	root := uint32(len(f.Insts))
	result := add(upper, lowerCarry)
	f.Blocks = []Block{{InstCount: uint32(len(f.Insts)), Weight: 4}}
	f.Results = []VReg{result}
	if err := Verify(f); err != nil {
		t.Fatal(err)
	}
	return mulHighFixture{f: f, shift: shift, sharedHigh: xHigh, root: root}
}

func TestSelectARM64MulHighIdiomsContractsCanonicalDAG(t *testing.T) {
	fixture := newMulHighFixture(t)
	selected, err := SelectARM64MulHighIdioms(fixture.f)
	if err != nil {
		t.Fatal(err)
	}
	if selected != 1 || fixture.f.Insts[fixture.root].Op != OpARM64I64MulHighU {
		t.Fatalf("selected=%d root=%#v", selected, fixture.f.Insts[fixture.root])
	}
	operands := fixture.f.InstructionOperands(fixture.root)
	if len(operands) != 2 || operands[0].Reg != 1 || operands[1].Reg != 2 {
		t.Fatalf("mul-high operands = %#v", operands)
	}
	for id, instruction := range fixture.f.Insts {
		if uint32(id) == fixture.root {
			continue
		}
		if instruction.Result != 0 && fixture.f.VRegs[instruction.Result].Flags&VRegElided == 0 {
			t.Fatalf("member instruction %d was not elided: %#v", id, instruction)
		}
		if instruction.OperandCount != 0 {
			t.Fatalf("member instruction %d retained operands: %#v", id, fixture.f.InstructionOperands(uint32(id)))
		}
	}
	if err := Verify(fixture.f); err != nil {
		t.Fatal(err)
	}
}

func TestSelectARM64MulHighIdiomsRejectsNearMissAndEscapingIntermediate(t *testing.T) {
	nearMiss := newMulHighFixture(t)
	nearMiss.f.Insts[nearMiss.shift].Aux = 31
	if selected, err := SelectARM64MulHighIdioms(nearMiss.f); err != nil || selected != 0 {
		t.Fatalf("near miss selected=%d err=%v", selected, err)
	}

	escape := newMulHighFixture(t)
	rootResult := escape.f.Insts[escape.root].Result
	id := uint32(len(escape.f.Insts))
	result := VReg(len(escape.f.VRegs))
	escape.f.VRegs = append(escape.f.VRegs, VRegData{Def: id*6 + 3, Type: TypeI64, Bank: BankGPR})
	escape.f.Insts = append(escape.f.Insts, Inst{Op: wasm.InstrI64Xor, Result: result, Source: id + 1, OperandStart: uint32(len(escape.f.Operands)), OperandCount: 2})
	escape.f.Operands = append(escape.f.Operands,
		Operand{Reg: rootResult, Fixed: NoFixedReg, Bank: BankGPR, Flags: OperandUse},
		Operand{Reg: escape.sharedHigh, Fixed: NoFixedReg, Bank: BankGPR, Flags: OperandUse},
	)
	escape.f.Blocks[0].InstCount++
	escape.f.Results[0] = result
	if selected, err := SelectARM64MulHighIdioms(escape.f); err != nil || selected != 0 {
		t.Fatalf("escaping intermediate selected=%d err=%v", selected, err)
	}
}
