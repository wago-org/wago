//go:build arm64

package arm64

import (
	"bytes"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

type tailSelectExpr struct {
	source wasm.Reader
	end    int
	typ    machineType
	depth  uint8
}

func (e tailSelectExpr) bytes() []byte { r := e.source; b, _ := r.Bytes(e.end - r.Offset()); return b }

// Each argument packet is bounded pure integer arithmetic: no loads, writes,
// calls, comparisons or trapping instructions may be speculated across the if.
func (f *fn) scanTailSelectArm(r *wasm.Reader, target *inlineTarget, ending byte) ([4]tailSelectExpr, bool) {
	types := f.inlineTargets.localTypes(target)
	return f.scanPureIntegerPacket(r, types, types[:target.paramCount()], 0x12, ending)
}

func (f *fn) scanPureIntegerPacket(r *wasm.Reader, types, expected []machineType, terminal, ending byte) ([4]tailSelectExpr, bool) {
	var result [4]tailSelectExpr
	var stack [8]tailSelectExpr
	count := 0
	start := r.Offset()
	for operations := 0; operations < 32 && r.Offset()-start <= 96; operations++ {
		source := *r
		op, err := r.Byte()
		if err != nil {
			return result, false
		}
		typ := mtNone
		switch {
		case op == terminal:
			if terminal == 0x12 {
				index, err := r.U32()
				if err != nil || index != f.traceFuncIdx {
					return result, false
				}
			}
			if count != len(expected) || count > len(result) {
				return result, false
			}
			stop, err := r.Byte()
			if err != nil || stop != ending {
				return result, false
			}
			for i := 0; i < count; i++ {
				if stack[i].typ != expected[i] {
					return result, false
				}
				result[i] = stack[i]
			}
			return result, true
		case op == 0x20:
			index, err := r.U32()
			if err != nil || uint64(index) >= uint64(len(types)) {
				return result, false
			}
			typ = types[index]
		case op == 0x41:
			if _, err := r.I32(); err != nil {
				return result, false
			}
			typ = mtI32
		case op == 0x42:
			if _, err := r.I64(); err != nil {
				return result, false
			}
			typ = mtI64
		case op >= 0x6a && op <= 0x6c || op >= 0x71 && op <= 0x78 || op >= 0x7c && op <= 0x7e || op >= 0x83 && op <= 0x8a:
			typ = mtI32
			if op >= 0x7c {
				typ = mtI64
			}
			if count < 2 || stack[count-1].typ != typ || stack[count-2].typ != typ {
				return result, false
			}
			right, left := stack[count-1], stack[count-2]
			left.end = r.Offset()
			left.depth = max(left.depth, right.depth) + 1
			if left.depth > 4 {
				return result, false
			}
			count--
			stack[count-1] = left
			continue
		default:
			return result, false
		}
		if (typ != mtI32 && typ != mtI64) || count == len(stack) {
			return result, false
		}
		stack[count] = tailSelectExpr{source: source, end: r.Offset(), typ: typ, depth: 1}
		count++
	}
	return result, false
}

func (f *fn) emitTailSelectExpr(expr tailSelectExpr) error {
	r := wasm.ReaderFrom(expr.bytes())
	for r.HasNext() {
		f.wasmPC = f.tracePCBase + uint32(expr.source.Offset()+r.Offset())
		op, err := r.Byte()
		if err != nil {
			return err
		}
		if err = f.emitPlain(&r, op); err != nil {
			return err
		}
	}
	return nil
}

// Select pure arguments for two self-tail arms, then make one transfer. Argument
// values are captured before any parameter assignment, preserving permutations.
func (f *fn) tryTailArgumentSelect(r *wasm.Reader) (bool, error) {
	if f.inlineTailLoopFrame == 0 || profileEnabled || f.interruptible || f.moduleEH || f.gcFrameRoots != nil || len(f.customInstructions) != 0 {
		return false, nil
	}
	target := f.inlineTargets.target(int(f.traceFuncIdx))
	if target == nil || !target.tailLoop() || target.paramCount() < 1 || target.paramCount() > 4 {
		return false, nil
	}
	condition := f.s.back()
	if condition == nil || condition.elemKind() == ekValue && condition.st.kind == stConst {
		return false, nil
	}
	free := 0
	blocked := f.pinned.union(f.pinnedLocalMask).union(f.reserved)
	for _, reg := range gpAlloc {
		if f.regUser[reg] == nil && !blocked.has(reg) {
			free++
		}
	}
	if free < target.paramCount()+5 {
		return false, nil
	}
	scan := *r
	left, ok := f.scanTailSelectArm(&scan, target, 0x05)
	if !ok {
		return false, nil
	}
	right, ok := f.scanTailSelectArm(&scan, target, 0x0b)
	if !ok {
		return false, nil
	}
	f.materializePendingTraps()
	condition = f.s.back()
	if free >= 2*target.paramCount()+3 && f.tailSelectFlagsCondition(condition) {
		return f.emitTailSelectOnFlags(r, scan.Offset(), left, right, target, condition)
	}
	cond := f.materialize(f.popValue())
	previousPins := f.pinned
	f.pinned = f.pinned.add(cond)
	held := true
	defer func() {
		if held {
			f.pinned = previousPins
			f.release(cond)
		}
	}()
	for i := 0; i < target.paramCount(); i++ {
		if bytes.Equal(left[i].bytes(), right[i].bytes()) {
			if err := f.emitTailSelectExpr(left[i]); err != nil {
				return true, err
			}
			continue
		}
		if err := f.emitTailSelectExpr(left[i]); err != nil {
			return true, err
		}
		lroot := f.s.back()
		lreg, lowned := f.materializeRead(lroot)
		saved := f.pinned
		f.pinned = f.pinned.add(lreg)
		if err := f.emitTailSelectExpr(right[i]); err != nil {
			return true, err
		}
		rroot := f.s.back()
		rreg, rowned := f.materializeRead(rroot)
		f.pinned = f.pinned.add(rreg)
		result := regNone
		if lowned {
			result = lreg
		} else if rowned {
			result = rreg
		} else {
			result = f.allocReg(maskOf(lreg, rreg, cond))
		}
		f.a.CmpImm32(cond, 0)
		f.a.Csel(result, rreg, lreg, condE, left[i].typ.is64())
		f.pinned = saved
		if lowned && result != lreg {
			f.release(lreg)
		}
		if rowned && result != rreg && rreg != lreg {
			f.release(rreg)
		}
		f.erase(rroot)
		f.erase(lroot)
		root := f.pushReg(result, left[i].typ)
		if left[i].typ == mtI32 {
			root.st.setValueFacts(factUpper32Zero)
		}
	}
	f.pinned = previousPins
	f.release(cond)
	held = false
	if err := r.JumpTo(scan.Offset()); err != nil {
		return true, err
	}
	f.stats.peep("tail-branch-select")
	return true, f.returnCallIndex(f.traceFuncIdx)
}
