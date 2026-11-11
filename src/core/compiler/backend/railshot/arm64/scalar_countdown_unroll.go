// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import "os"

var scalarLoopUnrollLayoutOnly = os.Getenv("WAGO_ARM64_SCALAR_LOOP_UNROLL_LAYOUT_ONLY") == "1"

// No control, NZCV dependency, native pointer or SP update is cloned. Every
// admitted word has the same architectural effects on every iteration.
func scalarUnrollWord(w uint32) (writes uint32, ok bool) {
	if w&0x3b000000 == 0x39000000 && w&(1<<26) == 0 && w>>22&3 <= 1 {
		base := w >> 5 & 31
		if base != 31 && base != uint32(linMemReg) {
			return 0, false
		}
		if w&(1<<22) != 0 {
			return (uint32(1) << (w & 31)) & 0x7fffffff, true
		}
		return 0, true
	}
	if (w&0x3fe00c00 == 0x38200800 || w&0x3fe00c00 == 0x38600800) && w>>5&31 == uint32(linMemReg) {
		if w&(1<<22) != 0 {
			return (uint32(1) << (w & 31)) & 0x7fffffff, true
		}
		return 0, true
	}
	x, ok := guardedIntegerWord(w)
	return x.writes, ok && !x.flags && !x.readsFlags
}

type scalarUnrollLoop struct {
	start, end, back int
	counter          uint32
}

func matchScalarUnroll(b []byte, back, n int) (g scalarUnrollLoop, ok bool) {
	if back < 12 || back+4 > n {
		return
	}
	branch := rdWord(b, back)
	if branch&0xff00001f != 0x54000001 {
		return
	}
	start, valid := branchTarget(back, branch)
	if !valid || start < 4 || back-start < 12 || back-start > 60 {
		return
	}
	sub := rdWord(b, back-4)
	counter := sub & 31
	if sub&0xfffffc00 != 0x71000400 || sub>>5&31 != counter || counter == 31 {
		return
	}
	end := back + 4
	entry := start - 4
	if rdWord(b, entry) == nopWord && entry >= 4 {
		entry -= 4
	}
	guard := rdWord(b, entry)
	exit, valid := branchTarget(entry, guard)
	if !valid || guard&0xff00001f != 0x34000000|counter || exit < end || exit > end+4 {
		return
	}
	if exit != end && (end+4 > n || rdWord(b, end) != nopWord) {
		return
	}
	for at := start; at < back-4; at += 4 {
		writes, valid := scalarUnrollWord(rdWord(b, at))
		if !valid || writes&(uint32(1)<<counter) != 0 || writes&(uint32(1)<<linMemReg) != 0 {
			return
		}
	}
	return scalarUnrollLoop{start, exit, back, counter}, true
}

// Separate from size-preserving peepholes: the original body entry redirects
// to a bounded appended loop. A parity peel leaves an even count, allowing two
// exact iterations per backedge. The final SUBS preserves the original exit
// counter representation and NZCV. The original zero-entry guard remains.
func (f *fn) finalizeScalarLoopUnroll(entries ...int) {
	if !f.opt(optScalarLoopUnroll) || f.moduleEH || f.interruptible || f.guardMode || f.threadedMemory0 || f.memoryAddr64(0) || f.opaqueFragments || len(f.customInstructions) != 0 || f.tracksGCFrameRoots() {
		return
	}
	n := len(f.a.B) &^ 3
	if n > 64<<10 {
		return
	}
	targets := make([]uint64, ((n/4)+63)/64)
	for at := 0; at < n; at += 4 {
		w := rdWord(f.a.B, at)
		if isIndirectBranch(w) {
			return
		}
		if t, ok := branchTarget(at, w); ok {
			branchTargetAdd(targets, t, n)
		}
	}
	for back := 12; back < n; back += 4 {
		g, ok := matchScalarUnroll(f.a.B, back, n)
		if !ok {
			continue
		}
		valid := true
		for at := g.start + 4; at < g.end; at += 4 {
			if branchTargeted(targets, at) {
				valid = false
			}
		}
		for _, entry := range entries {
			if entry >= g.start && entry < g.end {
				valid = false
			}
		}
		if !valid {
			continue
		}
		body := append([]byte(nil), f.a.B[g.start:g.back]...)
		pre := f.a.Len()
		put := func(w uint32) int {
			at := len(f.a.B)
			f.a.B = append(f.a.B, byte(w), byte(w>>8), byte(w>>16), byte(w>>24))
			return at
		}
		parity := put(0x36000000 | g.counter) // TBZ Wcounter,#0,bulk.
		f.a.B = append(f.a.B, body...)
		oddExit := put(0x54000000)
		for f.a.Len()%32 != 0 {
			put(nopWord)
		}
		bulk := f.a.Len()
		f.a.B = append(f.a.B, body...)
		firstSub := f.a.Len() - 4
		wrWord(f.a.B, firstSub, rdWord(f.a.B, firstSub)&^uint32(1<<29))
		second := f.a.Len()
		f.a.B = append(f.a.B, body...)
		latch := put(0x54000001)
		exit := put(0x14000000)
		wrWord(f.a.B, g.start, 0x14000000)
		f.recordPCRelative(g.start)
		f.patchBranch26(g.start, pre)
		for _, site := range []int{parity, oddExit, latch, exit} {
			f.recordPCRelative(site)
		}
		if scalarLoopUnrollLayoutOnly {
			// Same addresses/length as the unroll, but run one body per backedge at
			// the same aligned entry. Exit branches execute only once per invocation.
			wrWord(f.a.B, parity, 0x14000000)
			f.patchBranch26(parity, bulk)
			wrWord(f.a.B, firstSub, rdWord(f.a.B, firstSub)|1<<29)
			wrWord(f.a.B, second, 0x54000001)
			f.recordPCRelative(second)
			f.patchBranch19(second, bulk)
			wrWord(f.a.B, second+4, 0x14000000)
			f.recordPCRelative(second + 4)
			f.patchBranch26(second+4, g.end)
			f.stats.peep("scalar-loop-unroll-layout")
		} else {
			// TBZ has imm14, unlike the other conditional branches.
			wrWord(f.a.B, parity, 0x36000000|g.counter|uint32((bulk-parity)/4)<<5)
			f.stats.peep("scalar-loop-unroll")
		}
		f.patchBranch19(oddExit, g.end)
		f.patchBranch19(latch, bulk)
		f.patchBranch26(exit, g.end)
		back = g.end - 4
	}
}
