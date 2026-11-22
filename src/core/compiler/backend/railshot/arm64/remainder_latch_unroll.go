// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import "encoding/binary"

// unrollRemainderLatch recognizes the register-only Euclidean transfer
//
//	temporary = A; A = B; trap_if_zero(A)
//	quotient = temporary / A; remainder = temporary - quotient*A; B = remainder
//
// followed by a B!=0 loop latch. Four alternating remainder steps return A/B
// to their original physical roles. Each intermediate zero test remains, and
// odd exits restore the canonical local homes off the hot path.
func (f *fn) unrollRemainderLatch(fr *ctrlFrame, condition Reg) bool {
	if !f.opt(optScalarLoopUnroll) || !f.hasLoop || profileEnabled || f.loopInterruptible() || f.moduleEH || f.gcFrameRoots != nil ||
		len(f.customInstructions) != 0 || f.policy.CompactNative || f.a.ReserveBranch19 || f.opaqueFragments ||
		f.stats != nil && f.stats.RecordSources || fr.paramN != 0 {
		return false
	}
	start, end := fr.controlSite, f.a.Len()
	if start < 0 || end-start < 20 || end-start > 32 || (end-start)&3 != 0 {
		return false
	}
	body := start
	for body < end && rdWord(f.a.B, body) == nopWord {
		body += 4
	}
	words := (end - body) / 4
	if words != 5 && words != 6 {
		return false
	}
	moveTemporary, moveA := rdWord(f.a.B, body), rdWord(f.a.B, body+4)
	guard, divide := rdWord(f.a.B, body+8), rdWord(f.a.B, body+12)
	multiplySubtract := rdWord(f.a.B, body+16)
	temporary, sourceA, ok := coldLoopMove64(moveTemporary)
	if !ok {
		return false
	}
	a, b, ok := coldLoopMove64(moveA)
	if !ok || sourceA != a {
		return false
	}
	sf := guard & 0x80000000
	q, dividend, divisor := Reg(divide&31), Reg(divide>>5&31), Reg(divide>>16&31)
	r := Reg(multiplySubtract & 31)
	if guard != sf|0x34000000|uint32(a) ||
		divide != sf|0x1ac00800|uint32(a)<<16|uint32(temporary)<<5|uint32(q) ||
		multiplySubtract != sf|0x1b008000|uint32(a)<<16|uint32(temporary)<<10|uint32(q)<<5|uint32(r) ||
		dividend != temporary || divisor != a || condition != b ||
		a == b || a == temporary || b == temporary || a == r ||
		a >= 31 || b >= 31 || q >= 31 || temporary >= 31 || r >= 31 ||
		q == a || q == b || q == temporary || !f.pinnedLocalMask.has(a) || !f.pinnedLocalMask.has(b) {
		return false
	}
	if (words == 5 && r != b) || (words == 6 && (r == b || rdWord(f.a.B, body+20) != 0xaa0003e0|uint32(r)<<16|uint32(b))) {
		return false
	}
	blocked := f.pinnedLocalMask.union(f.pinned).union(f.reserved)
	if blocked.has(temporary) || blocked.has(q) || f.regUser[temporary] != nil || f.regUser[q] != nil ||
		words == 6 && (blocked.has(r) || f.regUser[r] != nil) || gpAllocPos(q) < 0 {
		return false
	}

	// The existing source guard must remain the sole trap site in this body.
	// Keeping it at the same native offset preserves its source identity.
	guardSite := body + 8
	sc := f.scratchState()
	for code, sites := range sc.trapSites {
		if len(sites) == 0 {
			continue
		}
		site := int(sites[len(sites)-1].branch &^ 1)
		if site >= start && (uint32(code) != trapDivZero || site != guardSite || sites[len(sites)-1].branch&1 != 0) {
			return false
		}
	}
	divisionSites := sc.trapSites[trapDivZero]
	if len(divisionSites) == 0 || int(divisionSites[len(divisionSites)-1].branch) != guardSite {
		return false
	}

	// Phase zero reuses the original source slots so the trap site and loop
	// entry stay stable. The two obsolete transfers become NOPs.
	wrWord(f.a.B, body, nopWord)
	wrWord(f.a.B, body+4, nopWord)
	wrWord(f.a.B, guardSite, sf|0x34000000|uint32(b))
	wrWord(f.a.B, body+12, sf|0x1ac00800|uint32(b)<<16|uint32(a)<<5|uint32(q))
	wrWord(f.a.B, body+16, sf|0x1b008000|uint32(b)<<16|uint32(a)<<10|uint32(q)<<5|uint32(a))

	appendWord := func(word uint32) { f.a.B = binary.LittleEndian.AppendUint32(f.a.B, word) }
	phaseZeroExit := body + 20
	if words == 6 {
		wrWord(f.a.B, phaseZeroExit, sf|0x34000000|uint32(a))
	} else {
		phaseZeroExit = f.a.Len()
		appendWord(sf | 0x34000000 | uint32(a))
	}
	var canonicalExit int
	var swappedExit int
	for phase := 1; phase < 4; phase++ {
		left, right := a, b
		if phase&1 != 0 {
			left, right = b, a
		}
		appendWord(sf | 0x1ac00800 | uint32(right)<<16 | uint32(left)<<5 | uint32(q))
		appendWord(sf | 0x1b008000 | uint32(right)<<16 | uint32(left)<<10 | uint32(q)<<5 | uint32(left))
		if phase == 3 {
			back := f.a.Len()
			appendWord(sf | 0x35000000 | uint32(left))
			f.patchBranch19(back, body+12)
		} else {
			exit := f.a.Len()
			appendWord(sf | 0x34000000 | uint32(left))
			if phase&1 != 0 {
				canonicalExit = exit
			} else {
				swappedExit = exit
			}
		}
	}
	skipRestore := f.a.Branch()
	restore := f.a.Len()
	appendWord(0xaa0003e0 | uint32(b)<<16 | uint32(a))
	appendWord(0xaa0003e0 | uint32(31)<<16 | uint32(b))
	complete := f.a.Len()
	f.patchBranch19(phaseZeroExit, restore)
	f.patchBranch19(swappedExit, restore)
	f.patchBranch19(canonicalExit, complete)
	f.patchBranch26(skipRestore, complete)
	f.stats.peep("scalar-loop-unroll-remainder-latch")
	return true
}
