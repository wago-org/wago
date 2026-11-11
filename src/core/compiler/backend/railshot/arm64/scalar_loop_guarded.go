//go:build arm64

package arm64

import "encoding/binary"

// unrollGuardedScalarLatch omits intermediate exits only when four increments
// fit below an invariant unsigned bound without wrapping. The scalar tail keeps
// the original do-while behavior, including an initially out-of-range counter.
// The caller has already proved every word position independent and nontrapping,
// a unique unit increment, and a temporary with no live register owner.
func (f *fn) unrollGuardedScalarLatch(start int, words []uint32, increment int, counter, temporary Reg, cc Cond) bool {
	last := words[len(words)-1]
	if last&0x7fe0fc1f != 0x6b00001f || (last^words[increment])>>31 != 0 {
		return false
	}
	rn, rm := Reg(last>>5&31), Reg(last>>16&31)
	bound := regNone
	if cc == condB && rn == counter {
		bound = rm
	} else if cc == condA && rm == counter {
		bound = rn
	}
	if bound == regNone || bound == counter || bound == 31 {
		return false
	}
	flags := false
	for _, word := range words[:len(words)-1] {
		rd, ok := scalarLoopWord(word)
		if !ok || rd == bound {
			return false
		}
		if word&0x7fe00c00 == 0x1a800000 && !flags {
			// The new guard changes NZCV. A CSEL must get all its flags from
			// this iteration, never from the old latch or an enclosing block.
			return false
		}
		switch {
		case word&0x1f800000 == 0x11000000, word&0x1f200000 == 0x0b000000:
			flags = flags || word&(1<<29) != 0
		case word&0x1f800000 == 0x12000000, word&0x1f000000 == 0x0a000000:
			flags = flags || word&0x60000000 == 0x60000000
		}
	}

	group := f.planScalarGroupSum(words, counter, temporary)
	f.truncateNativeCode(start)
	sf := last & (1 << 31)
	// SUBS temporary,bound,counter rejects subtraction borrow before testing
	// the distance. Therefore distance>=4 proves all four additions stay in range.
	f.a.B = binary.LittleEndian.AppendUint32(f.a.B, 0x6b000000|sf|uint32(counter)<<16|uint32(bound)<<5|uint32(temporary))
	borrow := f.a.Bcond(condB)
	if sf == 0 {
		f.a.CmpImm32(temporary, 4)
	} else {
		f.a.CmpImm64(temporary, 4)
	}
	short := f.a.Bcond(condB)
	for phase := 0; phase < 4; phase++ {
		current := counter
		if phase != 0 {
			current = temporary
		}
		for i, word := range words[:len(words)-1] {
			if i == increment {
				current = temporary
				if phase == 3 {
					current = counter
				}
				word = word&^uint32(0xfff<<10|31) | uint32(phase+1)<<10 | uint32(current)
			} else {
				word = renameScalarLoopReads(word, counter, current)
			}
			if i == group.index {
				word = group.word(word, phase)
			}
			f.a.B = binary.LittleEndian.AppendUint32(f.a.B, word)
		}
	}
	if group.index >= 0 {
		f.a.B = binary.LittleEndian.AppendUint32(f.a.B, 0x0b000000|group.width|uint32(group.scratch)<<16|uint32(group.accumulator)<<5|uint32(group.accumulator))
		f.stats.peep("scalar-loop-group-sum")
	}
	f.a.B = binary.LittleEndian.AppendUint32(f.a.B, last)
	f.patchBranch19(f.a.Bcond(cc), start)
	done := f.a.Branch()
	tail := f.a.Len()
	f.patchBranch19(borrow, tail)
	f.patchBranch19(short, tail)
	for _, word := range words {
		f.a.B = binary.LittleEndian.AppendUint32(f.a.B, word)
	}
	f.patchBranch19(f.a.Bcond(cc), tail)
	f.patchBranch26(done, f.a.Len())
	f.stats.peep("scalar-loop-unroll-guarded")
	return true
}
