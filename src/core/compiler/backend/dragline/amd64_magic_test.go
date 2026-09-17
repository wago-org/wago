package dragline

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railmach"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestAMD64UnsignedI32ImmediateMagic(t *testing.T) {
	for _, divisor := range []uint32{9, 13, 25, 67, 100, 125, 1_000_000, 100_000_000} {
		multiplier, shift, ok := amd64UnsignedI32ImmediateMagic(divisor)
		if !ok {
			t.Fatalf("divisor %d has no immediate magic", divisor)
		}
		values := []uint32{0, 1, divisor - 1, divisor, divisor + 1, ^uint32(0) - 1, ^uint32(0)}
		state := uint32(0x9e3779b9) ^ divisor
		for range 100_000 {
			state ^= state << 13
			state ^= state >> 17
			state ^= state << 5
			values = append(values, state)
		}
		for _, dividend := range values {
			got := uint32(uint64(dividend) * uint64(multiplier) >> shift)
			if want := dividend / divisor; got != want {
				t.Fatalf("%d / %d via %#x >> %d = %d, want %d", dividend, divisor, multiplier, shift, got, want)
			}
		}
	}
	for _, divisor := range []uint32{0, 1, 3, 7, 20, 10_000, ^uint32(0)} {
		if _, _, ok := amd64UnsignedI32ImmediateMagic(divisor); ok {
			t.Fatalf("divisor %d unexpectedly has positive immediate magic", divisor)
		}
	}
}

func TestRefineAMD64ConstantDivisionConstraints(t *testing.T) {
	for _, test := range []struct {
		divisor uint32
		kind    wasm.InstrKind
		want    bool
	}{
		{100, wasm.InstrI32DivU, true},
		{16, wasm.InstrI32DivU, false},
		{16, wasm.InstrI32RemU, false},
		{20, wasm.InstrI32DivU, false},
		{100, wasm.InstrI32RemU, true},
	} {
		machine := railmach.Func{
			Target: railmach.TargetAMD64,
			Insts: []railmach.Inst{
				{Op: wasm.InstrI32Const, Aux: uint64(test.divisor), Result: 2},
				{Op: railmach.MOpcode(test.kind), OperandStart: 0, OperandCount: 2, Result: 3},
			},
			Operands: []railmach.Operand{
				{Reg: 1, Fixed: 0, Bank: railmach.BankGPR, Flags: railmach.OperandUse | railmach.OperandFixed},
				{Reg: 2, Fixed: railmach.NoFixedReg, Bank: railmach.BankGPR, Flags: railmach.OperandUse},
			},
			VRegs: make([]railmach.VRegData, 4),
		}
		machine.VRegs[2] = railmach.VRegData{Def: 3, Type: railmach.TypeI32, Bank: railmach.BankGPR}
		refineAMD64ConstantDivisionConstraints(&machine, test.kind == wasm.InstrI32RemU && test.want)
		got := machine.Operands[0].Flags&railmach.OperandFixed == 0 && machine.Operands[0].Fixed == railmach.NoFixedReg
		if got != test.want {
			t.Fatalf("%s by %d constraint released = %v, want %v", test.kind, test.divisor, got, test.want)
		}
	}
}

func TestNativeAMD64ImmediateRemaindersRequireRepeatedUses(t *testing.T) {
	for _, test := range []struct {
		uses int
		want bool
	}{{2, false}, {3, true}} {
		machine := railmach.Func{
			Target: railmach.TargetAMD64,
			Insts:  []railmach.Inst{{Op: wasm.InstrI32Const, Aux: 100, Result: 2}},
			VRegs:  make([]railmach.VRegData, test.uses+3),
		}
		machine.VRegs[2] = railmach.VRegData{Def: 3, Type: railmach.TypeI32, Bank: railmach.BankGPR}
		for use := range test.uses {
			machine.Insts = append(machine.Insts, railmach.Inst{
				Op: wasm.InstrI32RemU, OperandStart: uint32(len(machine.Operands)), OperandCount: 2, Result: railmach.VReg(use + 3),
			})
			machine.Operands = append(machine.Operands, railmach.Operand{Reg: 1}, railmach.Operand{Reg: 2})
		}
		if got := nativeAMD64ImmediateRemainders(&machine); got != test.want {
			t.Fatalf("%d uses admitted = %v, want %v", test.uses, got, test.want)
		}
	}
}
