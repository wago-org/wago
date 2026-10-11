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
func csetLineageWord(w uint32) (lineageIntegerEffects, bool) {
	if w&0x3b000000 == 0x39000000 && w&(1<<26) == 0 && w>>30 >= 2 && w>>22&3 <= 1 {
		x := lineageIntegerEffects{}
		if w&(1<<22) != 0 {
			x.writes = (uint32(1) << (w & 31)) & 0x7fffffff
		}
		return x, true
	}
	return lineageIntegerWord(w)
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

type lineageIntegerEffects struct {
	writes                  uint32
	flags, readsFlags, copy bool
}

// Deliberately narrow, architecturally pure integer encodings. Register31 is
// zero here; SP forms, extended arithmetic, division and unknown words decline.
func lineageIntegerWord(w uint32) (x lineageIntegerEffects, ok bool) {
	wide := w&0x80000000 != 0
	reg := func(r uint32) uint32 { return (uint32(1) << r) & 0x7fffffff }
	rd, rn := w&31, w>>5&31
	x.writes = reg(rd)
	switch {
	case w == nopWord:
		x.writes = 0
	case w&0x1f800000 == 0x12800000:
		op := w >> 29 & 3
		if op == 1 || !wide && w>>21&3 > 1 {
			return x, false
		}
	case w&0x1f000000 == 0x0a000000:
		if !wide && w>>10&63 >= 32 {
			return x, false
		}
		x.flags = w>>29&3 == 3
		x.copy = w&0x7fe0ffe0 == 0x2a0003e0 // ORR Rd,ZR,Rm,LSL0 (MOV)
	case w&0x1f800000 == 0x12000000:
		if rd == 31 && w>>29&3 != 3 { // Nonflag logical-immediate Rd31 is SP.
			return x, false
		}
		if !wide && w&(1<<22) != 0 {
			return x, false
		}
		encoded := (w>>22&1)<<6 | (^w >> 10 & 63)
		level := uint32(0)
		for encoded > 1 {
			encoded >>= 1
			level++
		}
		if level < 1 || w>>10&((1<<level)-1) == (1<<level)-1 {
			return x, false
		}
		x.flags = w>>29&3 == 3
	case w&0x1f800000 == 0x11000000:
		if rn == 31 || rd == 31 && w&(1<<29) == 0 {
			return x, false
		}
		x.flags = w&(1<<29) != 0
	case w&0x1f200000 == 0x0b000000:
		if w>>22&3 == 3 || !wide && w>>10&63 >= 32 {
			return x, false
		}
		x.flags = w&(1<<29) != 0
	case w&0x7fe00000 == 0x1b000000:
	case w&0x1f800000 == 0x13000000:
		if w>>29&3 == 3 || (w>>22&1 != w>>31) || !wide && (w>>16&63 >= 32 || w>>10&63 >= 32) {
			return x, false
		}
	case w&0x7fff0fe0 == 0x1a9f07e0: // CSET has no GPR inputs.
		x.readsFlags = true
	case w&0x1fe00000 == 0x1a800000: // Other integer conditional selects.
		if w&(1<<29) != 0 || w&(1<<11) != 0 {
			return x, false
		}
		x.readsFlags = true
	default:
		return x, false
	}
	return x, true
}
