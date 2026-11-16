// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import (
	"math/bits"
	"os"
)

var csetLineageEnabled = os.Getenv("WAGO_ARM64_NO_CSET_LINEAGE") != "1"

const csetLineageFunctionLimit = 8192

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
	// Scalar register-offset loads and stores preserve NZCV just like their
	// unsigned-immediate counterparts above. They occur naturally between a
	// select and the next comparison when a loop updates linear memory. Keep
	// exact destination effects so a load which overwrites a saved boolean still
	// invalidates its lineage. Exclude SIMD/FP forms and the size=8 PRFM encoding.
	if w&0x3b20fc00 == 0x38206800 && w&(1<<26) == 0 {
		opc := w >> 22 & 3
		if opc == 2 && w>>30 == 3 {
			return guardedIntegerEffects{}, false
		}
		x := guardedIntegerEffects{}
		if opc != 0 {
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

// The comparison boolean itself is dead when the recovered CSEL overwrites its
// register without any intervening read. In that case the original CSET can be
// replaced by a NOP along with the redundant CMP. Keep this deliberately local:
// copies and unknown words decline through the exact integer-effect decoder.
func csetLineageSetDead(b []byte, set, cmp, sel, n int, reg uint32) bool {
	if set < 0 || set+4 > cmp || sel != cmp+4 || sel+4 > n || rdWord(b, sel)&31 != reg {
		return false
	}
	mask := uint32(1) << reg
	for pc := set + 4; pc < cmp; pc += 4 {
		x, ok := csetLineageWord(rdWord(b, pc))
		if !ok || x.reads&mask != 0 || x.writes&mask != 0 {
			return false
		}
	}
	x, ok := csetLineageWord(rdWord(b, sel))
	return ok && x.writes&mask != 0 && x.reads&mask == 0
}

// Reuse the producer's flags for a canonical CSET boolean, retaining the CSET
// itself for other value uses. Facts are scoped to a straight-line NZCV epoch;
// only exact W/X MOV copies propagate them. The pass adds no function metadata
// and caps both function size and producer-to-consumer distance.
func (f *fn) foldCsetLineage(b []byte, n, firstSet int, targets []uint64, entries ...int) {
	if !f.opt(optCsetLineage) || n > csetLineageFunctionLimit || f.opaqueFragments || f.moduleEH || len(f.customInstructions) != 0 {
		return
	}
	var boundaries [csetLineageFunctionLimit / (64 * 4)]uint64
	copy(boundaries[:], targets)
	for _, entry := range entries {
		branchTargetAdd(boundaries[:], entry, n)
	}
	type fact struct {
		epoch     uint16
		condition Cond
		set       uint16 // instruction index plus one; functions are capped at 4 KiB
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
				set := -1
				if v.set != 0 {
					set = int(v.set-1) * 4
				}
				if csetLineageSetDead(b, set, pc, pc+4, n, w>>5&31) {
					wrWord(b, set, nopWord)
					f.stats.peep("dead-cset-lineage")
				}
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
				facts[rd] = fact{epoch: epoch, condition: cc, set: uint16(pc/4 + 1)}
			}
		}
	}
}
