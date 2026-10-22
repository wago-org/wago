//go:build arm64

package arm64

import (
	"fmt"
	railcore "github.com/wago-org/wago/src/core/compiler/backend/railshot"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const (
	maxInstructionRecipeNodes = 128
	maxInstructionRecipeWork  = 256
	maxInstructionRecipeDepth = 32
)

// instructionRecipeEligible bounds the expression tree emitted from a recipe
// DAG. Shared parents can otherwise multiply a small recipe into large code.
// Prepared nodes are topological; fixed arrays keep this check allocation-free.
func instructionRecipeEligible(custom railcore.CustomInstruction) bool {
	if len(custom.Nodes) == 0 || len(custom.Nodes) > maxInstructionRecipeNodes || custom.Output < 0 || custom.Output >= len(custom.Nodes) {
		return false
	}
	var work [maxInstructionRecipeNodes]uint16
	var depth [maxInstructionRecipeNodes]uint8
	for id, node := range custom.Nodes {
		count, height := uint32(1), uint8(1)
		var children [3]int
		n := 0
		switch node.Op {
		case railcore.CustomInstructionInput, railcore.CustomInstructionConst:
		case railcore.CustomInstructionNot, railcore.CustomInstructionIsZero:
			children[0], n = node.A, 1
		case railcore.CustomInstructionSelect:
			children, n = [3]int{node.A, node.B, node.C}, 3
		default:
			if node.Op < railcore.CustomInstructionAdd || node.Op > railcore.CustomInstructionGeS {
				return false
			}
			children[0], children[1], n = node.A, node.B, 2
		}
		for _, child := range children[:n] {
			if child < 0 || child >= id {
				return false
			}
			count += uint32(work[child])
			if depth[child]+1 > height {
				height = depth[child] + 1
			}
		}
		if count > maxInstructionRecipeWork || height > maxInstructionRecipeDepth {
			return false
		}
		work[id], depth[id] = uint16(count), height
	}
	return true
}

func (f *fn) emitInstructionRecipe(custom railcore.CustomInstruction, ft *wasm.CompType) error {
	if len(ft.Results) != 1 || !wasm.EqualValType(ft.Results[0], wasm.I32) {
		return fmt.Errorf("custom instruction %d must return one i32", f.traceFuncIdx)
	}
	// Snapshot physical arguments into stable frame slots. Recipes are expression
	// DAGs, not a postfix-only macro: an input may be reused, reordered, or omitted.
	// Keeping the original argument nodes on the operand stack reserves the slots
	// until the result has been fully materialized.
	var args []*elem
	var argSlots []int
	if !custom.StackCompatible {
		roots := f.rootsBottomToTop()
		if len(roots) < len(ft.Params) {
			return fmt.Errorf("custom instruction %d has %d stack argument(s), want %d", f.traceFuncIdx, len(roots), len(ft.Params))
		}
		args = append([]*elem(nil), roots[len(roots)-len(ft.Params):]...)
		argSlots = make([]int, len(args))
		for i, arg := range args {
			f.materialize(arg)
			f.spill(arg)
			argSlots[i] = arg.st.slotIndex()
		}
	}

	maskTop := func(width int32) {
		if width >= 32 {
			return
		}
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64((uint64(1) << uint(width)) - 1)})
		f.pushBinOp(opAnd, mtI32)
	}
	signExtendTop := func(width int32) {
		if width >= 32 {
			return
		}
		shift := int64(32 - width)
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: shift})
		f.pushBinOp(opShl, mtI32)
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: shift})
		f.pushBinOp(opShrS, mtI32)
	}
	var emit func(int) error
	emit = func(id int) error {
		if id < 0 || id >= len(custom.Nodes) {
			return fmt.Errorf("custom instruction node %d out of range", id)
		}
		n := custom.Nodes[id]
		switch n.Op {
		case railcore.CustomInstructionInput:
			if custom.StackCompatible {
				return nil // physical parameters already occupy the operand stack
			}
			if n.Input < 0 || n.Input >= len(argSlots) {
				return fmt.Errorf("custom instruction input %d out of range", n.Input)
			}
			f.pushValue(storage{kind: stSlot, typ: mtI32, slot: uint32(argSlots[n.Input])})
			maskTop(n.Width)
			return nil
		case railcore.CustomInstructionConst:
			f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(n.Const)})
			return nil
		case railcore.CustomInstructionNot:
			if err := emit(n.A); err != nil {
				return err
			}
			f.pushValue(storage{kind: stConst, typ: mtI32, cval: -1})
			f.pushBinOp(opXor, mtI32)
			maskTop(n.Width)
		case railcore.CustomInstructionIsZero:
			if err := emit(n.A); err != nil {
				return err
			}
			f.pushUnOp(opEqz, mtI32)
		case railcore.CustomInstructionSelect:
			if err := emit(n.A); err != nil {
				return err
			}
			if err := emit(n.B); err != nil {
				return err
			}
			if err := emit(n.C); err != nil {
				return err
			}
			f.emitSelect()
			maskTop(n.Width)
		default:
			if err := emit(n.A); err != nil {
				return err
			}
			operandWidth := custom.Nodes[n.A].Width
			signed := n.Op == railcore.CustomInstructionShrS || n.Op == railcore.CustomInstructionLtS || n.Op == railcore.CustomInstructionLeS || n.Op == railcore.CustomInstructionGtS || n.Op == railcore.CustomInstructionGeS
			if signed {
				signExtendTop(operandWidth)
			}
			if err := emit(n.B); err != nil {
				return err
			}
			if signed && n.Op != railcore.CustomInstructionShrS {
				signExtendTop(operandWidth)
			}
			if n.Op == railcore.CustomInstructionShl || n.Op == railcore.CustomInstructionShrU || n.Op == railcore.CustomInstructionShrS {
				if operandWidth < 32 {
					f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(operandWidth)})
					f.pushBinOp(opRemU, mtI32)
				}
			}
			var op wOp
			switch n.Op {
			case railcore.CustomInstructionAdd:
				op = opAdd
			case railcore.CustomInstructionSub:
				op = opSub
			case railcore.CustomInstructionMul:
				op = opMul
			case railcore.CustomInstructionAnd:
				op = opAnd
			case railcore.CustomInstructionOr:
				op = opOr
			case railcore.CustomInstructionXor:
				op = opXor
			case railcore.CustomInstructionShl:
				op = opShl
			case railcore.CustomInstructionShrU:
				op = opShrU
			case railcore.CustomInstructionShrS:
				op = opShrS
			case railcore.CustomInstructionEq:
				op = opEq
			case railcore.CustomInstructionNe:
				op = opNe
			case railcore.CustomInstructionLtU:
				op = opLtU
			case railcore.CustomInstructionLtS:
				op = opLtS
			case railcore.CustomInstructionLeU:
				op = opLeU
			case railcore.CustomInstructionLeS:
				op = opLeS
			case railcore.CustomInstructionGtU:
				op = opGtU
			case railcore.CustomInstructionGtS:
				op = opGtS
			case railcore.CustomInstructionGeU:
				op = opGeU
			case railcore.CustomInstructionGeS:
				op = opGeS
			default:
				return fmt.Errorf("unsupported custom instruction operation %d", n.Op)
			}
			f.pushBinOp(op, mtI32)
			if !isCompare(op) {
				maskTop(n.Width)
			}
		}
		return nil
	}
	if err := emit(custom.Output); err != nil {
		return err
	}
	// A recipe may return its single input without an arithmetic node.
	// Its physical i32 argument still needs the logical output-width mask.
	if custom.StackCompatible && custom.Nodes[custom.Output].Op == railcore.CustomInstructionInput {
		maskTop(custom.ResultWidth)
	}
	if !custom.StackCompatible {
		result := f.popValue()
		for _, arg := range args {
			f.erase(arg)
		}
		if result.st.kind == stReg {
			f.pushReg(result.st.reg, mtI32)
		} else {
			f.pushValue(result.st)
		}
	}
	f.stats.call("custom")
	return nil
}
