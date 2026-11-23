// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package arm64

import (
	"encoding/binary"
	"runtime"
)

// indexedReuseWindow records one omitted X16 address computation. The proof
// examined the straight-line bytes in [start, at); the reusing access is
// emitted at `at`. A branch target t with start < t <= at enters the access
// from a predecessor that never established X16, so the reuse is unsound.
type indexedReuseWindow struct {
	start, at int
}

// ResetIndexedReuse clears the per-function reuse proof state.
func (a *Asm) ResetIndexedReuse() {
	a.indexedReuseFence = 0
	a.indexedReuseWindows = a.indexedReuseWindows[:0]
}

// NoteBranchTarget records that control may enter at byte offset t. Later
// indexed-base proofs never look back across it.
func (a *Asm) NoteBranchTarget(t int) {
	if t > a.indexedReuseFence {
		a.indexedReuseFence = t
	}
}

// admitIndexedReuse accepts a proof that examined bytes from `start` up to the
// current position, and records it for ValidateIndexedReuse.
func (a *Asm) admitIndexedReuse(start int) bool {
	if start < a.indexedReuseFence {
		return false
	}
	a.indexedReuseWindows = append(a.indexedReuseWindows, indexedReuseWindow{start: start, at: len(a.B)})
	return true
}

// IndexedReuseCount reports how many omitted X16 computations await validation.
func (a *Asm) IndexedReuseCount() int { return len(a.indexedReuseWindows) }

// IndexedReuseAcceptsTarget checks an entry or branch target against the
// recorded reuse windows. Callers can validate jump-table words as they are
// decoded, without staging a temporary list of target offsets.
func (a *Asm) IndexedReuseAcceptsTarget(t int) bool {
	w := a.indexedReuseWindows
	// Windows are recorded in emission order, so starts are non-decreasing.
	lo, hi := 0, len(w)
	for lo < hi {
		mid := (lo + hi) / 2
		if w[mid].start < t {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	// Only windows starting before t can contain it; they are w[:lo]. Each
	// window spans at most a few words, so scan back while still in reach.
	for i := lo - 1; i >= 0 && t-w[i].start <= 64; i-- {
		if t <= w[i].at {
			return false
		}
	}
	return true
}

// indexedReuseTargetOpcode is a first-byte filter over B, BL, B.cond,
// CBZ/CBNZ, TBZ/TBNZ and ADR. Exact decoding still checks the remaining bits.
var indexedReuseTargetOpcode = [256]bool{
	0x14: true, 0x15: true, 0x16: true, 0x17: true, // B
	0x94: true, 0x95: true, 0x96: true, 0x97: true, // BL
	0x54: true,                                     // B.cond
	0x34: true, 0x35: true, 0xb4: true, 0xb5: true, // CBZ/CBNZ
	0x36: true, 0x37: true, 0xb6: true, 0xb7: true, // TBZ/TBNZ
	0x10: true, 0x30: true, 0x50: true, 0x70: true, // ADR
}

// ValidateIndexedReuse reports whether every recorded indexed-base reuse is
// still straight-line: no direct branch, ADR landing pad, or extra target
// (jump-table entries, internal entries) lands inside a proof window. Call it
// once the function body is complete and before any pass moves code. Opaque
// data ranges are skipped; their words are not instructions.
func (a *Asm) ValidateIndexedReuse(extraTargets []int, opaque func(pc int) bool) bool {
	if len(a.indexedReuseWindows) == 0 {
		return true
	}
	for _, t := range extraTargets {
		if !a.IndexedReuseAcceptsTarget(t) {
			return false
		}
	}
	b := a.B
	for pc := 0; pc+4 <= len(b); pc += 4 {
		if runtime.Compiler == "tinygo" && !indexedReuseTargetOpcode[b[pc+3]] {
			continue
		}
		if opaque != nil && opaque(pc) {
			continue
		}
		word := binary.LittleEndian.Uint32(b[pc:])
		if t, ok := dominatedDirectBranchTarget(pc, word); ok && !a.IndexedReuseAcceptsTarget(t) {
			return false
		}
		if word&0x9F000000 == 0x10000000 { // ADR
			imm := int32((word>>5&0x7FFFF)<<2|word>>29&3) << 11 >> 11
			if !a.IndexedReuseAcceptsTarget(pc + int(imm)) {
				return false
			}
		}
	}
	return true
}
