//go:build arm64

package arm64

import (
	"os"
)

// branchFoldEnabled gates the post-assembly double-branch peephole. On by
// default; WAGO_ARM64_NOBRFOLD=1 disables it for A/B measurement.
var branchFoldEnabled = os.Getenv("WAGO_ARM64_NOBRFOLD") != "1"

// storeLoadFwdEnabled gates the adjacent store→load forwarding peephole. On by
// default; WAGO_ARM64_NOSTLDFWD=1 disables it for A/B measurement.
var storeLoadFwdEnabled = os.Getenv("WAGO_ARM64_NOSTLDFWD") != "1"

const (
	nopWord = 0xD503201F // NOP (HINT #0)
)

// finalizePeepholes runs the size-preserving post-assembly peepholes over the
// finalized function code. They share one linear scan's worth of preconditions:
// the code must contain no indirect branch (br_table jump table — its embedded
// data words would be misread and its case stubs are reached through computed
// targets), and each rewrite must not disturb a word another branch targets. So
// the branch-target set is collected once here and threaded into both passes.
func (f *fn) finalizePeepholes() {
	compact := nativeFinalizerEnabled && f.compactNative()
	if !f.opt(optBranchFold) && !f.opt(optStoreLoadFwd) && !compact {
		return
	}
	// The established size-stable peephole abandons a function when it reaches
	// an indirect branch because jump-table data follows in the instruction
	// stream. Keep that no-rewrite boundary under compaction too: the explicit
	// fragment inventory still lets the finalizer shrink frame reservations and
	// repatch PC-relative words without retaining every branch target in a large
	// map merely to prove local peepholes safe.
	if compact && f.opaqueFragments {
		return
	}
	b := f.a.B
	n := len(b) &^ 3 // whole words only
	if n < 8 {
		return
	}
	sc := f.scratchState()
	instructions := n / 4
	words := (instructions + 63) / 64
	var targets []uint64
	if words <= len(sc.branchTargetInline) {
		targets = sc.branchTargetInline[:words]
		clear(targets)
	} else {
		// A giant function gets exact ephemeral backing. Do not retain its
		// high-water in module or parallel-worker scratch.
		targets = make([]uint64, words)
	}
	sc.branchTargets = targets
	for pc := 0; pc < n; pc += 4 {
		w := rdWord(b, pc)
		if !compact && isIndirectBranch(w) {
			return
		}
		if t, ok := branchTarget(pc, w); ok {
			sc.hasBranchTargets = true
			branchTargetAdd(targets, t, n)
			if compact && t == pc+4 && w&0xFC000000 != 0x94000000 {
				f.recordBranchNext(pc)
			}
		}
	}
	if f.opt(optBranchFold) {
		f.foldSingleBitBranches(b, n, targets)
		f.foldBranchPairs(b, n, targets)
	}
	if f.opt(optStoreLoadFwd) {
		f.forwardStoreLoads(b, n, targets)
	}
}

type singleBitTestSite struct {
	off int
	reg Reg
	bit uint8
}

func (f *fn) recordSingleBitTest(off int, reg Reg, bit uint8) {
	if !f.opt(optBranchFold) || !nativeFinalizerEnabled || !f.compactNative() {
		return
	}
	sc := f.scratchState()
	if int(sc.singleBitTestN) == len(sc.singleBitTests) {
		return
	}
	sc.singleBitTests[sc.singleBitTestN] = singleBitTestSite{off: off, reg: reg, bit: bit}
	sc.singleBitTestN++
}

// foldSingleBitBranches consumes only candidates explicitly recorded by the
// masked-eqz lowering. The final target is now known, so an adjacent TST plus
// EQ/NE branch can become TBZ/TBNZ when the tighter imm14 range permits it.
func branchTargetAdd(targets []uint64, off, n int) {
	if off < 0 || off >= n || off&3 != 0 {
		return
	}
	word := off >> 8
	targets[word] |= uint64(1) << ((off >> 2) & 63)
}

func branchTargeted(targets []uint64, off int) bool {
	if off < 0 || off&3 != 0 {
		return false
	}
	word := off >> 8
	return word < len(targets) && targets[word]&(uint64(1)<<((off>>2)&63)) != 0
}

func (f *fn) foldSingleBitBranches(b []byte, n int, targets []uint64) {
	sc := f.scratchState()
	for _, site := range sc.singleBitTests[:sc.singleBitTestN] {
		test, branch := site.off, site.off+4
		if test < 0 || branch+4 > n || branchTargeted(targets, branch) || int(sc.deadHoleN) == len(sc.deadHoleSites) {
			continue
		}
		w := rdWord(b, branch)
		if w&0xFF000010 != 0x54000000 {
			continue
		}
		cc := Cond(w & 0xF)
		if cc != condE && cc != condNE {
			continue
		}
		target, ok := branchTarget(branch, w)
		if !ok {
			continue
		}
		delta := target - test
		if delta&3 != 0 {
			continue
		}
		words := delta / 4
		if words < -(1<<13) || words >= 1<<13 {
			continue
		}
		base := uint32(0x36000000) // TBZ
		if cc == condNE {
			base |= 0x01000000 // TBNZ
		}
		word := base | uint32(site.bit>>5)<<31 | uint32(site.bit&31)<<19 |
			(uint32(words)&0x3FFF)<<5 | uint32(site.reg&31)
		wrWord(b, test, word)
		wrWord(b, branch, nopWord)
		f.recordDeadHole(branch)
		f.stats.peep("single-bit-test-branch")
	}
}

// foldBranchPairs rewrites the `B.cond +8 ; B target` double-branch idiom into a
// single `B.invcond target ; NOP`, in place over the finalized function code.
//
// The idiom is emitted all over the backend: every `br_if` to a structured
// label, every linear `br_table` case, every eqz loop exit lowers to a
// conditional branch that *skips* an unconditional branch to the real target
// (the skip keeps the edge's value-move/converge code, if any, on the taken
// path). When that edge carries no code the two branches collapse to one:
// inverting the condition and pointing it straight at the target removes a
// taken branch and its control dependency.
//
// The rewrite is size-preserving (the freed slot becomes a NOP), so it runs
// after every branch has been patched and never perturbs another offset. It is
// correct as long as nothing branches *into* the middle word (the unconditional
// B that becomes a NOP): an external entrant would otherwise see a NOP where it
// expected a branch. We prove that by collecting every PC-relative branch
// target first and only folding pairs whose middle word is not among them.
func (f *fn) foldBranchPairs(b []byte, n int, targets []uint64) {
	for pc := 0; pc+8 <= n; pc += 4 {
		w := rdWord(b, pc)
		cc, ok := bcondSkipOne(w) // B.cond whose displacement is exactly +2 words
		if !ok {
			continue
		}
		mid := pc + 4
		if branchTargeted(targets, mid) {
			continue // something jumps to the middle word — cannot NOP it
		}
		wm := rdWord(b, mid)
		off, ok := uncondBranchImm(wm)
		if !ok {
			continue // middle word is not an unconditional B
		}
		tgt := mid + off
		d := (tgt - pc) / 4
		if d < -(1<<18) || d >= (1<<18) {
			continue // target out of B.cond (imm19) range — keep the two-branch form
		}
		inv := uint32(cc.Invert())
		wrWord(b, pc, 0x54000000|(uint32(d)&0x7FFFF)<<5|inv)
		wrWord(b, mid, nopWord)
		f.recordDeadHole(mid)
		f.stats.peep("br-pair-fold")
		if f.stats != nil {
			f.stats.NativeSize.BranchFoldHoleBytes += 4
		}
		pc += 4 // step past the NOP we just wrote
	}
}

// forwardStoreLoads rewrites an SP-relative store immediately followed by a load
// of the same slot at the same width — `STR Xs,[SP,#k] ; LDR Xd,[SP,#k]` — into
// the store plus `MOV Xd,Xs` (or a NOP when Xd==Xs), forwarding the just-stored
// value instead of round-tripping it through memory. The store is kept: a later
// read of the slot must still see it. Emitted around inlined-call arg staging and
// call-adjacent spills, where a value is flushed to its canonical slot and then
// reloaded on the very next instruction.
//
// Correct because the two instructions are adjacent (nothing rewrites the slot or
// SP between them) and only fired when nothing branches to the load: an external
// entrant that skipped the store must genuinely load from memory.
func (f *fn) forwardStoreLoads(b []byte, n int, targets []uint64) {
	for pc := 0; pc+8 <= n; pc += 4 {
		if f.forwardStoreLoadAt(b, n, pc, targets, true) {
			pc += 4 // step past the word we just rewrote
		}
	}
}

func (f *fn) forwardStoreLoadAt(b []byte, n, pc int, targets []uint64, recordHole bool) bool {
	rs, k, w64, ok := spStoreImm(rdWord(b, pc))
	if !ok {
		return false
	}
	ld := pc + 4
	if ld+4 > n || branchTargeted(targets, ld) {
		return false // a branch lands on the load — it must read memory
	}
	rd, k2, w642, ok := spLoadImm(rdWord(b, ld))
	if !ok || k != k2 || w64 != w642 || rd == 31 {
		return false
	}
	if rd == rs {
		wrWord(b, ld, nopWord)
		if recordHole {
			f.recordDeadHole(ld)
		}
		if f.stats != nil {
			f.stats.NativeSize.StoreLoadNopBytes += 4
		}
	} else if w64 {
		wrWord(b, ld, 0xAA0003E0|uint32(rs)<<16|uint32(rd))
	} else {
		wrWord(b, ld, 0x2A0003E0|uint32(rs)<<16|uint32(rd))
	}
	f.stats.peep("store-load-fwd")
	return true
}

// finalizerOpaqueAt decodes the marker-map representation used by validation.
// Production compaction scans scratch.finalFragments through an ordered cursor
// so it does not pay these hash probes for every emitted word.
func finalizerOpaqueAt(markers map[int]bool, pc int, opaque *bool) bool {
	if markers[finalizerMarkerKey(pc, markerJumpDataEnd)] || markers[finalizerMarkerKey(pc, markerOpaqueDataEnd)] || markers[finalizerMarkerKey(pc, markerPluginEnd)] {
		*opaque = false
	}
	if markers[finalizerMarkerKey(pc, markerJumpDataStart)] || markers[finalizerMarkerKey(pc, markerOpaqueDataStart)] || markers[finalizerMarkerKey(pc, markerPluginStart)] {
		*opaque = true
	}
	return *opaque
}

// recordDeadHole retains explicit hole positions in the branch-target map after
// the target scan is complete. Negative keys cannot be valid function-relative
// branch targets, so this reuses existing scratch without another allocation or
// per-function slice. The finalizer decodes these entries before compaction.
func (f *fn) recordDeadHole(off int) {
	if !nativeFinalizerEnabled || !f.compactNative() {
		return
	}
	sc := f.scratchState()
	if int(sc.deadHoleN) == len(sc.deadHoleSites) {
		sc.deadHoleOverflow = true
		return
	}
	sc.deadHoleSites[sc.deadHoleN] = off
	sc.deadHoleN++
	if nativeFinalizerValidate {
		f.recordFinalizerMarker(off, markerDeadHole)
	}
}

// spStoreImm / spLoadImm decode an unsigned-offset SP-relative STR/LDR of a full
// 32- or 64-bit GPR, returning the transferred register, the byte offset, and
// whether it is 64-bit. Only the SP base (Rn==31) is matched: these slots are the
// only ones the store→load-forwarding invariant (adjacent, no aliasing) holds for.
func spStoreImm(w uint32) (rt Reg, off int, w64, ok bool) {
	switch {
	case w&0xFFC00000 == 0xF9000000: // STR Xt,[SP,#imm]
		w64 = true
	case w&0xFFC00000 == 0xB9000000: // STR Wt,[SP,#imm]
	default:
		return 0, 0, false, false
	}
	if (w>>5)&0x1F != 31 { // base must be SP
		return 0, 0, false, false
	}
	scale := 4
	if w64 {
		scale = 8
	}
	return Reg(w & 0x1F), int((w>>10)&0xFFF) * scale, w64, true
}

func spLoadImm(w uint32) (rt Reg, off int, w64, ok bool) {
	switch {
	case w&0xFFC00000 == 0xF9400000: // LDR Xt,[SP,#imm]
		w64 = true
	case w&0xFFC00000 == 0xB9400000: // LDR Wt,[SP,#imm]
	default:
		return 0, 0, false, false
	}
	if (w>>5)&0x1F != 31 {
		return 0, 0, false, false
	}
	scale := 4
	if w64 {
		scale = 8
	}
	return Reg(w & 0x1F), int((w>>10)&0xFFF) * scale, w64, true
}

// rdWord/wrWord read and write a little-endian 32-bit instruction word.
func rdWord(b []byte, pc int) uint32 {
	return uint32(b[pc]) | uint32(b[pc+1])<<8 | uint32(b[pc+2])<<16 | uint32(b[pc+3])<<24
}

func wrWord(b []byte, pc int, w uint32) {
	b[pc] = byte(w)
	b[pc+1] = byte(w >> 8)
	b[pc+2] = byte(w >> 16)
	b[pc+3] = byte(w >> 24)
}

// bcondSkipOne reports whether w is a `B.cond` whose displacement is exactly +2
// words (it skips the following instruction), returning its condition.
func bcondSkipOne(w uint32) (Cond, bool) {
	if w&0xFF000010 != 0x54000000 { // B.cond: 0101_0100 ... 0 cccc
		return 0, false
	}
	if imm19(w) != 2 {
		return 0, false
	}
	return Cond(w & 0xF), true
}

// uncondBranchImm returns the signed word displacement of an unconditional `B`
// (imm26), or ok=false if w is not a plain B (BL is excluded).
func uncondBranchImm(w uint32) (int, bool) {
	if w&0xFC000000 != 0x14000000 { // B: 000101 imm26
		return 0, false
	}
	d := int(w & 0x03FFFFFF)
	if d&(1<<25) != 0 {
		d -= 1 << 26
	}
	return d * 4, true
}

// branchTarget returns the byte offset a PC-relative branch at pc jumps to.
// Covers B, BL, B.cond, CBZ/CBNZ, TBZ/TBNZ — every static branch that can land
// inside the function. Explicit fragment markers keep embedded data out of the
// instruction walk; indirect branches require no target entry.
func branchTarget(pc int, w uint32) (int, bool) {
	switch {
	case w&0xFC000000 == 0x14000000, w&0xFC000000 == 0x94000000: // B / BL (imm26)
		d := int(w & 0x03FFFFFF)
		if d&(1<<25) != 0 {
			d -= 1 << 26
		}
		return pc + d*4, true
	case w&0xFF000010 == 0x54000000: // B.cond (imm19)
		return pc + imm19(w)*4, true
	case w&0x7E000000 == 0x34000000: // CBZ/CBNZ (imm19)
		return pc + imm19(w)*4, true
	case w&0x7E000000 == 0x36000000: // TBZ/TBNZ (imm14)
		d := int((w >> 5) & 0x3FFF)
		if d&(1<<13) != 0 {
			d -= 1 << 14
		}
		return pc + d*4, true
	}
	return 0, false
}

// imm19 sign-extends the 19-bit branch displacement field (bits 23:5), in words.
func imm19(w uint32) int {
	d := int((w >> 5) & 0x7FFFF)
	if d&(1<<18) != 0 {
		d -= 1 << 19
	}
	return d
}

// isIndirectBranch reports whether w is a BR (unconditional indirect branch).
// Without explicit fragment handling, the old peephole path stops here because
// a br_table may place data immediately after the dispatch.
func isIndirectBranch(w uint32) bool {
	return w&0xFFFFFC1F == 0xD61F0000 // BR Xn
}
