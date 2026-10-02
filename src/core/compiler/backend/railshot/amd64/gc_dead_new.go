//go:build amd64

package amd64

import "github.com/wago-org/wago/src/core/compiler/wasm"

type checkedDeadGCUse uint8

const (
	checkedDeadGCNone checkedDeadGCUse = iota
	checkedDeadGCImmediate
	checkedDeadGCNested
)

// checkedDeadGCConstructorUse recognizes constructors whose unreachable payload
// population can disappear only after a reservation helper preserves the real
// allocation plus size/segment/value traps. Immediate drops consume no future
// operand. Nested reservations are limited to payloads whose omitted writes
// cannot remove transitive roots before a later enclosing allocation.
func (f *fn) checkedDeadGCConstructorUse(r *wasm.Reader, nestedPayloadSafe bool) checkedDeadGCUse {
	if !f.opt(optDeadGCNew) {
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
			panic("amd64: checked dead GC constructor lost following drop")
		}
	} else if use != checkedDeadGCNested {
		panic("amd64: invalid checked dead GC constructor use")
	}
	// The checked helper now performs the dropped allocation itself and therefore
	// records the constructor's real safepoint in callGCStructHelper.
	f.stats.peep("gc-dead-new")
	f.stats.peep("gc-dead-new-checked")
}

// gcConstructorFeedsDroppedTree recognizes a bounded postfix constructor tree.
// It tracks only how many values sit above the candidate; struct.new and
// array.new_fixed either combine that candidate into a new candidate or build a
// sibling value that a later container consumes. Dynamic array constructors are
// excluded because their checked dead path must validate the real initializer.
// Nested reservations retain their real compact result so every earlier child
// remains rooted across each later allocation exactly as in the source program.
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
		case 0x41: // i32.const
			if _, err := look.I32(); err != nil {
				return false
			}
			above++
		case 0x42: // i64.const
			if _, err := look.I64(); err != nil {
				return false
			}
			above++
		case 0x43: // f32.const
			if _, err := look.LEU32(); err != nil {
				return false
			}
			above++
		case 0x44: // f64.const
			if _, err := look.LEU64(); err != nil {
				return false
			}
			above++
		case 0xd0: // ref.null
			if _, _, err := readRefHeapTypeImmediate(&look); err != nil {
				return false
			}
			above++
		case 0xfd: // v128.const
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
			case 0: // struct.new typeidx
				typeIndex, err := look.U32()
				if err != nil {
					return false
				}
				st, ok := f.stagedStructType(typeIndex)
				if !ok {
					return false
				}
				operands = len(st.Comp.Fields)
			case 1: // struct.new_default typeidx
				if _, err := look.U32(); err != nil {
					return false
				}
				operands = 0
			case 8: // array.new_fixed typeidx count
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
				above = 0 // candidate was consumed and the constructor result replaces it.
			} else {
				above = above - operands + 1
			}
		case 0x1a: // drop
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
		panic("amd64: invalid dead GC constructor operand count")
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
		panic("amd64: invalid dead GC constructor operand count")
	}
	dead := roots[len(roots)-n-1 : len(roots)-1]
	for _, root := range dead {
		if !f.treeDiscardable(root) {
			// Preserve deferred trap order. flush walks all roots bottom-to-top,
			// exactly like the constructor helper call this replaces.
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
