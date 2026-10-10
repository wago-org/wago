//go:build arm64

package arm64

import "os"

var commonExitCompareEnabled = os.Getenv("WAGO_ARM64_NO_COMMON_EXIT_COMPARE") != "1" &&
	os.Getenv("WAGO_ARM64_EXPERIMENT_COMMON_EXIT_COMPARE") != "0"

// CMP; B.c1 exit; CMP; B.c2 exit -> CMP; CCMP(!c1); NOP; B.c2 exit.
// The forced NZCV satisfies c2 when c1 originally took the exit. Both
// continuations must overwrite NZCV before any use. No GPR or layout changes.
func (f *fn) foldCommonExitCompares(b []byte, n int, targets []uint64, entryOffsets ...int) {
	if !f.opt(optCommonExitCompare) || f.opaqueFragments || f.moduleEH || len(f.customInstructions) != 0 {
		return
	}
	for pc := 0; pc+16 <= n; pc += 4 {
		first := rdWord(b, pc)
		if !pureCompareWord(first) {
			continue
		}
		branch1 := rdWord(b, pc+4)
		if branch1&0xFF000010 != 0x54000000 {
			continue
		}
		second, branch2 := rdWord(b, pc+8), rdWord(b, pc+12)
		if branch2&0xFF000010 != 0x54000000 {
			continue
		}
		c1, c2 := Cond(branch1&15), Cond(branch2&15)
		if c1 >= 14 || c2 >= 14 {
			continue
		}
		dest1, ok1 := branchTarget(pc+4, branch1)
		dest2, ok2 := branchTarget(pc+12, branch2)
		if !ok1 || !ok2 || dest1 != dest2 || !deadFlagsPath(b, pc+16, n) || !deadFlagsPath(b, dest1, n) {
			continue
		}
		if branchTargeted(targets, pc+4) || branchTargeted(targets, pc+8) || branchTargeted(targets, pc+12) {
			continue
		}
		entry := false
		for _, off := range entryOffsets {
			if off >= pc+4 && off <= pc+12 {
				entry = true
				break
			}
		}
		if entry {
			continue
		}
		var fallback uint32
		for fallback = 0; fallback < 16; fallback++ {
			if compareCondHolds(c2, fallback) {
				break
			}
		}
		conditional, ok := conditionalCompareWord(second, invertCond(c1), fallback)
		if !ok {
			continue
		}
		wrWord(b, pc+4, conditional)
		wrWord(b, pc+8, nopWord)
		f.stats.peep("common-exit-compare")
		pc += 12
	}
}

func pureCompareWord(w uint32) bool {
	reg := w & 0x7FE0FC1F
	imm := w & 0x7F00001F
	return reg == 0x6B00001F || reg == 0x2B00001F || imm == 0x7100001F || imm == 0x3100001F
}

func conditionalCompareWord(w uint32, condition Cond, nzcv uint32) (uint32, bool) {
	wide := w & 0x80000000
	rn := w >> 5 & 31
	reg := w & 0x7FE0FC1F
	if reg == 0x6B00001F || reg == 0x2B00001F {
		base := uint32(0x7A400000)
		if reg == 0x2B00001F {
			base = 0x3A400000
		}
		return wide | base | (w & 0x001F0000) | uint32(condition)<<12 | rn<<5 | nzcv, true
	}
	imm := w & 0x7F00001F
	if (imm != 0x7100001F && imm != 0x3100001F) || rn == 31 {
		return 0, false
	}
	value := w >> 10 & 0xfff
	if w&(1<<22) != 0 {
		value <<= 12
	}
	if value > 31 {
		return 0, false
	}
	base := uint32(0x7A400800)
	if imm == 0x3100001F {
		base = 0x3A400800
	}
	return wide | base | value<<16 | uint32(condition)<<12 | rn<<5 | nzcv, true
}

func compareCondHolds(c Cond, flags uint32) bool {
	n, z, carry, v := flags&8 != 0, flags&4 != 0, flags&2 != 0, flags&1 != 0
	switch c {
	case 0:
		return z
	case 1:
		return !z
	case 2:
		return carry
	case 3:
		return !carry
	case 4:
		return n
	case 5:
		return !n
	case 6:
		return v
	case 7:
		return !v
	case 8:
		return carry && !z
	case 9:
		return !carry || z
	case 10:
		return n == v
	case 11:
		return n != v
	case 12:
		return !z && n == v
	case 13:
		return z || n != v
	}
	return false
}
