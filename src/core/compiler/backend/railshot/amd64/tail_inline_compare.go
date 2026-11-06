//go:build amd64

package amd64

import (
	"bytes"
	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
)

// Compare facts are independent of entry-copy constant propagation: the
// remaining dynamic tail loop can reuse flags too. Every structured boundary
// invalidates a pending false-edge fact, including boundaries emitting no code.
type inlineTailCompare struct {
	active                                         bool
	lastCompare, reuseStart, reuseEnd, reuseResume int
}

// The false edge must be the sole edge reaching this if's end, and must land
// at a bounded flag-preserving span before the next compare. The true edge
// cannot reach this if's end.
func (f *fn) rememberInlineTailCompare(fr *ctrlFrame, resume int) {
	c := &f.inlineTailCompare
	if !c.active || !f.opt(optBranchFold) || fr.resultN != 0 || fr.has(ctrlEndReachable) || resume != f.a.Len() {
		return
	}
	start := c.lastCompare
	if start < 0 || start >= len(f.a.B) {
		return
	}
	size, _, _, ok := selfTailEntryCompare(f.a.B[start:])
	if !ok || start+size+2 != fr.controlSite || start+size+6 > len(f.a.B) || f.a.B[start+size] != 0x0f || f.a.B[start+size+1]&0xf0 != 0x80 {
		return
	}
	c.reuseStart, c.reuseEnd, c.reuseResume = start, start+size, resume
}

func (f *fn) tryReuseInlineTailCompare(left Reg, right *elem, wide bool) bool {
	c := &f.inlineTailCompare
	if !c.active {
		return false
	}
	resume, start, end := c.reuseResume, c.reuseStart, c.reuseEnd
	c.reuseResume = -1
	if !f.opt(optBranchFold) || resume < 0 || resume > f.a.Len() || start < 0 || end <= start || end > len(f.a.B) {
		return false
	}
	// Encode only the contemplated CMP into bounded scratch. No instruction or
	// observer event is emitted into the function until reuse has been decided.
	var storage [16]byte
	probe := encoder.Asm{B: storage[:0]}
	switch right.st.kind {
	case stConst:
		if !fitsImm32(right.st.cval) {
			return false
		}
		probe.AluRI(cmpDigit, left, int32(right.st.cval), wide)
	case stReg, stLocalReg, stGlobReg:
		if wide {
			probe.Cmp64(left, right.st.reg)
		} else {
			probe.Cmp32(left, right.st.reg)
		}
	default:
		return false
	}
	if !bytes.Equal(probe.B, f.a.B[start:end]) || !inlineTailComparePreserved(f.a.B[resume:], inlineTailCompareOperands(probe.B)) {
		return false
	}
	c.lastCompare = -1
	f.stats.peep("inline-tail-compare-reuse")
	return true
}

// The caller has already validated this register CMP with selfTailEntryCompare.
func inlineTailCompareOperands(b []byte) regMask {
	p, rex := 0, byte(0)
	if b[0]&0xf0 == 0x40 {
		rex, p = b[0], 1
	}
	op, modrm := b[p], b[p+1]
	operands := maskOf(Reg(modrm&7 | (rex&1)<<3))
	if op == 0x39 || op == 0x3b || op == 0x85 {
		operands = operands.add(Reg(modrm>>3&7 | (rex>>2&1)<<3))
	}
	return operands
}

// Admit only complete register MOVs and LEAs. Neither changes flags or reads
// memory. Bound the entire range and reject every write to a CMP operand,
// including 32-bit writes that clear its upper half. Unknown forms fall back.
func inlineTailComparePreserved(b []byte, operands regMask) bool {
	if len(b) > 16 {
		return false
	}
	for len(b) != 0 {
		p, rex := 0, byte(0)
		if b[0]&0xf0 == 0x40 {
			rex, p = b[0], 1
		}
		if len(b) < p+2 {
			return false
		}
		op, modrm := b[p], b[p+1]
		p += 2
		mod, rm := modrm>>6, modrm&7
		dest := Reg(modrm>>3&7 | (rex>>2&1)<<3)
		switch op {
		case 0x89, 0x8b:
			if mod != 3 {
				return false
			}
			if op == 0x89 {
				dest = Reg(rm | (rex&1)<<3)
			}
		case 0x8d:
			if mod == 3 {
				return false
			}
			base := rm
			if rm == 4 {
				if len(b) <= p {
					return false
				}
				base = b[p] & 7
				p++
			}
			if mod == 1 {
				p++
			} else if mod == 2 || mod == 0 && base == 5 {
				p += 4
			}
		default:
			return false
		}
		if p > len(b) || operands.has(dest) {
			return false
		}
		b = b[p:]
	}
	return true
}
