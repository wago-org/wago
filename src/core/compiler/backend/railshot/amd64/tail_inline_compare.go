//go:build amd64

package amd64

import (
	"bytes"
	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
)

// The false edge must be the sole edge reaching this if's end, and must land
// directly at the next compare. The true edge exits the inline invocation.
func (f *fn) rememberInlineTailCompare(fr *ctrlFrame, resume int) {
	c := f.inlineTailConstants
	if c == nil || !f.opt(optBranchFold) || fr.resultN != 0 || fr.has(ctrlEndReachable) || resume != f.a.Len() {
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
	c := f.inlineTailConstants
	if c == nil {
		return false
	}
	resume, start, end := c.reuseResume, c.reuseStart, c.reuseEnd
	c.reuseResume = -1
	if !f.opt(optBranchFold) || resume != f.a.Len() || start < 0 || end <= start || end > len(f.a.B) {
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
	if !bytes.Equal(probe.B, f.a.B[start:end]) {
		return false
	}
	c.lastCompare = -1
	f.stats.peep("inline-tail-compare-reuse")
	return true
}
