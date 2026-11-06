//go:build arm64

package arm64

import "encoding/binary"

var scalarLoopUnrollEnabled = true

// scalarLoopWord proves a position-independent integer instruction with no
// memory or control effect. It returns the destination, regNone for flag-only/NOP.
func scalarLoopWord(word uint32) (Reg, bool) {
	if word == 0xd503201f {
		return regNone, true
	}
	rd, rn := Reg(word&31), Reg(word>>5&31)
	switch {
	case word&0x1f800000 == 0x11000000: // ADD/SUB immediate;31 can mean SP
		if rn == 31 || rd == 31 && word&(1<<29) == 0 {
			return regNone, false
		}
	case word&0x1f200000 == 0x0b000000: // ADD/SUB shifted register
	case word&0x1f000000 == 0x0a000000: // logical shifted register, including MOV
	case word&0x1f800000 == 0x12800000: // MOVN/MOVZ/MOVK
	case word&0x7fe0fc00 == 0x1ac00800: // UDIV has no architectural trap; control checks are copied separately
	case word&0x1f000000 == 0x1b000000: // integer multiply/add/subtract
	default:
		return regNone, false
	}
	if rd == 31 {
		return regNone, true
	}
	return rd, true
}

// scalarLoopSources identifies only the register fields of a proved word.
// Immediate bits must never be rewritten as if they named registers.
func scalarLoopSources(word uint32) uint32 {
	switch {
	case word&0x1f800000 == 0x11000000:
		return 1 << 5
	case word&0x1f200000 == 0x0b000000, word&0x1f000000 == 0x0a000000, word&0x7fe0fc00 == 0x1ac00800:
		return 1<<5 | 1<<16
	case word&0x1f000000 == 0x1b000000:
		return 1<<5 | 1<<10 | 1<<16
	}
	return 0
}

func renameScalarLoopReads(word uint32, from, to Reg) uint32 {
	fields := scalarLoopSources(word)
	for _, shift := range []uint{5, 10, 16} {
		if fields&(1<<shift) != 0 && Reg(word>>shift&31) == from {
			word = word&^(31<<shift) | uint32(to)<<shift
		}
	}
	return word
}

// A four-iteration group retains an exit test after every iteration. A unique
// pinned induction variable advances once per group; intermediate indexes are
// computed from its original value, breaking the serial increment chain.
func (f *fn) unrollScalarLatch(fr *ctrlFrame, cc Cond) bool {
	if !f.opt(optScalarLoopUnroll) || !f.hasLoop || profileEnabled || f.interruptible || f.moduleEH || f.gcFrameRoots != nil || len(f.customInstructions) != 0 || f.policy.CompactNative || f.a.ReserveBranch19 || f.stats != nil && f.stats.RecordSources || fr.paramN != 0 || uint8(cc) >= 14 {
		return false
	}
	start, end := fr.controlSite, f.a.Len()
	if start < 0 || end-start < 8 || end-start > 32 || (end-start)&3 != 0 {
		return false
	}
	var words [8]uint32
	n := (end - start) / 4
	used := regMask(0)
	increment := -1
	counter := regNone
	for i := 0; i < n; i++ {
		word := binary.LittleEndian.Uint32(f.a.B[start+4*i:])
		words[i] = word
		rd, ok := scalarLoopWord(word)
		if !ok || rd != regNone && (f.reserved.has(rd) || rd == X18 || rd == X26 || rd == X28 || rd == Reg(29) || rd == Reg(30)) {
			return false
		}
		if rd != regNone {
			used = used.add(rd)
		}
		fields := scalarLoopSources(word)
		for _, shift := range []uint{5, 10, 16} {
			if fields&(1<<shift) != 0 {
				used = used.add(Reg(word >> shift & 31))
			}
		}
		// ADD{W,X} local,local,#1, without NZCV writes or a shifted immediate.
		if word&0x7fc00000 == 0x11000000 && word>>10&0xfff == 1 && rd == Reg(word>>5&31) && rd != regNone && f.pinnedLocalMask.has(rd) {
			if increment != -1 {
				return false
			}
			increment, counter = i, rd
		}
	}
	if increment < 0 || increment == n-1 {
		return false
	}
	for i := 0; i < n; i++ {
		rd, _ := scalarLoopWord(words[i])
		if i != increment && rd == counter {
			return false
		}
	}
	last := words[n-1]
	// The final flags come from a genuine comparison of the updated index.
	cmpReg := last&0x7f20001f == 0x6b00001f
	cmpImm := last&0x7f80001f == 0x7100001f
	if !cmpReg && !cmpImm || Reg(last>>5&31) != counter && (!cmpReg || Reg(last>>16&31) != counter) {
		return false
	}
	temporary := regNone
	blocked := used.union(f.reserved).union(f.pinned).union(f.pinnedLocalMask)
	for _, r := range gpAlloc {
		if !blocked.has(r) && f.regUser[r] == nil {
			temporary = r
			break
		}
	}
	if temporary == regNone {
		return false
	}
	// The first body is already emitted. Replace it only after every proof has
	// succeeded; a rejected candidate leaves the original instruction stream.
	f.a.B = f.a.B[:start]
	var exits [3]int
	for phase := 0; phase < 4; phase++ {
		current := counter
		if phase != 0 {
			current = temporary
		}
		for i := 0; i < n; i++ {
			word := words[i]
			if i == increment {
				current = temporary
				if phase == 3 {
					current = counter
				}
				word = word&^uint32(0xfff<<10|31) | uint32(phase+1)<<10 | uint32(current)
			} else {
				word = renameScalarLoopReads(word, counter, current)
			}
			f.a.B = binary.LittleEndian.AppendUint32(f.a.B, word)
		}
		if phase < 3 {
			exits[phase] = f.a.Bcond(invertCond(cc))
		} else {
			f.patchBranch19(f.a.Bcond(cc), start)
		}
	}
	complete := f.a.Branch()
	for _, exit := range exits {
		f.patchBranch19(exit, f.a.Len())
	}
	// All early exits carry their exact updated index in the free temporary.
	f.a.B = binary.LittleEndian.AppendUint32(f.a.B, 0xaa0003e0|uint32(temporary)<<16|uint32(counter))
	f.patchBranch26(complete, f.a.Len())
	f.stats.peep("scalar-loop-unroll")
	return true
}
