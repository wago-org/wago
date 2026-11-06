//go:build amd64

package amd64

import "github.com/wago-org/wago/src/core/compiler/wasm"

var pureIntegerIfSelectEnabled = true

type pureIntegerIfPlan struct {
	arms  [2]wasm.Reader
	stops [2]int
	end   int
}

// Only bounded, nontrapping integer expressions may be evaluated on both paths.
// Each arm must build its own result without consuming the caller's operands.
func (f *fn) scanPureIntegerIf(r wasm.Reader, typ machineType) (pureIntegerIfPlan, bool) {
	var plan pureIntegerIfPlan
	if typ != mtI32 && typ != mtI64 {
		return plan, false
	}
	start, totalCost := r.Offset(), 0
	for arm := range plan.arms {
		plan.arms[arm] = r
		depth, cost := 0, 0
		for count := 0; ; count++ {
			if count > 8 || r.Offset()-start > 64 {
				return plan, false
			}
			pos := r.Offset()
			op, err := r.Byte()
			if err != nil {
				return plan, false
			}
			if op == 0x05 || op == 0x0b {
				want := byte(0x05)
				if arm == 1 {
					want = 0x0b
				}
				if op != want || depth != 1 {
					return plan, false
				}
				plan.stops[arm] = pos
				break
			}
			switch {
			case op == 0x01:
			case op == 0x20:
				x, err := r.U32()
				absolute := uint64(x) + uint64(f.localBase)
				if err != nil || absolute >= uint64(len(f.localType)) || f.localType[absolute] != typ {
					return plan, false
				}
				depth++
			case op == 0x41 && typ == mtI32:
				if _, err := r.I32(); err != nil {
					return plan, false
				}
				depth++
			case op == 0x42 && typ == mtI64:
				if _, err := r.I64(); err != nil {
					return plan, false
				}
				depth++
			default:
				base := byte(0x6a)
				if typ == mtI64 {
					base = 0x7c
				}
				delta := int(op) - int(base)
				if depth < 2 || !(delta >= 0 && delta <= 2 || delta >= 7 && delta <= 14) {
					return plan, false
				}
				depth--
				cost++
				if delta == 2 {
					cost++
				}
			}
			if depth > 4 || cost > 4 {
				return plan, false
			}
		}
		totalCost += cost
	}
	if r.Offset()-start > 64 || totalCost > 6 {
		return plan, false
	}
	plan.end = r.Offset()
	return plan, true
}

func (f *fn) tryPureIntegerIfSelect(r *wasm.Reader, typ machineType) (bool, error) {
	if !f.opt(optPureIntegerIfSelect) || profileEnabled || f.moduleEH || f.gcFrameRoots != nil || f.policy.CompactNative ||
		len(f.customInstructions) != 0 || len(f.intervalLast) != 0 || f.intervalControl || f.inlineDepth != 0 || f.inlineForwardN != 0 ||
		f.stats != nil && f.stats.RecordSources || f.s.back() == f.s.head || !treeReorderSafe(f.s.back()) {
		return false, nil
	}
	plan, ok := f.scanPureIntegerIf(*r, typ)
	if !ok {
		return false, nil
	}
	if err := r.JumpTo(plan.end); err != nil {
		return false, err
	}
	condition := f.s.back()
	// Preserve the ordinary if boundary for earlier trapping operands. Both
	// arms read unchanged locals and contain only bounded nontrapping arithmetic.
	f.materializeTrapsBefore(condition)
	for arm, reader := range plan.arms {
		for reader.Offset() < plan.stops[arm] {
			op, err := reader.Byte()
			if err != nil {
				return true, err
			}
			if op == 0x01 {
				continue
			}
			if err := f.emitPlain(&reader, op); err != nil {
				return true, err
			}
		}
	}
	f.s.moveValentBlockToTop(condition)
	f.emitSelect()
	f.stats.peep("pure-integer-if-select")
	return true, nil
}

// Nodes are pointer-stable even when arm construction grows the arena. Move
// the complete condition tree and invalidate positional stack/barrier caches.
func (s *stack) moveValentBlockToTop(root *elem) {
	if root == s.back() {
		return
	}
	first := baseOfValentBlock(root)
	before, after, last := first.prev, root.next, s.head.prev
	before.next, after.prev = after, before
	first.prev, last.next = last, first
	root.next, s.head.prev = s.head, root
	s.canonicalSlots = false
	s.pendingPrefix, s.pendingPrefixLoads = nil, false
}
