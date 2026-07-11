//go:build amd64

package amd64

import (
	"encoding/binary"
	"os"
)

// branchFoldEnabled gates the post-assembly double-branch peephole. On by
// default; WAGO_AMD64_NOBRFOLD=1 disables it for A/B measurement.
var branchFoldEnabled = os.Getenv("WAGO_AMD64_NOBRFOLD") != "1"

// recordBrFold notes a br_if lowering that collapsed to the `Jcc over ; JMP
// target ; over:` idiom with no edge code — a candidate for folding into a single
// inverted Jcc. `over` is the Jcc's rel32 field offset (from JccPlaceholder). The
// idiom holds only when the unconditional jump sits immediately after the Jcc,
// i.e. the byte just past the Jcc's rel32 is the 0xE9 jump opcode; a non-empty
// edge (value moves, reg-merge, a function-return result load) puts other bytes
// there instead, so it is not recorded. The jump's rel32 is patched later (block
// ends), so the actual fold runs post-assembly in finalizeBranchFolds.
func (f *fn) recordBrFold(over int) {
	if !branchFoldEnabled {
		return
	}
	if b := f.a.B; over+4 < len(b) && b[over+4] == 0xE9 {
		f.brFoldSites = append(f.brFoldSites, over)
	}
}

// finalizeBranchFolds rewrites each recorded `Jcc(cc) over ; JMP target ; over:`
// into `Jcc(!cc) target ; NOP`, in place over the finalized function code. It runs
// after every branch has been patched, so the jump's target is final. The rewrite
// is size-preserving (the freed jump becomes a 5-byte NOP), so it never perturbs
// another offset; correctness follows from re-verifying the exact idiom byte shape
// at each recorded site (a stale/mismatched site is skipped, not miswritten).
//
// Only opBr/brIfFused sites are recorded — never br_table, whose jump-table data
// words must not be touched — so the pass reads and writes nothing it did not emit
// as this precise pair.
func (f *fn) finalizeBranchFolds() {
	if !branchFoldEnabled {
		return
	}
	b := f.a.B
	for _, over := range f.brFoldSites {
		// Idiom: 0F 8x <rel32=5> | E9 <rel32> | over:
		//   Jcc opcode at over-2..over-1, rel32 at over..over+3 (ends at over+4);
		//   JMP opcode at over+4, rel32 at over+5..over+8 (ends/over: at over+9).
		if over < 2 || over+9 > len(b) {
			continue
		}
		if b[over-2] != 0x0F || b[over+4] != 0xE9 {
			continue
		}
		if int32(binary.LittleEndian.Uint32(b[over:])) != 5 {
			continue // Jcc no longer targets the byte just past the 5-byte JMP
		}
		jmpTarget := over + 9 + int(int32(binary.LittleEndian.Uint32(b[over+5:])))
		b[over-1] ^= 1 // invert the condition (flip the tttn low bit)
		binary.LittleEndian.PutUint32(b[over:], uint32(int32(jmpTarget-(over+4))))
		copy(b[over+4:over+9], []byte{0x0F, 0x1F, 0x44, 0x00, 0x00}) // 5-byte NOP
		f.stats.peep("br-pair-fold")
	}
}
