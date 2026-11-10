// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import (
	"encoding/binary"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// unrollScalarPretest copies a bounded register-only loop with one exit. The
// exit must be a proved pending block edge or a local-only cold tuple exit.
// Every copied edge is registered with that same owner before its end is
// resolved; no already-patched or unknown PC-relative instruction is copied.
func (f *fn) unrollScalarPretest(fr *ctrlFrame, after ...*wasm.Reader) bool {
	if !f.opt(optScalarLoopUnroll) || !f.hasLoop || profileEnabled || f.interruptible || f.moduleEH || f.gcFrameRoots != nil || len(f.customInstructions) != 0 || f.policy.CompactNative || f.a.ReserveBranch19 || f.stats != nil && f.stats.RecordSources {
		return false
	}
	budget := f.newScalarLoopMetadataBudget()
	loopIndex := len(f.ctrl)
	for i := range f.ctrl {
		if !budget.take(1) {
			return false
		}
		if &f.ctrl[i] == fr {
			loopIndex = i
			break
		}
	}
	directParams := loopIndex < len(f.ctrl) && f.loopLocalParams.depth == uint32(loopIndex+1)
	if fr.paramN != 0 && !directParams {
		return false
	}
	start, end := fr.controlSite, f.a.Len()
	if start < 0 || end-start < 8 || end-start > 32 || (end-start)&3 != 0 {
		return false
	}
	var words [8]uint32
	n := (end - start) / 4
	exit := -1
	for i := 0; i < n; i++ {
		word := binary.LittleEndian.Uint32(f.a.B[start+4*i:])
		words[i] = word
		bcond := word&0xff000010 == 0x54000000 && word&15 < 14
		cbz := word&0x7e000000 == 0x34000000
		if (bcond || cbz) && word&0x00ffffe0 == 0 {
			if exit != -1 {
				return false
			}
			exit = i
			continue
		}
		rd, ok := scalarLoopWord(word)
		if !ok || rd != regNone && (f.reserved.has(rd) || rd == X18 || rd == X26 || rd == X28 || rd == Reg(29) || rd == Reg(30)) {
			return false
		}
	}
	if exit < 0 {
		return false
	}
	site := start + 4*exit
	var owner *ctrlFrame
	var coldCode []byte
	coldExit := false
	for i := range f.ctrl {
		if !budget.take(1) {
			return false
		}
		candidate := &f.ctrl[i]
		if i > loopIndex && directParams && candidate.isColdTupleLoopExit() {
			edges := f.frameColdEdges(candidate)
			if !budget.take(len(edges)) {
				return false
			}
			for _, edge := range edges {
				if edge.site == site {
					if owner != nil {
						return false
					}
					owner, coldCode, coldExit = candidate, edge.code, true
				}
			}
			continue
		}
		if i >= loopIndex || candidate.kind != cfBlock && candidate.kind != cfIf || candidate.branchN != 0 {
			continue
		}
		first, second, overflow := f.frameEndSites(candidate)
		if !budget.take(2 + len(overflow)) {
			return false
		}
		matches := func(packed uint32) bool {
			return packed&frameEndConditional != 0 && int(packed&^frameEndConditional)-1 == site
		}
		found := matches(first) || matches(second)
		for _, packed := range overflow {
			found = found || matches(packed)
		}
		if found {
			if owner != nil {
				return false
			}
			owner = candidate
		}
	}

	if owner == nil {
		return false
	}
	if !coldExit && f.unrollScalarCounter(start, words[:n], exit, owner, after...) {
		return true
	}
	pair, rotate := coldLoopPair{}, false
	if coldExit {
		pair, rotate = f.findColdLoopPair(words[:n], exit)
	}
	// Retain the original body and its original pending edge. New copies use the
	// same register homes and preserve an exit check in every iteration.
	firstPhase := 1
	if rotate {
		f.a.B = f.a.B[:start]
		firstPhase = 0
	}
	for phase := firstPhase; phase < 4; phase++ {
		a, b := pair.a, pair.b
		if phase&1 != 0 {
			a, b = b, a
		}
		for i := 0; i < n; i++ {
			if i == exit && phase != 0 {
				if coldExit {
					code := coldCode
					if rotate && phase&1 != 0 {
						code = pair.coldRestore(code)
					}
					f.appendFrameColdEdge(owner, coldEdge{site: f.a.Len(), code: code})
				} else {
					f.appendFrameEnd(owner, f.a.Len(), true)
				}
			}
			word := words[i]
			if rotate {
				if i == pair.save {
					continue
				}
				if i == pair.assign {
					a, b = b, a
					continue
				}
				if i == exit && word&0x7e000000 == 0x34000000 {
					reg := Reg(word & 31)
					if reg == pair.a {
						word = word&^31 | uint32(a)
					} else if reg == pair.b {
						word = word&^31 | uint32(b)
					}
				} else {
					word = pair.rewriteReads(word, a, b)
					if i == pair.sum {
						word = word&^31 | uint32(a)
					}
				}
			}
			f.a.B = binary.LittleEndian.AppendUint32(f.a.B, word)
		}
	}
	if rotate {
		f.emitScalarPretestLatch(start, words[:n], exit, owner, coldCode)
	} else {
		f.patchBranch26(f.a.Branch(), start)
	}
	f.stats.peep("scalar-loop-unroll-pretest")
	if coldExit {
		f.stats.peep("scalar-loop-unroll-cold")
	}
	if rotate {
		f.stats.peep("scalar-loop-unroll-pair")
	}
	return true
}

// The original entry check guards the first body. After four bodies, repeat
// that check and branch directly to the already-guarded body on continuation.
// For an alternating pair this removes the separate unconditional group
// backedge without skipping condition-prefix work or any iteration's exit
// check. Other scalar loops retain their established backedge layout. The
// four-copy group has restored canonical register roles before this prefix.
func (f *fn) emitScalarPretestLatch(start int, words []uint32, exit int, owner *ctrlFrame, coldCode []byte) {
	for _, word := range words[:exit] {
		f.a.B = binary.LittleEndian.AppendUint32(f.a.B, word)
	}
	word := words[exit]
	if word&0x7e000000 == 0x34000000 {
		word ^= 1 << 24 // CBZ <-> CBNZ, retaining the original operand width.
	} else {
		word ^= 1 // B.cond's inverse condition.
	}
	site := f.a.Len()
	f.a.B = binary.LittleEndian.AppendUint32(f.a.B, word)
	f.patchBranch19(site, start+4*(exit+1))
	// A false latch condition is an exit after four complete iterations. Cold
	// tuple payloads contain the same canonical result stores as the first edge.
	f.a.B = append(f.a.B, coldCode...)
	f.appendFrameEnd(owner, f.a.Branch(), false)
	f.stats.peep("scalar-loop-unroll-latch")
}

// unrollScalarCounter batches a unit increment whose local is neither read nor
// written elsewhere in the loop. An exit after k complete iterations restores
// base+k in a cold stub. The original phase-zero exit remains at its old site.
func (f *fn) unrollScalarCounter(start int, words []uint32, exit int, owner *ctrlFrame, after ...*wasm.Reader) bool {
	increment := -1
	for i, word := range words {
		rd := Reg(word & 31)
		if i <= exit || word&0x7fc00000 != 0x11000000 || word>>10&0xfff != 1 || rd != Reg(word>>5&31) || !f.pinnedLocalMask.has(rd) {
			continue
		}
		independent := true
		for j, other := range words {
			if j == i {
				continue
			}
			if j == exit {
				if other&0x7e000000 == 0x34000000 && Reg(other&31) == rd {
					independent = false
				}
				continue
			}
			destination, _ := scalarLoopWord(other)
			if destination == rd {
				independent = false
				break
			}
			fields := scalarLoopSources(other)
			for _, shift := range []uint{5, 10, 16} {
				if fields&(1<<shift) != 0 && Reg(other>>shift&31) == rd {
					independent = false
				}
			}
		}
		if independent {
			if increment != -1 {
				return false
			}
			increment = i
		}
	}
	if increment < 0 {
		return false
	}
	if f.unrollRemainderPair(start, words, exit, increment, owner, after...) {
		return true
	}
	update := words[increment]
	f.a.B = f.a.B[:start]
	var exits [3]int
	for phase := 0; phase < 4; phase++ {
		for i, word := range words {
			if i == increment {
				continue
			}
			if i == exit && phase != 0 {
				exits[phase-1] = f.a.Len()
			}
			f.a.B = binary.LittleEndian.AppendUint32(f.a.B, word)
		}
	}
	// Integer ADD retains the original operand width, including modular wrapping.
	f.a.B = binary.LittleEndian.AppendUint32(f.a.B, update&^uint32(0xfff<<10)|4<<10)
	f.patchBranch26(f.a.Branch(), start)
	for phase, site := range exits {
		f.patchBranch19(site, f.a.Len())
		f.a.B = binary.LittleEndian.AppendUint32(f.a.B, update&^uint32(0xfff<<10)|uint32(phase+1)<<10)
		f.appendFrameEnd(owner, f.a.Branch(), false)
	}
	f.stats.peep("scalar-loop-unroll-pretest")
	f.stats.peep("scalar-loop-unroll-counter")
	return true
}
