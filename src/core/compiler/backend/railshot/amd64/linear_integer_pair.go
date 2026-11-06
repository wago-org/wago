//go:build amd64

package amd64

import "math/bits"

var linearIntegerTreeEnabled = true

type linearPairTerm struct {
	source      *elem
	coefficient uint32
	index       uint32
	reads       int
}

type linearPairTree struct {
	terms                     [8]linearPairTerm
	count, budget, operations int
	constant                  uint32
}

// Collect modulo2^32 over current-local reads only. A captured older value,
// memory/global read, trapping operation or nonconstant product keeps its
// existing evaluation; even a zero coefficient must prove its whole subtree.
func (w *linearPairTree) collect(e *elem, scale uint32) bool {
	if e == nil || w.budget == 0 || e.valueType() != mtI32 {
		return false
	}
	w.budget--
	if e.isValue() {
		if e.st.kind == stConst {
			w.constant += uint32(e.st.cval) * scale
			return true
		}
		if e.st.kind != stLocalReg && e.st.kind != stLocalRef {
			return false
		}
		for i := 0; i < w.count; i++ {
			if w.terms[i].index == e.st.idx {
				w.terms[i].coefficient += scale
				w.terms[i].reads++
				return true
			}
		}
		if w.count == len(w.terms) {
			return false
		}
		w.terms[w.count] = linearPairTerm{source: e, coefficient: scale, index: e.st.idx, reads: 1}
		w.count++
		return true
	}
	if !e.isDeferred() {
		return false
	}
	a, b := e.arg0, e.arg1
	w.operations++
	switch e.deferredOp() {
	case opAdd:
		return w.collect(a, scale) && w.collect(b, scale)
	case opSub:
		return w.collect(a, scale) && w.collect(b, -scale)
	case opMul:
		if a != nil && a.isValue() && a.st.kind == stConst {
			a, b = b, a
		}
		if b == nil || !b.isValue() || b.st.kind != stConst {
			return false
		}
		return w.collect(a, scale*uint32(b.st.cval))
	case opShl:
		if b == nil || !b.isValue() || b.st.kind != stConst {
			return false
		}
		return w.collect(a, scale<<(uint32(b.st.cval)&31))
	}
	return false
}

func (f *fn) tryLinearIntegerPair(node *elem, dest Reg) Reg {
	if !f.opt(optLinearIntegerTree) || !f.hasLoop || profileEnabled || node.valueType() != mtI32 || node.deferredDepth() < 2 ||
		(node.deferredOp() != opAdd && node.deferredOp() != opSub) || len(f.customInstructions) != 0 ||
		len(f.intervalLast) != 0 || f.intervalControl {
		return regNone
	}
	w := linearPairTree{budget: 64}
	if !w.collect(node, 1) || w.operations < 3 {
		return regNone
	}
	var base, index *linearPairTerm
	duplicate, nonzero := false, 0
	for i := 0; i < w.count; i++ {
		term := &w.terms[i]
		duplicate = duplicate || term.reads > 1
		if term.coefficient == 0 {
			continue
		}
		nonzero++
		if base == nil && term.coefficient == 1 {
			base = term
		} else {
			index = term
		}
	}
	if !duplicate || nonzero != 2 || base == nil || index == nil || index.coefficient > 8 || index.coefficient&(index.coefficient-1) != 0 {
		return regNone
	}
	// Bound simultaneous source loads and the result before emitting anything.
	// The original tree remains available if the LEA cover cannot fit.
	blocked := f.pinned.union(f.pinnedLocalMask).union(f.reserved)
	needed, available := 1, 0
	for _, term := range []*linearPairTerm{base, index} {
		if term.source.st.kind == stLocalReg {
			blocked = blocked.add(term.source.st.reg)
		} else {
			needed++
		}
	}
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
	if dest == regNone {
		dest = f.allocReg(0)
	}
	f.a.LeaScaledW(dest, a, b, uint8(bits.TrailingZeros32(index.coefficient)), int32(w.constant), false)
	f.pinned = saved
	if ownA && a != dest {
		f.release(a)
	}
	if ownB && b != a && b != dest {
		f.release(b)
	}
	f.consumeBlockBelow(node)
	f.occupy(node, dest)
	node.setDeferredOp(opNone)
	node.st.setValueFacts(factUpper32Zero)
	f.stats.peep("linear-integer-pair")
	return dest
}
