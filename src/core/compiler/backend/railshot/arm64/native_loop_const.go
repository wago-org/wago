// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import (
	"os"
	"sort"
)

var nativeLoopConstEnabled = os.Getenv("WAGO_ARM64_NO_NATIVE_LOOP_CONST") != "1"
var nativeLoopConstLayoutOnly = os.Getenv("WAGO_ARM64_NATIVE_LOOP_CONST_LAYOUT_ONLY") == "1"

type nativeLoopConstant struct {
	pc, words int
	value     uint64
	reg       Reg
	wide      bool
}

// Only a complete MOVZ/MOVN followed by matching MOVKs is a materialization.
// A standalone MOVK depends on the incoming register and is never invariant.
func nativeLoopConstAt(b []byte, pc, end int) (c nativeLoopConstant, ok bool) {
	if pc < 0 || pc+4 > end || end > len(b) {
		return
	}
	w := rdWord(b, pc)
	if w&0x1f800000 != 0x12800000 || w>>29&3 != 0 && w>>29&3 != 2 || w&31 == 31 {
		return
	}
	c = nativeLoopConstant{pc: pc, words: 1, reg: Reg(w & 31), wide: w>>31 != 0}
	shift := (w >> 21 & 3) * 16
	if !c.wide && shift >= 32 {
		return c, false
	}
	c.value = uint64(w>>5&65535) << shift
	if w>>29&3 == 0 {
		c.value = ^c.value
	}
	for at := pc + 4; at+4 <= end; at += 4 {
		next := rdWord(b, at)
		if next&0xff80001f != (w&0x8000001f)|0x72800000 {
			break
		}
		shift = (next >> 21 & 3) * 16
		if !c.wide && shift >= 32 {
			return c, false
		}
		c.value = c.value&^(uint64(65535)<<shift) | uint64(next>>5&65535)<<shift
		c.words++
		if c.words > 4 {
			return c, false
		}
	}
	if !c.wide {
		c.value = uint64(uint32(c.value))
	}
	return c, c.words >= 2
}

func nativeLoopConstWord(w uint32) (x guardedIntegerEffects, ok bool) {
	// Scalar, non-writeback integer memory operations. They remain in place;
	// every real address and data operand participates in the register proof.
	reg := func(r uint32) uint32 { return uint32(1) << r & 0x7fffffff }
	if w&0x3b000000 == 0x39000000 && w&(1<<26) == 0 && w>>22&3 <= 1 {
		x.reads = reg(w >> 5 & 31)
		if w&(1<<22) != 0 {
			x.writes = reg(w & 31)
		} else {
			x.reads |= reg(w & 31)
		}
		return x, true
	}
	if w&0x3fe00c00 == 0x38200800 || w&0x3fe00c00 == 0x38600800 {
		x.reads = reg(w>>5&31) | reg(w>>16&31)
		if w&(1<<22) != 0 {
			x.writes = reg(w & 31)
		} else {
			x.reads |= reg(w & 31)
		}
		return x, true
	}
	if w&0xffe0fc00 == 0x9bc07c00 || w&0xffe0fc00 == 0x9b407c00 {
		return guardedIntegerEffects{reads: reg(w>>5&31) | reg(w>>16&31), writes: reg(w & 31), wide: true}, true
	}
	return guardedIntegerWord(w)
}

type nativeLoopConstPlan struct {
	start, back int
	constant    nativeLoopConstant
	cache       Reg
	uses        []nativeLoopConstant
}

func nativeLoopConstExitWord(w uint32) (guardedIntegerEffects, bool) {
	if x, ok := nativeLoopConstWord(w); ok {
		return x, true
	}
	return linearSpanExitWord(w)
}

func matchNativeLoopConst(b []byte, back int, targets []uint64, entries []int, relocs []callReloc) (p nativeLoopConstPlan, ok bool) {
	if back < 12 || back+4 > len(b) {
		return
	}
	w := rdWord(b, back)
	start, valid := branchTarget(back, w)
	if w&0xff00001f != 0x54000001 || !valid || start < 0 || back-start < 16 || back-start > 512 {
		return
	}
	p.start, p.back = start, back
	for at := start + 4; at <= back; at += 4 {
		if branchTargeted(targets, at) {
			return p, false
		}
	}
	for _, entry := range entries {
		if entry > start && entry <= back {
			return p, false
		}
	}
	used := uint32(0)
	var exits []int
	for at := start; at < back; at += 4 {
		word := rdWord(b, at)
		if to, branch := branchTarget(at, word); branch {
			if word&0xff000010 != 0x54000000 || to >= start && to <= back || to < 0 || to >= len(b) {
				return p, false
			}
			exits = append(exits, to)
			continue
		}
		x, valid := nativeLoopConstWord(word)
		if !valid {
			return p, false
		}
		used |= x.reads | x.writes
	}
	exits = append(exits, back+4)
	// Limit the lease to the existing caller-scratch clobber surface. Every
	// exit independently proves the original value killed before observation.
	for _, cache := range []Reg{X9, X10, X11, X14, X13, X12} {
		bit := uint32(1) << uint32(cache)
		if used&bit != 0 {
			continue
		}
		dead := true
		for _, exit := range exits {
			if !spanExitClobbersDeadWith(b, exit, bit, relocs, nativeLoopConstExitWord) {
				dead = false
				break
			}
		}
		if dead {
			p.cache = cache
			ok = true
			break
		}
	}
	if !ok {
		return p, false
	}
	best := 0
	for at := start; at < back; at += 4 {
		c, valid := nativeLoopConstAt(b, at, back)
		if !valid || c.words < 3 {
			continue
		}
		var uses []nativeLoopConstant
		saving := 0
		for site := start; site < back; site += 4 {
			x, valid := nativeLoopConstAt(b, site, back)
			if valid && x.value == c.value && x.wide == c.wide {
				uses = append(uses, x)
				saving += x.words - 1
				site += (x.words - 1) * 4
			}
		}
		if len(uses) >= 2 && saving > best {
			best = saving
			p.constant = c
			p.uses = uses
		}
		at += (c.words - 1) * 4
	}
	return p, best >= 4
}

// Lease an otherwise unused native register for exactly one loop. No memory
// operation, arithmetic result, NZCV producer/consumer or side exit is removed.
func (f *fn) finalizeNativeLoopConsts(entries ...int) {
	if !f.opt(optNativeLoopConst) || !f.hasLoop || f.makesCalls || f.moduleEH || f.interruptible || f.guardMode || f.threadedMemory0 || f.memoryAddr64(0) || f.opaqueFragments || f.preserveCallerPins || len(f.customInstructions) != 0 || f.tracksGCFrameRoots() {
		return
	}
	n := len(f.a.B) &^ 3
	if n > 64<<10 {
		return
	}
	targets := make([]uint64, ((n/4)+63)/64)
	knownWrites := uint32(0)
	for at := 0; at < n; at += 4 {
		w := rdWord(f.a.B, at)
		if isIndirectBranch(w) {
			return
		}
		if to, branch := branchTarget(at, w); branch {
			branchTargetAdd(targets, to, n)
		}
		if x, valid := nativeLoopConstWord(w); valid {
			knownWrites |= x.writes
		}
	}
	versions := 0
	for back := 16; back < n && versions < 2; back += 4 {
		p, ok := matchNativeLoopConst(f.a.B[:n], back, targets, entries, f.relocs)
		if !ok || knownWrites&(uint32(1)<<uint32(p.cache)) == 0 {
			continue
		}
		original := append([]byte{}, f.a.B[p.start:p.back+4]...)
		sources, sites := f.spanProfileDirectories()
		pre := f.a.Len()
		put := func(w uint32) int {
			at := f.a.Len()
			f.a.B = append(f.a.B, byte(w), byte(w>>8), byte(w>>16), byte(w>>24))
			return at
		}
		for i := 0; i < p.constant.words; i++ {
			old := p.constant.pc + i*4
			at := put(rdWord(original, old-p.start)&^uint32(31) | uint32(p.cache))
			f.copySpanProfileWord(sources, sites, old, at)
		}
		for f.a.Len()%32 != 0 {
			put(nopWord)
		}
		fast := f.a.Len()
		removed := 0
		for old := p.start; old <= p.back; old += 4 {
			if !nativeLoopConstLayoutOnly {
				found := false
				for _, use := range p.uses {
					if old == use.pc {
						w := uint32(0xaa0003e0) | uint32(p.cache)<<16 | uint32(use.reg)
						if !use.wide {
							w &^= 1 << 31
						}
						at := put(w)
						f.copySpanProfileWord(sources, sites, old, at)
						old += (use.words - 1) * 4
						removed += use.words - 1
						found = true
						break
					}
				}
				if found {
					continue
				}
			}
			w := rdWord(original, old-p.start)
			at := f.a.Len()
			if to, branch := branchTarget(old, w); branch {
				put(w &^ uint32(0x00ffffe0))
				f.recordPCRelative(at)
				if old == p.back {
					to = fast
				}
				f.patchBranch19(at, to)
			} else {
				put(w)
			}
			f.copySpanProfileWord(sources, sites, old, at)
		}
		exit := put(0x14000000)
		f.recordPCRelative(exit)
		f.patchBranch26(exit, p.back+4)
		for range removed {
			put(nopWord)
		}
		wrWord(f.a.B, p.start, 0x14000000)
		f.recordPCRelative(p.start)
		f.patchBranch26(p.start, pre)
		f.stats.peep("native-loop-constant-lease")
		versions++
	}
	if profileEnabled && f.stats != nil && f.stats.RecordSources {
		sort.Slice(f.stats.SourceRanges, func(i, j int) bool { return f.stats.SourceRanges[i].Offset < f.stats.SourceRanges[j].Offset })
		sort.Slice(f.stats.CodeSites, func(i, j int) bool { return f.stats.CodeSites[i].Offset < f.stats.CodeSites[j].Offset })
	}
}
