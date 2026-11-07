//go:build arm64

package arm64

import "os"

var scalarGroupSumEnabled = os.Getenv("WAGO_ARM64_NO_SCALAR_GROUP_SUM") != "1"

type scalarGroupSum struct {
	index                int
	accumulator, scratch Reg
	width                uint32
}

// The guarded unroller has already proved four nontrapping, side-effect-free
// iterations. Reassociate one wrapping integer sum only when its old value is
// read by exactly its single in-place ADD and never by the term computation,
// index update, or latch. The original scalar tail remains unchanged.
func (f *fn) planScalarGroupSum(words []uint32, counter, temporary Reg) scalarGroupSum {
	no := scalarGroupSum{index: -1}
	if !scalarGroupSumEnabled {
		return no
	}
	used := f.reserved.union(f.pinned).union(f.pinnedLocalMask).add(temporary)
	for _, w := range words {
		rd, ok := scalarLoopWord(w)
		if !ok {
			return no
		}
		if rd != regNone {
			used = used.add(rd)
		}
		fields := scalarLoopSources(w)
		for _, shift := range []uint{5, 10, 16} {
			if fields&(1<<shift) != 0 {
				used = used.add(Reg(w >> shift & 31))
			}
		}
	}
	scratch := regNone
	for _, r := range gpAlloc {
		if !used.has(r) && f.regUser[r] == nil {
			scratch = r
			break
		}
	}
	if scratch == regNone {
		return no
	}
	for i, w := range words {
		if w&0x7fe0fc00 != 0x0b000000 {
			continue
		}
		rd, rn, rm := Reg(w&31), Reg(w>>5&31), Reg(w>>16&31)
		if rd == counter || rd == 31 || !f.pinnedLocalMask.has(rd) || !(rd == rn && rd != rm || rd == rm && rd != rn) {
			continue
		}
		term := rm
		if rd == rm {
			term = rn
		}
		if term == 31 {
			continue
		}
		valid := true
		for j, v := range words {
			if i == j {
				continue
			}
			dest, _ := scalarLoopWord(v)
			if dest == rd {
				valid = false
				break
			}
			fields := scalarLoopSources(v)
			for _, shift := range []uint{5, 10, 16} {
				if fields&(1<<shift) != 0 && Reg(v>>shift&31) == rd {
					valid = false
				}
			}
			if !valid {
				break
			}
		}
		if valid {
			return scalarGroupSum{index: i, accumulator: rd, scratch: scratch, width: w & (1 << 31)}
		}
	}
	return no
}

func (p scalarGroupSum) word(original uint32, phase int) uint32 {
	rn, rm := Reg(original>>5&31), Reg(original>>16&31)
	term := rm
	if term == p.accumulator {
		term = rn
	}
	if phase == 0 {
		return 0x2a0003e0 | p.width | uint32(term)<<16 | uint32(p.scratch)
	}
	return 0x0b000000 | p.width | uint32(term)<<16 | uint32(p.scratch)<<5 | uint32(p.scratch)
}
