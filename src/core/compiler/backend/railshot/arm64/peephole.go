//go:build arm64

package arm64

import "os"

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
	if !branchFoldEnabled && !storeLoadFwdEnabled {
		return
	}
	b := f.a.B
	n := len(b) &^ 3 // whole words only
	if n < 8 {
		return
	}
	var targets map[int]bool
	for pc := 0; pc < n; pc += 4 {
		w := rdWord(b, pc)
		if isIndirectBranch(w) {
			return
		}
		if t, ok := branchTarget(pc, w); ok {
			if targets == nil {
				targets = make(map[int]bool, 16)
			}
			targets[t] = true
		}
	}
	if branchFoldEnabled {
		f.foldBranchPairs(b, n, targets)
	}
	if storeLoadFwdEnabled {
		f.forwardStoreLoads(b, n, targets)
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
func (f *fn) foldBranchPairs(b []byte, n int, targets map[int]bool) {
	for pc := 0; pc+8 <= n; pc += 4 {
		w := rdWord(b, pc)
		cc, ok := bcondSkipOne(w) // B.cond whose displacement is exactly +2 words
		if !ok {
			continue
		}
		mid := pc + 4
		if targets[mid] {
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
		f.stats.peep("br-pair-fold")
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
func (f *fn) forwardStoreLoads(b []byte, n int, targets map[int]bool) {
	for pc := 0; pc+8 <= n; pc += 4 {
		rs, k, w64, ok := spStoreImm(rdWord(b, pc))
		if !ok {
			continue
		}
		ld := pc + 4
		if targets[ld] {
			continue // a branch lands on the load — it must read memory
		}
		rd, k2, w642, ok := spLoadImm(rdWord(b, ld))
		if !ok || k != k2 || w64 != w642 {
			continue // different slot or width
		}
		if rd == 31 { // LDR ZR — degenerate; leave it
			continue
		}
		if rd == rs {
			wrWord(b, ld, nopWord) // value already in the register
		} else if w64 {
			wrWord(b, ld, 0xAA0003E0|uint32(rs)<<16|uint32(rd)) // MOV Xd,Xs (ORR Xd,XZR,Xs)
		} else {
			wrWord(b, ld, 0x2A0003E0|uint32(rs)<<16|uint32(rd)) // MOV Wd,Ws (ORR Wd,WZR,Ws)
		}
		f.stats.peep("store-load-fwd")
		pc += 4 // step past the word we just rewrote
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
// inside the function. Indirect branches are handled by isIndirectBranch.
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
// BLR and RET are excluded: a BLR resumes at the following word (a fall-through,
// never a fold middle) and RET leaves the function.
func isIndirectBranch(w uint32) bool {
	return w&0xFFFFFC1F == 0xD61F0000 // BR Xn
}
