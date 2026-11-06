//go:build amd64

package amd64

import "encoding/binary"

// Decode one bounded, read-only register CMP/TEST at a known instruction
// boundary. Memory operands and unknown prefixes retain ordinary loop entry.
// zeroReg identifies only a plain register-zero test, for exact ZF reuse.
func selfTailEntryCompare(code []byte) (size int, zeroReg Reg, wide, ok bool) {
	p, rex := 0, byte(0)
	if len(code) != 0 && code[0]&0xf0 == 0x40 {
		rex, p = code[0], 1
	}
	if len(code) < p+2 {
		return 0, regNone, false, false
	}
	op, modrm := code[p], code[p+1]
	if modrm&0xc0 != 0xc0 {
		return 0, regNone, false, false
	}
	wide = rex&8 != 0
	rm := Reg(modrm&7 | (rex&1)<<3)
	reg := Reg(modrm>>3&7 | (rex>>2&1)<<3)
	zeroReg = regNone
	switch op {
	case 0x85, 0x39, 0x3b: // TEST r,r; CMP r,r
		if op == 0x85 && rm == reg {
			zeroReg = rm
		}
		return p + 2, zeroReg, wide, true
	case 0x81, 0x83: // CMP r,imm
		if modrm&0x38 != 0x38 {
			return 0, regNone, false, false
		}
		n := 4
		if op == 0x83 {
			n = 1
		}
		if len(code) < p+2+n {
			return 0, regNone, false, false
		}
		zero := true
		for _, b := range code[p+2 : p+2+n] {
			zero = zero && b == 0
		}
		if zero {
			zeroReg = rm
		}
		return p + 2 + n, zeroReg, wide, true
	}
	return 0, regNone, false, false
}

// The complete last-assignment range must be one ADD/SUB-immediate writing
// the tested register at the same width. Never match a byte suffix: a MOV's
// immediate could contain bytes resembling an arithmetic instruction.
func selfTailAssignmentSetsZero(code []byte, reg Reg, wide bool) bool {
	if reg == regNone {
		return false
	}
	p, rex := 0, byte(0)
	if len(code) != 0 && code[0]&0xf0 == 0x40 {
		rex, p = code[0], 1
	}
	if len(code) < p+3 || (rex&8 != 0) != wide {
		return false
	}
	op, modrm := code[p], code[p+1]
	if modrm&0xc0 != 0xc0 || Reg(modrm&7|(rex&1)<<3) != reg {
		return false
	}
	group := modrm >> 3 & 7
	if group != 0 && group != 5 {
		return false
	}
	if op == 0x83 {
		return len(code) == p+3
	}
	return op == 0x81 && len(code) == p+6
}

func (f *fn) threadSelfTailEntryTest(lastAssignment int) bool {
	if f.nLocals != f.nParams {
		return false
	}
	return f.threadSelfTailEntryTestAt(f.selfTailEntry, lastAssignment)
}

func (f *fn) threadSelfTailEntryTestAt(entry, lastAssignment int) bool {
	if !f.opt(optBranchFold) {
		return false
	}
	end := f.a.Len()
	if entry < 0 || entry >= end {
		return false
	}
	size, zeroReg, wide, ok := selfTailEntryCompare(f.a.B[entry:])
	branch := entry + size
	if !ok || branch+6 > end || f.a.B[branch] != 0x0f || f.a.B[branch+1]&0xf0 != 0x80 {
		return false
	}
	cc := Cond(f.a.B[branch+1] & 15)
	cold := branch + 6
	hot := cold + int(int32(binary.LittleEndian.Uint32(f.a.B[branch+2:branch+6])))
	if hot <= cold || hot >= end {
		return false
	}
	reuse := false
	if (cc == condE || cc == condNE) && lastAssignment >= hot && lastAssignment < end {
		reuse = selfTailAssignmentSetsZero(f.a.B[lastAssignment:end], zeroReg, wide)
	}
	if reuse {
		f.stats.peep("tail-self-test-flags")
	} else {
		f.a.EmitBytes(f.a.B[entry:branch])
	}
	f.a.PatchRel32(f.a.JccPlaceholder(cc), hot)
	f.a.PatchRel32(f.a.JmpPlaceholder(), cold)
	f.stats.peep("tail-self-entry-test")
	return true
}
