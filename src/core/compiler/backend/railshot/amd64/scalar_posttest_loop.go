//go:build amd64

package amd64

var scalarLoopUnrollEnabled = true

// scalarLoopBody admits only position-independent integer register operations.
// This deliberately small decoder recognizes complete encoder forms, including
// their immediates, so an immediate byte can never masquerade as an instruction.
// Memory accesses, branches, calls, traps and all unrecognized prefixes fall back.
func scalarLoopBody(b []byte) bool { return scalarLoopBodyWithPins(b, 0) }

func scalarLoopBodyWithPins(b []byte, pins regMask) bool {
	_, ok := scalarLoopBodyWrites(b, pins)
	return ok
}

func scalarLoopBodyWrites(b []byte, pins regMask) (regMask, bool) {
	_, writes, ok := scalarLoopBodyRegisters(b, pins)
	return writes, ok
}

// Register reads include every LEA base/index and both operands of destructive
// arithmetic. None of the admitted instructions consumes incoming flags.
func scalarLoopBodyRegisters(b []byte, pins regMask) (regMask, regMask, bool) {
	reads, writes, _, ok := scalarLoopBodyEffects(b, pins)
	return reads, writes, ok
}

func scalarLoopBodyEffects(b []byte, pins regMask) (regMask, regMask, bool, bool) {
	definesFlags := false
	var reads, writes regMask
	if len(b) == 0 || len(b) > 48 {
		return 0, 0, false, false
	}
	for p := 0; p < len(b); {
		rex := byte(0)
		if b[p]&0xf0 == 0x40 {
			rex = b[p]
			p++
			if p == len(b) {
				return 0, 0, false, false
			}
		}
		op := b[p]
		p++
		if p == len(b) {
			return 0, 0, false, false
		}
		m := b[p]
		p++
		mod, r, rm := m>>6, Reg((m>>3)&7)|Reg((rex>>2)&1)<<3, Reg(m&7)|Reg(rex&1)<<3
		dst := rm
		imm := 0
		switch op {
		case 0x01, 0x09, 0x21, 0x29, 0x31, 0x89: // register destination r/m
			if mod != 3 {
				return 0, 0, false, false
			}
			reads = reads.add(r)
			if op != 0x89 {
				reads = reads.add(rm)
			}
		case 0x03, 0x0b, 0x23, 0x2b, 0x33, 0x8b: // register destination reg
			if mod != 3 {
				return 0, 0, false, false
			}
			dst = r
			reads = reads.add(rm)
			if op != 0x8b {
				reads = reads.add(r)
			}
		case 0x39, 0x3b, 0x85: // CMP / TEST
			if mod != 3 {
				return 0, 0, false, false
			}
			dst = regNone
			reads = reads.add(r).add(rm)
		case 0x81, 0x83: // integer immediate ALU, excluding ADC/SBB
			if mod != 3 || (m>>3)&7 == 2 || (m>>3)&7 == 3 {
				return 0, 0, false, false
			}
			reads = reads.add(rm)
			if (m>>3)&7 == 7 {
				dst = regNone
			}
			imm = 4
			if op == 0x83 {
				imm = 1
			}
		case 0x8d: // LEA has an address encoding but never accesses memory.
			if mod == 3 || mod == 0 && m&7 == 5 {
				return 0, 0, false, false
			}
			dst = r
			base := rm
			if m&7 == 4 {
				if p == len(b) {
					return 0, 0, false, false
				}
				sib := b[p]
				p++
				if mod == 0 && sib&7 == 5 {
					return 0, 0, false, false
				}
				base = Reg(sib&7) | Reg(rex&1)<<3
				index := Reg((sib>>3)&7) | Reg((rex>>1)&1)<<3
				if index != RSP {
					reads = reads.add(index)
				}
			}
			reads = reads.add(base)
			if mod == 1 {
				imm = 1
			}
			if mod == 2 {
				imm = 4
			}
		default:
			return 0, 0, false, false
		}
		if dst == RSP || dst == RBP && !pins.has(RBP) || len(b)-p < imm {
			return 0, 0, false, false
		}
		if dst != regNone {
			writes = writes.add(dst)
		}
		if op != 0x89 && op != 0x8b && op != 0x8d {
			definesFlags = true
		}
		p += imm
	}
	return reads, writes, definesFlags, true
}

// tryScalarPosttestLoop keeps every original iteration's exit test, with three
// forward exits and one backedge per group. No induction/bound-distance proof is
// needed: zero bounds, signed comparisons and modular integer wrap keep their
// original behavior. The bounded pure-register body has no relocation inventory.
func (f *fn) tryScalarPosttestLoop(fr *ctrlFrame, fi, depth int, cc Cond) bool {
	if !f.opt(optScalarLoopUnroll) || f.interruptible || profileEnabled ||
		f.stats != nil && f.stats.RecordSources || f.moduleEH || f.gcFrameRoots != nil ||
		f.policy.CompactNative || len(f.customInstructions) != 0 ||
		fi != len(f.ctrl)-1 || fr.kind != cfLoop || fr.paramN != 0 || fr.resultN != 0 || fr.height != 0 || depth != 0 ||
		fr.controlSite < 0 || fr.controlSite >= f.a.Len() {
		return false
	}
	body := f.a.B[fr.controlSite:]
	if !scalarLoopBody(body) {
		return false
	}
	var saved [48]byte
	n := copy(saved[:], body)
	var exits [3]int
	for i := range exits {
		exits[i] = f.a.JccPlaceholder(invertCond(cc))
		f.a.B = append(f.a.B, saved[:n]...)
	}
	back := f.a.JccPlaceholder(cc)
	f.a.PatchRel32(back, fr.controlSite)
	for _, exit := range exits {
		f.a.PatchRel32(exit, f.a.Len())
	}
	f.stats.peep("scalar-posttest-unroll4")
	return true
}
