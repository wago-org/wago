// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import "os"

var repeatedStoreRegionEnabled = os.Getenv("WAGO_ARM64_NO_REPEATED_STORE_REGION") != "1"

// truncateNativeCode preserves the assembler's existing rollback behavior and
// discards provenance for bytes that may subsequently be reused for other code.
func (f *fn) truncateNativeCode(n int) {
	if f.sc != nil && f.sc.repeatedStoreOriginsSet && n < len(f.sc.repeatedStoreOrigins)*256 {
		word, bit := n>>8, uint((n>>2)&63)
		f.sc.repeatedStoreOrigins[word] &= (uint64(1) << bit) - 1
		clear(f.sc.repeatedStoreOrigins[word+1:])
	}
	f.a.B = f.a.B[:n]
}

// Record only native stores emitted directly by an unshared Wasm constant
// store. The fixed bitset is reused per function and does not retain code.
func (f *fn) recordRepeatedWasmStores(start, end int) {
	if f.guardMode || f.threadedMemory0 || f.moduleEH || f.interruptible || len(f.customInstructions) != 0 {
		return
	}
	sc := f.scratchState()
	for pc := start; pc+4 <= end && pc>>8 < len(sc.repeatedStoreOrigins); pc += 4 {
		if repeatRegionStore(rdWord(f.a.B, pc)) {
			bit := uint64(1) << ((pc >> 2) & 63)
			if sc.repeatedStoreOrigins[pc>>8]&bit == 0 && sc.repeatedStoreOriginsN != ^uint16(0) {
				sc.repeatedStoreOriginsN++
			}
			sc.repeatedStoreOrigins[pc>>8] |= bit
			sc.repeatedStoreOriginsSet = true
		}
	}
}

func repeatedStoreOriginsCover(b []byte, first, second, words int, origins []uint64) bool {
	for i := 0; i < words; i++ {
		a, c := first+4*i, second+4*i
		if repeatRegionStore(rdWord(b, a)) && (!branchTargeted(origins, a) || !branchTargeted(origins, c)) {
			return false
		}
	}
	return true
}

func repeatRegionStore(w uint32) bool {
	return w&0x3b000000 == 0x39000000 && w&(1<<26) == 0 && w>>22&3 == 0 || w&0x3fe00c00 == 0x38200800
}

func repeatRegionWord(w uint32) bool {
	return w == nopWord || w&0x7f800000 == 0x52800000 || repeatRegionStore(w)
}

func repeatRegionAnyStore(w uint32) bool {
	if repeatRegionStore(w) || w&0x3b200c00 == 0x38000000 && w&(1<<26) == 0 && w&(1<<22) == 0 {
		return true
	}
	return w&0xffe0fc00 == 0xbc206800 || w&0xffe0fc00 == 0xfc206800 || w&0xffc00000 == 0xbd000000 || w&0xffc00000 == 0xfd000000
}

// Admit only addresses derived from the reserved linear-memory base. Native
// stores to globals, frames, or runtime state need a different effect model.
func repeatLinearStoreRegion(b []byte, first, words int, targets []uint64, entries []int) bool {
	if first < 4 || first+4*words > len(b) || branchTargeted(targets, first) {
		return false
	}
	for _, e := range entries {
		if e == first {
			return false
		}
	}
	a := rdWord(b, first-4)
	index := a >> 16 & 31
	if a&0xffe0fc00 != 0x8b000000 || a&31 != 16 || a>>5&31 != 26 || index == 16 || index == 26 || index == 31 {
		return false
	}
	for i := 0; i < words; i++ {
		w := rdWord(b, first+4*i)
		if repeatRegionStore(w) && w>>5&31 != 16 && w>>5&31 != 26 {
			return false
		}
		x, ok := nativeLoopConstExitWord(w)
		if !ok || x.writes&(1<<16|1<<26) != 0 {
			return false
		}
	}
	return true
}

// repeatStoreRegionAt removes the second of two identical bounded store
// sequences only when the first dominates it, every input and written register
// is unchanged between them, and the intervening region has no stores or calls.
func repeatStoreRegionAt(b []byte, first, second, words int, targets []uint64, entries []int) bool {
	if words < 4 || words > 24 || first < 0 || second < first+4*words || second+4*words > len(b) || second-(first+4*words) > 128 {
		return false
	}
	endFirst, endSecond := first+4*words, second+4*words
	var readsBefore, writes uint32
	stores := 0
	for i := 0; i < words; i++ {
		a, c := first+4*i, second+4*i
		w := rdWord(b, a)
		if w != rdWord(b, c) || !repeatRegionWord(w) {
			return false
		}
		if repeatRegionStore(w) {
			// The effect decoder omits SP; do not infer its stability here.
			if w>>5&31 == 31 {
				return false
			}
			stores++
		}
		x, ok := nativeLoopConstExitWord(w)
		if !ok {
			return false
		}
		readsBefore |= x.reads &^ writes
		writes |= x.writes
		if i > 0 && (branchTargeted(targets, a) || branchTargeted(targets, c)) {
			return false
		}
	}
	if stores < 2 || readsBefore&writes != 0 {
		return false
	}
	relevant := readsBefore | writes
	for _, e := range entries {
		if e > first && e < endSecond {
			return false
		}
	}
	for at := 0; at+4 <= len(b); at += 4 {
		to, branch := branchTarget(at, rdWord(b, at))
		if !branch {
			continue
		}
		if to > first && to < endFirst || to > second && to < endSecond {
			return false
		}
		if to >= endFirst && to <= second && (at < endFirst || at >= second) {
			return false
		}
	}
	for at := endFirst; at < second; at += 4 {
		w := rdWord(b, at)
		if to, branch := branchTarget(at, w); branch {
			if w&0xfc000000 == 0x94000000 || to <= at && to >= first && to < endSecond {
				return false
			}
			continue
		}
		if repeatRegionAnyStore(w) {
			return false
		}
		// SP is excluded from the generic register-effect masks. A stack
		// adjustment could invalidate addresses captured through another GPR.
		if w&0xffc0001f == 0x9100001f {
			return false
		}
		x, ok := nativeLoopConstExitWord(w)
		if !ok || x.writes&relevant != 0 {
			return false
		}
	}
	for at := second; at < endSecond; at += 4 {
		wrWord(b, at, nopWord)
	}
	return true
}

func (f *fn) foldRepeatedStoreRegions(b []byte, targets []uint64, entries ...int) {
	// The repeated blocks selected here commonly arise after many identical
	// indexed addresses are folded. Avoid another scan for ordinary loops.
	if !f.opt(optRepeatedStoreRegion) || !f.hasLoop || f.a.DominatedIndexedCandidates < 8 || f.scratchState().repeatedStoreOriginsN < 16 || f.guardMode || f.threadedMemory0 || f.moduleEH || f.interruptible || f.opaqueFragments || profileEnabled || len(f.customInstructions) != 0 || len(b) > 16<<10 {
		return
	}
	for at := 0; at+4 <= len(b); at += 4 {
		if isIndirectBranch(rdWord(b, at)) {
			return
		}
	}
	// Each prospective pair can require a full scan for alternate entries.
	// Bound aggregate proof work even on functions with many repeated stores.
	budget := 1 << 18
	charge := len(b)/4 + 64
	for first := 0; first+16 <= len(b); first += 4 {
		if !repeatRegionStore(rdWord(b, first)) || first >= 4 && repeatRegionWord(rdWord(b, first-4)) {
			continue
		}
		words := 0
		for words < 24 && first+4*words+4 <= len(b) && repeatRegionWord(rdWord(b, first+4*words)) {
			words++
		}
		if words < 4 || words == 24 {
			continue
		}
		if !repeatLinearStoreRegion(b, first, words, targets, entries) {
			continue
		}
		end := first + 4*words
		for second := end; second+4*words <= len(b) && second-end <= 128; second += 4 {
			if rdWord(b, second) != rdWord(b, first) {
				continue
			}
			if !repeatedStoreOriginsCover(b, first, second, words, f.scratchState().repeatedStoreOrigins[:]) {
				continue
			}
			if budget < charge {
				return
			}
			budget -= charge
			if repeatStoreRegionAt(b, first, second, words, targets, entries) {
				f.stats.peep("repeated-store-region")
				first = second + 4*words - 4
				break
			}
		}
	}
}
