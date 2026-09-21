package railmach

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// SelectARM64MultiplyAdds contracts a private integer multiply consumed by an
// add or by the right-hand side of a subtraction into ARM64 MADD or MSUB. Wasm
// integer multiply, add, and subtraction all wrap at their result width,
// exactly matching the low i32/i64 result of those instructions.
//
// uses is caller-owned reusable scratch. Counting machine operands, edge
// transfers, and function results makes producer elision explicit and keeps the
// pass linear without adding function-sized allocation to the compiler path.
func SelectARM64MultiplyAdds(f *Func, uses []uint32) (uint32, error) {
	if err := Verify(f); err != nil {
		return 0, err
	}
	if f.Target != TargetARM64 {
		return 0, nil
	}
	if len(uses) < len(f.VRegs) {
		return 0, fmt.Errorf("railmach: multiply-add use scratch has %d entries, want %d", len(uses), len(f.VRegs))
	}
	uses = uses[:len(f.VRegs)]
	clear(uses)
	for instructionID := range f.Insts {
		for _, operand := range f.InstructionOperands(uint32(instructionID)) {
			uses[operand.Reg]++
		}
	}
	for _, transfer := range f.Transfers {
		uses[transfer.Src]++
	}
	for _, result := range f.Results {
		uses[result]++
	}

	var selected uint32
	for rootID := range f.Insts {
		root := &f.Insts[rootID]
		semantic := SemanticOpcode(root.Op)
		if semantic != wasm.InstrI32Add && semantic != wasm.InstrI64Add && semantic != wasm.InstrI32Sub && semantic != wasm.InstrI64Sub || root.Result == 0 {
			continue
		}
		rootOperands := f.InstructionOperands(uint32(rootID))
		if len(rootOperands) != 2 {
			continue
		}
		wantType, mulOp := TypeI32, MOpcode(wasm.InstrI32Mul)
		selectedOp := OpARM64I32Madd
		if semantic == wasm.InstrI64Add || semantic == wasm.InstrI64Sub {
			wantType, mulOp = TypeI64, MOpcode(wasm.InstrI64Mul)
			selectedOp = OpARM64I64Madd
		}
		if semantic == wasm.InstrI32Sub {
			selectedOp = OpARM64I32Msub
		} else if semantic == wasm.InstrI64Sub {
			selectedOp = OpARM64I64Msub
		}
		if int(root.Result) >= len(f.VRegs) || f.VRegs[root.Result].Type != wantType {
			continue
		}

		for multiplied := 0; multiplied < 2; multiplied++ {
			// MSUB computes addend - product. A product on the left of Wasm
			// subtraction would require a different negated form.
			if (semantic == wasm.InstrI32Sub || semantic == wasm.InstrI64Sub) && multiplied != 1 {
				continue
			}
			product := rootOperands[multiplied].Reg
			if product == 0 || int(product) >= len(f.VRegs) || uses[product] != 1 || f.VRegs[product].Type != wantType {
				continue
			}
			data := f.VRegs[product]
			if data.Flags&(VRegInitial|VRegBlockParam|VRegElided) != 0 || data.Def%6 != 3 {
				continue
			}
			producerID := data.Def / 6
			if int(producerID) >= len(f.Insts) {
				continue
			}
			producer := &f.Insts[producerID]
			if producer.Result != product || producer.ResultCount() != 1 || SemanticOpcode(producer.Op) != mulOp {
				continue
			}
			mulOperands := f.InstructionOperands(producerID)
			if len(mulOperands) != 2 {
				continue
			}

			lhs, rhs, addend := mulOperands[0].Reg, mulOperands[1].Reg, rootOperands[1-multiplied].Reg
			start := uint32(len(f.Operands))
			f.Operands = append(f.Operands,
				Operand{Reg: lhs, Fixed: NoFixedReg, Bank: BankGPR, Flags: OperandUse},
				Operand{Reg: rhs, Fixed: NoFixedReg, Bank: BankGPR, Flags: OperandUse},
				Operand{Reg: addend, Fixed: NoFixedReg, Bank: BankGPR, Flags: OperandUse},
			)
			root.OperandStart, root.OperandCount, root.Op = start, 3, selectedOp
			producer.OperandCount = 0
			f.VRegs[product].Flags |= VRegElided
			selected++
			break
		}
	}
	if err := Verify(f); err != nil {
		return 0, err
	}
	return selected, nil
}
