//go:build arm64

package arm64

import "github.com/wago-org/wago/src/core/compiler/wasm"

type checkedDeadGCUse uint8

const (
	checkedDeadGCNone checkedDeadGCUse = iota
	checkedDeadGCImmediate
	checkedDeadGCNested
)

func (f *fn) checkedDeadGCConstructorUse(r *wasm.Reader, nestedPayloadSafe bool) checkedDeadGCUse {
	if !f.policy.EnabledOption(optGCDeadNew) {
		return checkedDeadGCNone
	}
	if op, ok := r.Peek(); ok && op == 0x1a {
		return checkedDeadGCImmediate
	}
	if nestedPayloadSafe && f.gcConstructorFeedsDroppedTree(r) {
		return checkedDeadGCNested
	}
	return checkedDeadGCNone
}

func deadGCReservationResults(typeIndex uint32, use checkedDeadGCUse) []wasm.ValType {
	if use != checkedDeadGCNested {
		return nil
	}
	result := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: typeIndex}), false))
	return []wasm.ValType{result}
}

func (f *fn) reserveDeadGCStructConstructor(typeIndex uint32, operandN int, use checkedDeadGCUse) error {
	f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(typeIndex)})
	if err := f.callGCStructHelper(gcStructReserveDead, []wasm.ValType{wasm.I32}, deadGCReservationResults(typeIndex, use)); err != nil {
		return err
	}
	if use == checkedDeadGCNested {
		f.discardGCConstructorOperandsBelowResult(operandN)
	} else {
		f.discardGCConstructorOperands(operandN)
	}
	return nil
}

func (f *fn) reserveDeadGCFixedArrayConstructor(typeIndex, count uint32, use checkedDeadGCUse) error {
	f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(typeIndex)})
	f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(count)})
	if err := f.callGCStructHelper(gcArrayCheckFixed, []wasm.ValType{wasm.I32, wasm.I32}, deadGCReservationResults(typeIndex, use)); err != nil {
		return err
	}
	if use == checkedDeadGCNested {
		f.discardGCConstructorOperandsBelowResult(int(count))
	} else {
		f.discardGCConstructorOperands(int(count))
	}
	return nil
}

func (f *fn) finishCheckedDeadGCConstructor(r *wasm.Reader, use checkedDeadGCUse) {
	if use == checkedDeadGCImmediate {
		op, err := r.Byte()
		if err != nil || op != 0x1a {
			panic("arm64: checked dead GC constructor lost following drop")
		}
	} else if use != checkedDeadGCNested {
		panic("arm64: invalid checked dead GC constructor use")
	}
	f.stats.peep("gc-dead-new")
	f.stats.peep("gc-dead-new-checked")
}

// gcConstructorFeedsDroppedTree recognizes at most 32 postfix instructions.
// It retains a real compact result from every nested reservation, keeping prior
// children rooted across later allocation helpers without retaining an IR.
func (f *fn) gcConstructorFeedsDroppedTree(r *wasm.Reader) bool {
	look := *r
	above := 0
	const maxLookaheadInstructions = 32
	for range maxLookaheadInstructions {
		op, err := look.Byte()
		if err != nil {
			return false
		}
		switch op {
		case 0x20, 0x23, 0xd2: // local.get, global.get, ref.func
			if _, err := look.U32(); err != nil {
				return false
			}
			above++
		case 0x41:
			if _, err := look.I32(); err != nil {
				return false
			}
			above++
		case 0x42:
			if _, err := look.I64(); err != nil {
				return false
			}
			above++
		case 0x43:
			if _, err := look.LEU32(); err != nil {
				return false
			}
			above++
		case 0x44:
			if _, err := look.LEU64(); err != nil {
				return false
			}
			above++
		case 0xd0:
			if _, _, err := readRefHeapTypeImmediate(&look); err != nil {
				return false
			}
			above++
		case 0xfd:
			sub, err := look.U32()
			if err != nil || sub != 12 {
				return false
			}
			if _, err := look.Bytes(16); err != nil {
				return false
			}
			above++
		case 0xfb:
			sub, err := look.U32()
			if err != nil {
				return false
			}
			var operands int
			switch sub {
			case 0:
				typeIndex, err := look.U32()
				if err != nil {
					return false
				}
				st, ok := f.stagedStructType(typeIndex)
				if !ok {
					return false
				}
				operands = len(st.Comp.Fields)
			case 1:
				if _, err := look.U32(); err != nil {
					return false
				}
			case 8:
				if _, err := look.U32(); err != nil {
					return false
				}
				count, err := look.U32()
				if err != nil || uint64(count) > uint64(^uint(0)>>1) {
					return false
				}
				operands = int(count)
			default:
				return false
			}
			if operands > above {
				above = 0
			} else {
				above = above - operands + 1
			}
		case 0x1a:
			if above == 0 {
				return true
			}
			above--
		default:
			return false
		}
	}
	return false
}

func (f *fn) discardGCConstructorOperands(n int) {
	if n == 0 {
		return
	}
	roots := f.rootsBottomToTop()
	if n < 0 || n > len(roots) {
		panic("arm64: invalid dead GC constructor operand count")
	}
	dead := roots[len(roots)-n:]
	for _, root := range dead {
		if !f.treeDiscardable(root) {
			f.flush()
			for _, operand := range dead {
				f.erase(operand)
			}
			return
		}
	}
	for i := len(dead) - 1; i >= 0; i-- {
		f.discardTree(dead[i])
	}
}

func (f *fn) discardGCConstructorOperandsBelowResult(n int) {
	if n == 0 {
		return
	}
	roots := f.rootsBottomToTop()
	if n < 0 || n+1 > len(roots) {
		panic("arm64: invalid dead GC constructor operand count")
	}
	dead := roots[len(roots)-n-1 : len(roots)-1]
	for _, root := range dead {
		if !f.treeDiscardable(root) {
			f.flush()
			for _, operand := range dead {
				f.erase(operand)
			}
			return
		}
	}
	for i := len(dead) - 1; i >= 0; i-- {
		f.discardTree(dead[i])
	}
}
