//go:build arm64

package arm64

import (
	"encoding/binary"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"os"
)

var remainderPairLatchEnabled = os.Getenv("WAGO_ARM64_NO_REMAINDER_PAIR_LATCH") != "1"

// Alternate A/B roles for two unsigned remainder iterations, restoring the
// canonical homes on the odd exit. The initial divisor check retains its Wasm
// trap identity; subsequent divisors are proved nonzero by the preceding exit.
func (f *fn) pairRemainderLatch(fr *ctrlFrame, condition Reg, end int, after *wasm.Reader) bool {
	if !remainderPairLatchEnabled || !f.opt(optScalarLoopUnroll) || profileEnabled || f.interruptible || f.moduleEH || f.gcFrameRoots != nil || len(f.customInstructions) != 0 || f.policy.CompactNative || f.a.ReserveBranch19 || f.opaqueFragments || f.preserveCallerPins || f.makesCalls || f.localBase != 0 || f.inlineForwardN != 0 || f.stats != nil && f.stats.RecordSources || f.sc == nil || after == nil || len(f.ctrl) == 0 || fr != &f.ctrl[len(f.ctrl)-1] || fr.kind != cfLoop || fr.paramN != 0 || fr.resultN != 0 || fr.height != 0 || f.depth() != 0 || f.nLocals > 64 || f.a.Len() != end+4 || !f.remainderForwardExit(fr, condition, end) {
		return false
	}
	next := *after
	op, err := next.Byte()
	if err != nil || op != 0x0b {
		return false
	}
	start := fr.controlSite
	if rdWord(f.a.B, end) != 0x7100001f|uint32(condition)<<5 {
		return false
	}
	first, second, divide := rdWord(f.a.B, start), rdWord(f.a.B, start+4), rdWord(f.a.B, start+12)
	a, b, s, q := Reg(first>>16&31), Reg(second>>16&31), Reg(first&31), Reg(divide&31)
	if q == a || q == b || q == s || q >= 31 {
		return false
	}
	for _, r := range []Reg{s, q} {
		if f.pinnedLocalMask.has(r) || f.pinned.has(r) || f.reserved.has(r) || f.regUser[r] != nil {
			return false
		}
		found := false
		for _, candidate := range gpAlloc {
			found = found || candidate == r
		}
		if !found {
			return false
		}
	}
	budget := f.newScalarLoopMetadataBudget()
	if !budget.take(f.nLocals) {
		return false
	}
	countA, countB := 0, 0
	for x := 0; x < f.nLocals; x++ {
		reg, floating, pinned := f.pinReg(x)
		if !pinned || floating || f.localType[x] != mtI32 {
			continue
		}
		if reg == a {
			countA++
		}
		if reg == b {
			countB++
		}
	}
	if countA != 1 || countB != 1 || !f.pinnedLocalMask.has(a) || !f.pinnedLocalMask.has(b) {
		return false
	}
	guard := start + 8
	trapIndex := -1
	for code, sites := range f.sc.trapSites {
		if !budget.take(len(sites)) {
			return false
		}
		for i, site := range sites {
			at := int(site.branch &^ 1)
			if at < start || at >= end+4 {
				continue
			}
			if uint32(code) != trapDivZero || at != guard || site.branch&1 != 0 || trapIndex >= 0 {
				return false
			}
			trapIndex = i
		}
	}
	if trapIndex < 0 || rdWord(f.a.B, guard)&0x00ffffe0 != 0 || !budget.take(len(f.relocs)) {
		return false
	}
	for _, reloc := range f.relocs {
		if int(reloc.at) >= start && int(reloc.at) < end+4 {
			return false
		}
	}
	for head := f.sc.retSiteHead; head != 0; {
		if !budget.take(1) {
			return false
		}
		at := int(head-1) * 4
		if at < 0 || at+4 > len(f.a.B) || at >= start {
			return false
		}
		head = rdWord(f.a.B, at) & 0x03ffffff
	}
	protected := func(packed uint32) bool {
		if packed == 0 {
			return false
		}
		at := int(packed&^frameEndConditional) - 1
		return at >= start && at < end+4
	}
	for i := range f.ctrl {
		owner := &f.ctrl[i]
		first, second, extra := f.frameEndSites(owner)
		if !budget.take(2+len(extra)) || protected(first) || protected(second) {
			return false
		}
		for _, site := range extra {
			if protected(site) {
				return false
			}
		}
		if merge := f.ctrlMerge(owner); merge != nil {
			if !budget.take(len(merge.coldEdges)) {
				return false
			}
			for _, edge := range merge.coldEdges {
				if edge.site >= start && edge.site < end+4 {
					return false
				}
			}
		}
	}
	// No code or metadata changes occur until the complete proof succeeds.
	f.a.B = f.a.B[:start]
	appendWord := func(w uint32) { f.a.B = binary.LittleEndian.AppendUint32(f.a.B, w) }
	appendWord(0x34000000 | uint32(b))
	loop := f.a.Len()
	emitRem := func(left, right Reg) {
		appendWord(0x1ac00800 | uint32(right)<<16 | uint32(left)<<5 | uint32(q))
		appendWord(0x1b008000 | uint32(right)<<16 | uint32(left)<<10 | uint32(q)<<5 | uint32(left))
	}
	emitRem(a, b)
	odd := f.a.Len()
	appendWord(0x34000000 | uint32(a))
	emitRem(b, a)
	back := f.a.Len()
	appendWord(0x35000000 | uint32(b))
	f.patchBranch19(back, loop)
	done := f.a.Branch()
	f.patchBranch19(odd, f.a.Len())
	appendWord(0xaa0003e0 | uint32(b)<<16 | uint32(a))
	appendWord(0xaa1f03e0 | uint32(b))
	f.patchBranch26(done, f.a.Len())
	f.sc.trapSites[trapDivZero][trapIndex].branch = uint32(start)
	fr.controlSite = loop
	f.loopSeedSite = 0
	// Three extra hot words plus thirteen cold words preserve a64-byte phase.
	f.phasePadWords += 13
	f.stats.peep("remainder-pair-latch")
	f.stats.peep("remainder-forward-exit")
	f.stats.peep("normal-zero-branch")
	f.stats.peep("zero-branch")
	return true
}
