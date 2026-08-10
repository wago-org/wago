//go:build amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

type checkedDeadGCUse uint8

const (
	checkedDeadGCNone checkedDeadGCUse = iota
	checkedDeadGCImmediate
	checkedDeadGCNested
)

// checkedDeadGCConstructorUse recognizes dynamic constructors whose unreachable
// payload population can disappear only after a reservation helper preserves the
// real allocation plus size/segment/value traps. Immediate drops consume no future
// operand; the nested shape uses the same bounded postfix proof as recursive fixed
// constructors.
func (f *fn) checkedDeadGCConstructorUse(r *wasm.Reader) checkedDeadGCUse {
	if !deadGCNewEnabled {
		return checkedDeadGCNone
	}
	if op, ok := r.Peek(); ok && op == 0x1a {
		return checkedDeadGCImmediate
	}
	if f.gcConstructorFeedsDroppedTree(r) {
		return checkedDeadGCNested
	}
	return checkedDeadGCNone
}

func (f *fn) finishCheckedDeadGCConstructor(r *wasm.Reader, use checkedDeadGCUse) {
	if use == checkedDeadGCImmediate {
		op, err := r.Byte()
		if err != nil || op != 0x1a {
			panic("amd64: checked dead GC constructor lost following drop")
		}
	} else if use == checkedDeadGCNested {
		f.pushValue(storage{kind: stConst, typ: mtI64}) // private null placeholder
	} else {
		panic("amd64: invalid checked dead GC constructor use")
	}
	// The checked helper now performs the dropped allocation itself and therefore
	// records the constructor's real safepoint in callGCStructHelper.
	f.stats.peep("gc-dead-new")
	f.stats.peep("gc-dead-new-checked")
}

// skipDroppedGCConstructor consumes an immediately following drop and removes
// operandN constructor arguments from the compiler stack without allocating the
// object. All constructor arguments have already been evaluated in Wasm order.
// Non-trapping deferred trees can disappear completely; if any argument still
// carries a deferred trap, flush preserves the original bottom-to-top order.
func (f *fn) skipDroppedGCConstructor(r *wasm.Reader, operandN int) bool {
	if !deadGCNewEnabled {
		return false
	}
	op, ok := r.Peek()
	if !ok || op != 0x1a { // drop
		return false
	}
	_, _ = r.Byte() // Peek proved this byte exists.
	f.discardGCConstructorOperands(operandN)
	f.retireGCFrameSafepoint()
	f.stats.peep("gc-dead-new")
	return true
}

// deferGCConstructorForDroppedStruct replaces a constructor result with a null
// placeholder only when bounded lookahead proves that it flows through a tree of
// struct.new/array.new_fixed containers whose final result is dropped. Every
// container is removed before the placeholder can become an observable Wasm value.
func (f *fn) deferGCConstructorForDroppedStruct(r *wasm.Reader, operandN int) bool {
	if !deadGCNewEnabled || !f.gcConstructorFeedsDroppedTree(r) {
		return false
	}
	f.discardGCConstructorOperands(operandN)
	f.retireGCFrameSafepoint()
	f.pushValue(storage{kind: stConst, typ: mtI64}) // private null placeholder
	f.stats.peep("gc-dead-new")
	return true
}

// gcConstructorFeedsDroppedTree recognizes a bounded postfix constructor tree.
// It tracks only how many values sit above the candidate; struct.new and
// array.new_fixed either combine that candidate into a new candidate or build a
// sibling value that a later container consumes. Dynamic array constructors are
// excluded because their checked dead path must validate the real initializer,
// not the private null placeholder used for an already-elided child.
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
			if _, err := look.S33(); err != nil {
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

// retireGCFrameSafepoint consumes the frontend's liveness entry for an
// allocation removed by this pass. Keeping the dense ID preserves every later
// site's compiler/runtime identity; no native call can publish the retired ID.
func (f *fn) retireGCFrameSafepoint() {
	plan := f.gcFrameRoots
	if plan == nil || !plan.Candidate {
		return
	}
	index := len(plan.Safepoints)
	id := plan.SafepointBase + uint32(index+1)
	if index >= len(plan.LiveLocalMasks) || id == 0 || id > shared.GCSafepointIDMax {
		plan.Exact = false
		return
	}
	plan.Safepoints = append(plan.Safepoints, shared.GCFrameSafepointPlan{ID: id})
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
			// Preserve deferred trap order. flush walks all roots bottom-to-top,
			// exactly like the constructor helper call this replaces.
			f.flush()
			for range n {
				f.erase(f.s.back())
			}
			return
		}
	}
	for i := len(dead) - 1; i >= 0; i-- {
		f.discardTree(dead[i])
	}
}
