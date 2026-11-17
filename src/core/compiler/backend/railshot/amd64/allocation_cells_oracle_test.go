//go:build amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func oldAllocationScanLoopCallFreeDetails(r *wasm.Reader, classifier wasm.ModuleInstructionClassifier, gcStructHelpers bool, pinnedLocals []int, wantFloat bool, budget *int) (bool, uint64, storage, bool) {
	if budget == nil || *budget >= maxCallFreeLoopLookaheadBytes {
		return false, 0, storage{}, false
	}
	r2 := *r
	depth := 0
	var writes uint64
	brTable := false
	nestedLoop := false
	hasInnerControl := false
	type candidate struct {
		st    storage
		score uint32
	}
	var candidates [16]candidate
	n, steps := 0, 0
	floatSafe := true
	finish := func(writes uint64) (bool, uint64, storage, bool) {
		if !wantFloat || !floatSafe || n == 0 {
			return true, writes, storage{}, false
		}
		best := 0
		for i := 1; i < n; i++ {
			if candidates[i].score > candidates[best].score {
				best = i
			}
		}
		return true, writes, candidates[best].st, true
	}
	var imm wasm.InstructionImmediate
	for {
		before := r2.Offset()
		op, err := r2.Byte()
		if err != nil {
			return false, 0, storage{}, false
		}
		// Immediate-free instructions cannot write a local, introduce control,
		// select a float literal, or call a GC/atomic helper. Keep their proof
		// step scalar instead of clearing and filling an unused immediate record.
		if _, simple := wasm.ImmediateFreeInstructionKind(op); simple {
			if *budget == maxCallFreeLoopLookaheadBytes {
				return false, 0, storage{}, false
			}
			*budget++
			steps++
			continue
		}
		// The proof observes only calls, local writes, bulk effects and float
		// literals. Decode ordinary scalar forms without an unused full record.
		kind, index, bulk := wasm.InstrInvalid, uint32(0), false
		var floatBits int64
		if op >= 0x28 && op <= 0x3e {
			err = classifier.SkipScalarMemoryImmediate(&r2)
		} else {
			switch op {
			case 0x05, 0x0b: // else/end have no immediate or proof effect.
			case 0x20, 0x23, 0x24, 0x0c, 0x0d, 0xd2, 0xd5, 0xd6:
				_, err = r2.U32()
			case 0x21, 0x22:
				index, err = r2.U32()
				kind = wasm.InstrLocalSet
			case 0x10, 0x12, 0x14, 0x15:
				_, err = r2.U32()
				kind = wasm.InstrCall
			case 0x41:
				_, err = r2.I32()
			case 0x42:
				_, err = r2.I64()
			case 0x43:
				var bits uint32
				bits, err = r2.LEU32()
				floatBits = int64(bits)
			case 0x44:
				var bits uint64
				bits, err = r2.LEU64()
				floatBits = int64(bits)
			default:
				err = classifier.ClassifyInto(&r2, op, &imm)
				kind, index, bulk = imm.Kind, imm.Index, imm.UsesBulkMemory
			}
		}
		if err != nil {
			used := min(maxCallFreeLoopLookaheadBytes-*budget, r2.Offset()-before)
			*budget += used
			return false, 0, storage{}, false
		}
		consumed := r2.Offset() - before
		remaining := maxCallFreeLoopLookaheadBytes - *budget
		if consumed > remaining {
			*budget = maxCallFreeLoopLookaheadBytes
			return false, 0, storage{}, false
		}
		*budget += consumed
		if wantFloat && steps < 256 && (op == 0x43 || op == 0x44) {
			st := storage{kind: stConst, typ: mtF32, cval: floatBits}
			if op == 0x44 {
				st.typ = mtF64
			}
			weight := uint32(1) << min(depth*2, 8)
			found := false
			for i := 0; i < n; i++ {
				if candidates[i].st.typ == st.typ && candidates[i].st.cval == st.cval {
					candidates[i].score += weight
					found = true
					break
				}
			}
			if !found && n < len(candidates) {
				candidates[n] = candidate{st: st, score: weight}
				n++
			}
		}
		steps++
		if op == 0x02 || op == 0x04 || op == 0x1f {
			hasInnerControl = true
		}
		switch kind {
		case wasm.InstrCall, wasm.InstrCallIndirect, wasm.InstrReturnCall,
			wasm.InstrReturnCallIndirect, wasm.InstrCallRef, wasm.InstrReturnCallRef,
			wasm.InstrMemoryGrow:
			return false, 0, storage{}, false
		case wasm.InstrTableSet, wasm.InstrTableInit, wasm.InstrTableCopy,
			wasm.InstrTableGrow, wasm.InstrTableFill:
			floatSafe = false // GC table helpers may call even in a numeric loop.
		case wasm.InstrLocalSet, wasm.InstrLocalTee:
			for i, x := range pinnedLocals {
				if uint32(x) == index {
					writes |= uint64(1) << i
					break
				}
			}
		case wasm.InstrBrTable:
			// br_table first reconciles every pin to its frame slot. Its
			// per-target stubs then undo register-only loop state, often on
			// the hot path. Keep the existing stack convergence there.
			brTable = true
		}
		if gcOrAtomicInstructionMayCall(kind, gcStructHelpers) {
			return false, 0, storage{}, false
		}
		if bulk {
			floatSafe = false
		}
		switch op {
		case 0x02, 0x03, 0x04, 0x1f:
			if op == 0x03 {
				nestedLoop = true
			}
			depth++
		case 0x0b:
			if depth == 0 {
				// Nested loop entries already reconcile their own pin state. An
				// outer register-only header can add stores at that inner entry
				// without removing stores on its hot backedge. Inner blocks and
				// ifs likewise create merge edges where the weaker register-only
				// state adds reconciliation outside the loop latch.
				if brTable || nestedLoop || hasInnerControl {
					return finish(0)
				}
				return finish(writes)
			}
			depth--
		}
	}
}

func (f *fn) oldAllocationUnsignedAddressMaximum(e *elem, wide bool, budget *int) uint64 {
	limit := uint64(^uint32(0))
	typ := mtI32
	if wide {
		limit = ^uint64(0)
		typ = mtI64
	}
	if e == nil || *budget <= 0 || e.st.typ != typ {
		return limit
	}
	*budget--
	if e.elemKind() == ekValue {
		if e.st.kind == stConst {
			value := uint64(e.st.cval)
			if !wide {
				value = uint64(uint32(value))
			}
			return value
		}
		return limit
	}
	if e.elemKind() != ekDeferred {
		return limit
	}
	op := e.deferredOp()
	switch op {
	case opAdd, opAnd, opOr, opXor, opMul, opShl, opShrU:
	default:
		return limit
	}
	left, right := e.arg0, e.arg1
	a := f.oldAllocationUnsignedAddressMaximum(left, wide, budget)
	b := f.oldAllocationUnsignedAddressMaximum(right, wide, budget)
	constant := uint64(0)
	known := right != nil && right.elemKind() == ekValue && right.st.kind == stConst && right.st.typ == typ
	if known {
		constant = uint64(right.st.cval)
	}
	return shared.UnsignedOperationMaximum(shared.IntOp(op), a, b, constant, known, wide)
}
