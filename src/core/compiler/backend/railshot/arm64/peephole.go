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

// A bounded inline inventory replaces repeated whole-function scans. Overflow
// retains the complete scan; a large function can never lose an optimization.
type peepholeSites struct {
	pcs      [64]uint32
	n        int
	overflow bool
}

type singleBitTestSite struct {
	off int
	reg Reg
	bit uint8
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
	b = b[:n:n]
	for pc := 0; pc <= len(b)-8; pc += 4 {
		if f.foldBranchPairAt(b, pc, targets) {
			pc += 4 // step past the NOP we just wrote
		}
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
