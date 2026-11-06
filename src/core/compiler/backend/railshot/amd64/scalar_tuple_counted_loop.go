//go:build amd64

package amd64

// tupleLoopUnitCounter proves the entire condition is a zero test and the hot
// tail writes that counter only in its final unit decrement. Other GP work may
// read the counter; repeating the whole tail preserves those per-iteration reads.
func tupleLoopUnitCounter(prefix, tail []byte, pins regMask) (Reg, bool, bool) {
	counter, wide, _, ok := tupleLoopCounterTail(prefix, tail, pins)
	return counter, wide, ok
}

func tupleLoopCounterTail(prefix, tail []byte, pins regMask) (Reg, bool, []byte, bool) {
	counter, wide, ok := scalarLoopZeroCounter(prefix)
	if !ok || !pins.has(counter) {
		return regNone, false, nil, false
	}
	// A complete SUB-immediate occupies at most seven bytes. A candidate
	// boundary must also finish decoding every preceding GP instruction.
	for p := max(0, len(tail)-7); p < len(tail); p++ {
		if !scalarLoopUnitDecrement(tail[p:], counter, wide) {
			continue
		}
		if p != 0 {
			writes, ok := scalarLoopBodyWrites(tail[:p], pins)
			if !ok || writes.has(counter) {
				continue
			}
		}
		return counter, wide, tail[:p], true
	}
	return regNone, false, nil, false
}

// The original scalar graph handles entry and all remaining counts below four.
// With counter >= 4, four unit decrements cannot reach zero before the fourth
// tail completes, so the pure GP work needs just one check per group. The end
// check branches directly to the next group; no extra group-backedge JMP is used.
func (f *fn) tryScalarTupleCountedLoop(prefix, tail []byte, hotCondition Cond, scalar int) bool {
	if hotCondition != condNE {
		return false
	}
	counter, wide, body, ok := tupleLoopCounterTail(prefix, tail, f.pinnedLocalMask)
	if !ok {
		return false
	}
	// A body that never reads the counter sees the same values when its four
	// unit decrements become one SUB 4. Keep every original decrement whenever
	// an arithmetic operand or a LEA address observes the per-iteration value.
	reads, _, decoded := scalarLoopBodyRegisters(body, f.pinnedLocalMask)
	coalesce := len(body) == 0 || decoded && !reads.has(counter)
	copyTail := tail
	if coalesce {
		copyTail = body
	}
	f.a.AluRI(7, counter, 4, wide)
	remainder := f.a.JccPlaceholder(condB)
	f.a.PatchRel32(remainder, scalar)
	// The additional native loop inherits the source loop's fetch alignment.
	// This padding is on group entry, outside the repeated hot tail.
	if f.compactLoopAlign32 {
		f.a.AlignLoop32()
	} else {
		f.a.AlignLoop()
	}
	group := f.a.Len()
	pair, pairOK := planScalarSumSwapGroup(body, f.pinnedLocalMask)
	if coalesce && pairOK {
		f.emitScalarSumSwapGroup(pair)
	} else {
		for phase := 0; phase < 4; phase++ {
			f.a.B = append(f.a.B, copyTail...)
		}
	}
	if coalesce {
		f.a.AluRI(5, counter, 4, wide)
		f.stats.peep("scalar-tuple-counted-counter-group")
	}
	f.a.AluRI(7, counter, 4, wide)
	back := f.a.JccPlaceholder(condAE)
	f.a.PatchRel32(back, group)
	f.a.JmpBack(scalar)
	f.tryTupleCountedInitialGroup(counter, wide, scalar, group)
	f.stats.peep("scalar-tuple-counted-unroll4")
	return true
}
