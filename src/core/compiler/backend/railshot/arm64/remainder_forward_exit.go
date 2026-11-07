//go:build arm64

package arm64

import "os"

var remainderForwardExitEnabled = os.Getenv("WAGO_ARM64_NO_REMAINDER_FORWARD_EXIT") != "1"

// Select a forward conditional exit for a short unsigned-remainder recurrence.
// Only the latch changes shape; all arithmetic, local assignments, and the
// divisor-zero guard remain at their original positions.
func (f *fn) remainderForwardExit(fr *ctrlFrame, condition Reg, end int) bool {
	if !remainderForwardExitEnabled || fr.controlSite < 0 || end-fr.controlSite != 20 || end > len(f.a.B) || condition >= 31 {
		return false
	}
	start := fr.controlSite
	first, second := rdWord(f.a.B, start), rdWord(f.a.B, start+4)
	a, b, s := Reg(first>>16&31), Reg(second>>16&31), Reg(first&31)
	if a == b || a == s || b == s || a >= 31 || b >= 31 || s >= 31 || condition != b || first != 0xaa0003e0|uint32(a)<<16|uint32(s) || second != 0xaa0003e0|uint32(b)<<16|uint32(a) {
		return false
	}
	guard := rdWord(f.a.B, start+8)
	if guard&0xff00001f != 0x34000000|uint32(a) {
		return false
	}
	divide := rdWord(f.a.B, start+12)
	q := Reg(divide & 31)
	return divide == 0x1ac00800|uint32(a)<<16|uint32(s)<<5|uint32(q) &&
		rdWord(f.a.B, start+16) == 0x1b008000|uint32(a)<<16|uint32(s)<<10|uint32(q)<<5|uint32(b)
}
