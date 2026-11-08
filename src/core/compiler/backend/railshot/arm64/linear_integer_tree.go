//go:build arm64

package arm64

import (
	"math/bits"
	"os"
)

var linearIntegerTreeEnabled = true

var linearFirstTermEnabled = os.Getenv("WAGO_ARM64_NO_LINEAR_FIRST_TERM") != "1"

type linearIntegerTerm struct {
	source      *elem
	coefficient uint32
	index       uint32
	reads       int
	reg         Reg
	owned       bool
}
type linearIntegerTree struct {
	f                         *fn
	terms                     [8]linearIntegerTerm
	count, budget, operations int
	constant                  uint32
}

// Arithmetic is collected modulo 2^32. Borrowed current-local reads have no
// observable effects; captured older reads, globals, memory, and trapping nodes
// fall back. Fixed scratch and a node budget bound planning cost.
func (w *linearIntegerTree) collect(e *elem, scale uint32) bool {
	if e == nil || w.budget == 0 || e.st.typ != mtI32 {
		return false
	}
	w.budget--
	if e.elemKind() == ekValue {
		if e.st.kind == stConst {
			w.constant += uint32(e.st.cval) * scale
			return true
		}
		if e.st.kind != stLocalReg && e.st.kind != stLocalRef {
			return false
		}
		for i := 0; i < w.count; i++ {
			t := &w.terms[i]
			if t.index == e.st.idx {
				t.coefficient += scale
				t.reads++
				return true
			}
		}
		if w.count == len(w.terms) {
			return false
		}
		w.terms[w.count] = linearIntegerTerm{source: e, coefficient: scale, index: e.st.idx, reads: 1}
		w.count++
		return true
	}
	if !e.isDeferred() {
		return false
	}
	a, b := w.f.s.arg0(e), w.f.s.arg1(e)
	w.operations++
	switch e.deferredOp() {
	case opAdd:
		return w.collect(a, scale) && w.collect(b, scale)
	case opSub:
		return w.collect(a, scale) && w.collect(b, -scale)
	case opMul:
		if a != nil && a.elemKind() == ekValue && a.st.kind == stConst {
			a, b = b, a
		}
		if b == nil || b.elemKind() != ekValue || b.st.kind != stConst {
			return false
		}
		return w.collect(a, scale*uint32(b.st.cval))
	case opShl:
		if b == nil || b.elemKind() != ekValue || b.st.kind != stConst {
			return false
		}
		return w.collect(a, scale<<(uint32(b.st.cval)&31))
	}
	return false
}

func (f *fn) tryLinearIntegerTree(node *elem, dest Reg) Reg {
	shifted := node.deferredOp() == opShl && shiftedLinearTreeEnabled
	if !f.opt(optLinearIntegerTree) || !f.hasLoop || node.st.typ != mtI32 || node.deferredDepth() < 2 || (node.deferredOp() != opAdd && node.deferredOp() != opSub && !shifted) || len(f.customInstructions) != 0 {
		return regNone
	}
	w := linearIntegerTree{f: f, budget: 64}
	if !w.collect(node, 1) {
		return regNone
	}
	nonzero, duplicate := 0, false
	for _, t := range w.terms[:w.count] {
		if t.coefficient != 0 {
			nonzero++
		}
		duplicate = duplicate || t.reads > 1
	}
	if !shifted && duplicate && nonzero == 2 && w.operations >= 3 && w.constant == 0 {
		if result := f.emitLinearIntegerPair(node, dest, &w); result != regNone {
			return result
		}
	}
	if shifted {
		// Distinct current locals avoid hiding an earlier duplicate-source cover
		// inside the cost comparison. Modular shifts can discard expensive high
		// coefficient bits even when no source is repeated.
		if duplicate || nonzero < 2 || nonzero > 4 || !f.profitableShiftedLinearTree(node, &w) {
			return regNone
		}
	} else if node.deferredDepth() < 4 || !duplicate || w.operations < nonzero*2+3 {
		return regNone
	}
	// Keep every surviving borrowed source live while materializing stack locals.
	// Reserve room for those loads plus the result and coefficient before emitting
	// anything: unlike the original tree, this cover holds all sources at once.
	blocked := f.pinned.union(f.pinnedLocalMask).union(f.reserved)
	needed := 2
	for _, t := range w.terms[:w.count] {
		if t.coefficient == 0 {
			continue
		}
		if t.source.st.kind == stLocalReg {
			blocked = blocked.add(t.source.st.reg)
		} else {
			needed++
		}
	}
	available := 0
	for _, r := range gpAlloc {
		if !blocked.has(r) {
			available++
		}
	}
	if available < needed {
		return regNone
	}
	savedPins := f.pinned
	for i := 0; i < w.count; i++ {
		t := &w.terms[i]
		if t.coefficient == 0 {
			continue
		}
		t.reg, t.owned = f.materializeRead(t.source)
		f.pinned = f.pinned.add(t.reg)
	}
	result := dest
	for _, t := range w.terms[:w.count] {
		if t.coefficient != 0 && t.reg == result {
			result = regNone
			break
		}
	}
	if result == regNone {
		result = f.allocReg(0)
	}
	first := w.constant == 0 && nonzero > 0 && (shifted || linearFirstTermEnabled)
	if first && !shifted {
		f.stats.peep("linear-first-term")
	}
	if !first {
		f.a.MovImm32(result, int32(w.constant))
	}
	coefficient := regNone
	for _, t := range w.terms[:w.count] {
		c := t.coefficient
		if c == 0 {
			continue
		}
		addend := result
		if first {
			addend, first = ZR, false
		}
		switch {
		case c == 1:
			f.a.Add32(result, addend, t.reg)
		case c == ^uint32(0):
			f.a.Sub32(result, addend, t.reg)
		case c&(c-1) == 0:
			f.a.AddShifted(result, addend, t.reg, uint8(bits.TrailingZeros32(c)), true)
		default:
			if coefficient == regNone {
				coefficient = f.allocReg(maskOf(result))
			}
			f.a.MovImm32(coefficient, int32(c))
			f.a.Madd32(result, t.reg, coefficient, addend)
		}
	}
	if coefficient != regNone {
		f.release(coefficient)
	}
	if dest != regNone && result != dest {
		f.a.MovReg32(dest, result)
		f.release(result)
		result = dest
	}
	f.pinned = savedPins
	for _, t := range w.terms[:w.count] {
		if t.coefficient != 0 && t.owned && t.reg != result {
			f.release(t.reg)
		}
	}
	if shifted {
		f.stats.peep("shifted-linear-tree")
	}
	f.stats.peep("linear-integer-tree")
	f.consumeBlockBelow(node)
	f.occupy(node, result)
	return result
}

// emitLinearIntegerPair covers two surviving terms when their coefficients fit
// one ADD (shifted register). Collection has proved exact modular I32 arithmetic
// over current-local reads, so cancellation neither drops traps nor old values.
func (f *fn) emitLinearIntegerPair(node *elem, dest Reg, tree *linearIntegerTree) Reg {
	var base, index *linearIntegerTerm
	for i := 0; i < tree.count; i++ {
		term := &tree.terms[i]
		if term.coefficient == 0 {
			continue
		}
		if base == nil && term.coefficient == 1 {
			base = term
		} else {
			index = term
		}
	}
	if base == nil || index == nil || index.coefficient == 0 || index.coefficient&(index.coefficient-1) != 0 {
		return regNone
	}
	blocked := f.pinned.union(f.pinnedLocalMask).union(f.reserved)
	needed := 1
	for _, term := range []*linearIntegerTerm{base, index} {
		if term.source.st.kind == stLocalReg {
			blocked = blocked.add(term.source.st.reg)
		} else {
			needed++
		}
	}
	available := 0
	for _, reg := range gpAlloc {
		if !blocked.has(reg) {
			available++
		}
	}
	if available < needed {
		return regNone
	}
	saved := f.pinned
	a, ownA := f.materializeRead(base.source)
	f.pinned = f.pinned.add(a)
	b, ownB := f.materializeRead(index.source)
	f.pinned = f.pinned.add(b)
	result := dest
	if result == regNone {
		result = f.allocReg(0)
	}
	f.a.AddShifted(result, a, b, uint8(bits.TrailingZeros32(index.coefficient)), true)
	f.pinned = saved
	if ownA && a != result {
		f.release(a)
	}
	if ownB && b != a && b != result {
		f.release(b)
	}
	f.stats.peep("linear-integer-pair")
	f.consumeBlockBelow(node)
	f.occupy(node, result)
	return result
}
