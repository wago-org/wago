package railmach

import (
	"fmt"
	"slices"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// SelectARM64VectorRotates contracts complementary i32x4 shifts consumed by
// v128.or into one explicit target operation before scheduling and allocation.
// The selected operation depends directly on the common vector source, so the
// two temporary vectors no longer inflate FPR pressure.
func SelectARM64VectorRotates(f *Func, selection *SelectionPlan, uses []uint32) (uint32, error) {
	if err := Verify(f); err != nil {
		return 0, err
	}
	selected, err := SelectARM64VectorRotatesVerified(f, selection, uses)
	if err != nil {
		return 0, err
	}
	if err := Verify(f); err != nil {
		return 0, err
	}
	return selected, nil
}

// SelectARM64VectorRotatesVerified consumes a machine function verified by the
// immediately preceding pipeline stage. The following selection stage verifies
// the transformed function, avoiding two whole-function verifier replays in
// production while the checked entry point remains available in isolation.
func SelectARM64VectorRotatesVerified(f *Func, selection *SelectionPlan, uses []uint32) (uint32, error) {
	if f.Target != TargetARM64 {
		return 0, nil
	}
	if selection == nil {
		return 0, fmt.Errorf("railmach: vector-rotate selection plan is nil")
	}
	if len(uses) < len(f.VRegs) {
		return 0, fmt.Errorf("railmach: vector-rotate use scratch has %d entries, want %d", len(uses), len(f.VRegs))
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
	rotateCandidates := 0
	for _, instruction := range f.Insts {
		if SemanticOpcode(instruction.Op) == wasm.InstrV128Or {
			rotateCandidates++
		}
	}
	// The combination slab is already function-owned. Grow it at most once so a
	// dense vector function does not repeatedly copy its complete dependency
	// stream while folds are appended. The selected unary form reuses the first
	// of v128.or's two existing operand slots.
	selection.Combinations = slices.Grow(selection.Combinations, rotateCandidates*2)

	definition := func(value VReg, op MOpcode) (uint32, []Operand, bool) {
		if value == 0 || int(value) >= len(f.VRegs) || f.VRegs[value].Def%6 != 3 {
			return 0, nil, false
		}
		instructionID := f.VRegs[value].Def / 6
		if int(instructionID) >= len(f.Insts) || f.Insts[instructionID].Result != value || SemanticOpcode(f.Insts[instructionID].Op) != op {
			return 0, nil, false
		}
		return instructionID, f.InstructionOperands(instructionID), true
	}
	constant := func(value VReg) (uint32, bool) {
		instructionID, _, ok := definition(value, wasm.InstrI32Const)
		if !ok {
			return 0, false
		}
		return uint32(f.Insts[instructionID].Aux), true
	}

	var selected uint32
	for finalID := range f.Insts {
		final := &f.Insts[finalID]
		if SemanticOpcode(final.Op) != wasm.InstrV128Or || final.Result == 0 || f.VRegs[final.Result].Type != TypeV128 {
			continue
		}
		finalOperands := f.InstructionOperands(uint32(finalID))
		if len(finalOperands) != 2 {
			continue
		}
		for rightSide := range 2 {
			rightValue, leftValue := finalOperands[rightSide].Reg, finalOperands[1-rightSide].Reg
			rightID, rightOperands, rightOK := definition(rightValue, wasm.InstrI32x4ShrU)
			leftID, leftOperands, leftOK := definition(leftValue, wasm.InstrI32x4Shl)
			if !rightOK || !leftOK || len(rightOperands) != 2 || len(leftOperands) != 2 || uses[rightValue] != 1 || uses[leftValue] != 1 || rightOperands[0].Reg != leftOperands[0].Reg {
				continue
			}
			right, rightConstant := constant(rightOperands[1].Reg)
			left, leftConstant := constant(leftOperands[1].Reg)
			right &= 31
			left &= 31
			if !rightConstant || !leftConstant || right == 0 || left != 32-right {
				continue
			}
			source := rightOperands[0].Reg
			if source == 0 || int(source) >= len(f.VRegs) || f.VRegs[source].Type != TypeV128 {
				continue
			}
			finalOperands[0] = Operand{Reg: source, Fixed: NoFixedReg, Bank: BankFPR, Flags: OperandUse}
			final.OperandCount, final.Op, final.Aux = 1, OpARM64I32x4RotrImmediate, uint64(right)
			selection.Combinations = append(selection.Combinations,
				Combination{Producer: rightID, Consumer: uint32(finalID), Kind: CombineVectorRotate},
				Combination{Producer: leftID, Consumer: uint32(finalID), Kind: CombineVectorRotate},
			)
			f.VRegs[rightValue].Flags |= VRegElided
			f.VRegs[leftValue].Flags |= VRegElided
			selected++
			break
		}
	}
	if selected != 0 {
		slices.SortStableFunc(selection.Combinations, func(lhs, rhs Combination) int {
			if lhs.Consumer < rhs.Consumer {
				return -1
			}
			if lhs.Consumer > rhs.Consumer {
				return 1
			}
			return 0
		})
	}
	return selected, nil
}
