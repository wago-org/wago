//go:build amd64

package amd64

import "os"

// Four independent scalar iterations may share one feature-qualified AVX
// register; remainder iterations use the original checked loop. Set the
// environment switch to 0 to disable this lowering for comparisons.
var regionWideIndependentEnabled = os.Getenv("WAGO_AMD64_WIDE_INDEPENDENT_LOOP") != "0"

// Two iterations of exact adjacent output pairs map to four lanes without
// reassociation. Existing stream guards cover the complete access extent;
// invariant loads remain exact scalar accesses followed by broadcasts.
var regionWideAdjacentEnabled = os.Getenv("WAGO_AMD64_WIDE_ADJACENT_LOOP") != "0"

// Reuse the original checked body only when its entry homes equal the fast
// exit homes. Other loops keep the separately emitted paired tail.
var regionWideCheckedTailEnabled = os.Getenv("WAGO_AMD64_WIDE_CHECKED_TAIL") != "0"

func (f *fn) regionCheckedTailHomes(entry *[256]regionEntryLocal) bool {
	for i := 0; i < f.nLocals; i++ {
		if entry[i].reg != f.locals[i].reg || entry[i].state != f.locals[i].state {
			return false
		}
	}
	return true
}

func (e *regionLoopEmitter) broadcastPair(reg Reg) {
	e.f.a.SseRR(0x66, 0x14, reg, reg, false)
	if e.p.wide {
		e.f.a.YInsertF128(reg, reg, reg, 1)
	}
}

// Complete wide groups use the original success-only range and alias proof.
// The paired tail consumes the next input image and exact remaining addresses.
// Emit each body once, with no bytecode rescan or second allocation pass.
func (e *regionLoopEmitter) bodyWithWidths(checkedTail int) {
	if !e.p.wide {
		e.body()
		return
	}
	if checkedTail >= 0 {
		e.bodyWithCheckedTail(checkedTail)
		return
	}
	f, p := e.f, e.p
	f.a.Load32(e.gp[0], RSP, e.off(8))
	f.a.Store64(RSP, e.off(30), e.gp[0]) // original positive trip count
	f.a.AluRI(aluTable[opAnd].digit, e.gp[0], -2, false)
	f.a.Store64(RSP, e.off(8), e.gp[0]) // even prefix, consumed by the wide body
	f.a.TestSelf(e.gp[0], false)
	single := f.a.JccPlaceholder(condE)
	e.body()
	f.a.Load32(e.gp[0], RSP, e.off(30))
	f.a.TestImm(e.gp[0], 1, false)
	done := f.a.JccPlaceholder(condE)
	// Wide publication leaves each affine input at the input image of its last
	// original iteration. Advance one step to the input of the remaining tail.
	// Non-recurrent written integer locals are not live inputs (finish proves
	// every live written integer input has this exact self-affine recurrence).
	for i, l := range p.locals[:p.localN] {
		if l.step != 0 {
			f.a.Load32(e.gp[0], RSP, e.off(i))
			f.a.AluRI(aluTable[opAdd].digit, e.gp[0], int32(l.step), false)
			f.a.Store32(RSP, e.off(i), e.gp[0])
		}
	}
	// Each parent stream starts the tail after all even-prefix iterations.
	// Child streams retain the same proven displacement from their parent.
	for i, s := range e.streams[:e.streamN] {
		if s.parent != uint8(i) || s.stride == 0 {
			continue
		}
		f.a.Load32(e.gp[0], RSP, e.off(8))
		if s.stride != 1 {
			f.a.ImulRI(e.gp[0], int32(s.stride), true)
		}
		f.a.Load64(e.gp[1], RSP, e.off(9+i))
		f.a.Add64(e.gp[1], e.gp[0])
		f.a.Store64(RSP, e.off(9+i), e.gp[1])
	}
	f.a.PatchRel32(single, f.a.Len())
	f.a.MovImm32(e.gp[0], 1)
	f.a.Store64(RSP, e.off(8), e.gp[0])
	p.wide = false
	e.body()
	p.wide = true
	f.a.PatchRel32(done, f.a.Len())
	f.stats.peep("region-loop-wide-odd-pair")
	f.stats.peep("region-loop-wide-paired-tail")
}

func (e *regionLoopEmitter) bodyWithCheckedTail(checkedTail int) {
	f := e.f
	mask := int32(e.p.iterationsPerVector() - 1)
	f.a.Load32(e.gp[0], RSP, e.off(8))
	f.a.Store64(RSP, e.off(30), e.gp[0])
	f.a.AluRI(aluTable[opAnd].digit, e.gp[0], ^mask, false)
	f.a.Store64(RSP, e.off(8), e.gp[0])
	f.a.TestSelf(e.gp[0], false)
	single := f.a.JccPlaceholder(condE)
	f.a.PatchRel32(single, checkedTail)
	e.body()
	// The complete groups have committed their original scalar exit values, including
	// every induction local. Matching entry/exit homes make those values the
	// checked body's next input. Its original predicate consumes the remainder.
	f.a.Load32(e.gp[0], RSP, e.off(30))
	f.a.TestImm(e.gp[0], uint32(mask), false)
	odd := f.a.JccPlaceholder(condNE)
	f.a.PatchRel32(odd, checkedTail)
	f.stats.peep("region-loop-wide-checked-tail")
}

// Preserve exact eight-byte memory accesses while defining all wide lanes.
var regionWideBroadcastEnabled = os.Getenv("WAGO_AMD64_WIDE_LOOP_BROADCAST") != "0"
