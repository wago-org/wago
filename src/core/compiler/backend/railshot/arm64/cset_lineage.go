//go:build arm64

package arm64

import (
	"math/bits"
	"os"
)

var csetLineageEnabled = os.Getenv("WAGO_ARM64_NO_CSET_LINEAGE") != "1"

func csetLineagePair(cmp, sel uint32) bool {
	return cmp & ^uint32(31<<5) == 0x7100001f && sel&0x7fe00c00 == 0x1a800000 && sel>>12&15 < 2
}

// Scalar unsigned-offset loads/stores do not touch NZCV or their base register.
// All other admitted words use the established pure integer decoder. Unknown,
// SIMD, signed-load, writeback and control encodings deliberately decline.
func csetLineageWord(w uint32) (guardedIntegerEffects, bool) {
	if w&0x3b000000 == 0x39000000 && w&(1<<26) == 0 && w>>30 >= 2 && w>>22&3 <= 1 {
		x := guardedIntegerEffects{}
		if w&(1<<22) != 0 {
			x.writes = (uint32(1) << (w & 31)) & 0x7fffffff
		}
		return x, true
	}
	return guardedIntegerWord(w)
}

// The replaced CMP has a different NZCV value from the producer. Require an
// unconditional overwrite or RET before any subsequent flag consumer, using
// only exact known words over a bounded straight path.
func csetLineageFlagsDead(b []byte, pc, n int) bool {
	for steps := 0; steps < 12 && pc >= 0 && pc+4 <= n; steps++ {
		w := rdWord(b, pc)
		if w == 0xd65f03c0 {
			return true
		}
		if w&0xfc000000 == 0x14000000 {
			next, ok := branchTarget(pc, w)
			if !ok {
				return false
			}
			pc = next
			continue
		}
		x, ok := csetLineageWord(w)
		if !ok || x.readsFlags {
			return false
		}
		if x.flags {
			return true
		}
		pc += 4
	}
	return false
}

// Reuse the producer's flags for a canonical CSET boolean, retaining the CSET
// itself for other value uses. Facts are scoped to a straight-line NZCV epoch;
// only exact W/X MOV copies propagate them. The pass adds no function metadata
// and caps both function size and producer-to-consumer distance.
func (f *fn) foldCsetLineage(b []byte, n, firstSet int, targets []uint64, entries ...int) {
	if !f.opt(optCsetLineage) || n > 4096 || f.opaqueFragments || f.moduleEH || len(f.customInstructions) != 0 {
		return
	}
	var boundaries [16]uint64
	copy(boundaries[:], targets)
	for _, entry := range entries {
		branchTargetAdd(boundaries[:], entry, n)
	}
	type fact struct {
		epoch     uint16
		condition Cond
	}
	var facts [32]fact
	epoch := uint16(1)
	producer := -1
	// Earlier producers cannot pass the 128-byte distance bound at any
	// consumer after the first CSET. Start with unknown facts at that bound.
	start := max(0, firstSet-128)
	for pc := start; pc+4 <= n; pc += 4 {
		if branchTargeted(boundaries[:], pc) {
			epoch++
			producer = -1
		}
		w := rdWord(b, pc)
		if pc+8 <= n && csetLineagePair(w, rdWord(b, pc+4)) {
			v := facts[w>>5&31]
			if producer >= 0 && pc-producer <= 128 && v.epoch == epoch && !branchTargeted(boundaries[:], pc+4) && csetLineageFlagsDead(b, pc+8, n) {
				sel := rdWord(b, pc+4)
				cc := v.condition
				if sel>>12&15 == 0 {
					cc = invertCond(cc)
				}
				wrWord(b, pc, nopWord)
				wrWord(b, pc+4, sel & ^uint32(15<<12) | uint32(cc)<<12)
				f.stats.peep("cset-lineage")
				continue
			}
		}
		x, ok := csetLineageWord(w)
		if !ok {
			epoch++
			producer = -1
			continue
		}
		if x.flags {
			epoch++
			producer = pc
			continue
		}
		copied := fact{}
		if x.copy {
			copied = facts[w>>16&31]
		}
		for writes := x.writes; writes != 0; writes &= writes - 1 {
			facts[bits.TrailingZeros32(writes)] = fact{}
		}
		rd := w & 31
		if x.copy && rd != 31 {
			facts[rd] = copied
		}
		if w&0x7fff0fe0 == 0x1a9f07e0 && producer >= 0 && rd != 31 {
			cc := invertCond(Cond(w >> 12 & 15))
			if cc < 14 {
				facts[rd] = fact{epoch, cc}
			}
		}
	}
}
