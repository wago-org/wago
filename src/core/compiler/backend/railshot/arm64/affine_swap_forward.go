// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import "os"

var affineSwapForwardEnabled = os.Getenv("WAGO_ARM64_NO_AFFINE_SWAP_FORWARD") != "1"

type affineSwapHead struct {
	header, load, store, skip, join int
	value                           Reg
}

// Validate an unrolled sequence of swaps between lower indexed words and
// the upper endpoint minus a fixed distance. The branch before each swap
// proves every intervening store is to a word other than the head.
func matchAffineSwapHead(b []byte, pc int) (p affineSwapHead, ok bool) {
	if pc+32 > len(b) {
		return
	}
	cmp, limitBranch := rdWord(b, pc), rdWord(b, pc+4)
	addr, load := rdWord(b, pc+8), rdWord(b, pc+12)
	if cmp&0xffc0001f != 0x7100001f || limitBranch&0xff00001f != 0x54000000|uint32(condAE) ||
		addr&0xffe0fc00 != 0x0b000800 || load&0xffe0fc00 != 0xb8606800 {
		return
	}
	idx, base, edge, value := cmp>>5&31, addr>>5&31, addr&31, load&31
	limit := cmp >> 10 & 4095
	if limit < 4 || limit > 24 || addr>>16&31 != idx || idx == 31 || base == 31 || edge == 31 || value == 31 ||
		idx == base || idx == edge || idx == value || base == edge || base == value || edge == value ||
		(idx == uint32(linMemReg) || base == uint32(linMemReg) || edge == uint32(linMemReg) || value == uint32(linMemReg)) ||
		idx == uint32(X16) || base == uint32(X16) || edge == uint32(X16) || value == uint32(X16) {
		return
	}
	lin := uint32(linMemReg) << 5
	if load != 0xb8606800|edge<<16|lin|value ||
		rdWord(b, pc+16) != 0xb8206800|base<<16|lin|value ||
		rdWord(b, pc+20) != 0xb8206800|edge<<16|lin|idx ||
		rdWord(b, pc+24) != 0x7100001f|3<<10|idx<<5 {
		return
	}
	skip := pc + 28
	first := rdWord(b, skip)
	if first&0xff00001f != 0x54000000|uint32(condL) {
		return
	}
	tail, _ := branchTarget(skip, first)
	join := tail - 4
	if join <= skip || join+4 > len(b) || join-pc > 256 || rdWord(b, join) != 0xb8606800|base<<16|lin|value {
		return
	}
	baseScratch := uint32(X16)
	offset := uint32(1)
	for at := skip + 4; at < join; offset++ {
		if at+32 > join || 2*offset+1 >= limit {
			return
		}
		bias := offset * 4
		nativeBase, left := rdWord(b, at), rdWord(b, at+4)
		sub, right := rdWord(b, at+8), rdWord(b, at+12)
		other, derived := right&31, sub&31
		if nativeBase != 0x8b000000|base<<16|lin|baseScratch ||
			left != 0xb9400000|offset<<10|baseScratch<<5|value ||
			sub != 0x51000000|bias<<10|edge<<5|derived ||
			right != 0xb8606800|derived<<16|lin|other ||
			rdWord(b, at+16) != 0xb9000000|offset<<10|baseScratch<<5|other ||
			rdWord(b, at+20) != 0xb8206800|derived<<16|lin|value ||
			rdWord(b, at+24) != 0x7100001f|(2*offset+3)<<10|idx<<5 ||
			derived == 31 || other == 31 || other == value || other == base || other == edge || other == idx ||
			derived == base || derived == idx || derived == value || derived == baseScratch ||
			other == derived || other == baseScratch || other == uint32(linMemReg) || derived == uint32(linMemReg) {
			return
		}
		next := rdWord(b, at+28)
		dest, _ := branchTarget(at+28, next)
		if dest != join {
			return
		}
		if next&0xff00001f == 0x54000000|uint32(condL) {
			if derived == edge {
				return
			}
			at += 32
			continue
		}
		if next&0xff00001f != 0x54000000|uint32(condNE) || at+60 != join || 2*offset+4 != limit {
			return
		}
		// The last, even-width center swap rotates two i32 words in place.
		start := at + 32
		load64 := rdWord(b, start+8)
		tmp := load64 & 31
		pairOffset := (offset + 1) * 4
		regAddr := uint32(31)<<16 | baseScratch<<5 | tmp
		if tmp == 31 || tmp == baseScratch || tmp == base || tmp == uint32(linMemReg) ||
			rdWord(b, start) != nativeBase ||
			rdWord(b, start+4) != 0x91000000|pairOffset<<10|baseScratch<<5|baseScratch ||
			load64 != 0xf8606800|regAddr ||
			rdWord(b, start+12) != 0x93c08000|tmp<<16|tmp<<5|tmp ||
			rdWord(b, start+16) != nativeBase ||
			rdWord(b, start+20) != rdWord(b, start+4) ||
			rdWord(b, start+24) != 0xf8206800|regAddr {
			return
		}
		at = join
	}
	return affineSwapHead{pc, pc + 12, pc + 16, skip, join, Reg(value)}, true
}

func (f *fn) forwardAffineSwapHeads(b []byte, candidates []int, targets []uint64, entries []int) int {
	hits := 0
	for _, pc := range candidates {
		p, ok := matchAffineSwapHead(b, pc)
		if !ok {
			continue
		}
		safe := true
		for _, entry := range entries {
			safe = safe && (entry <= p.header || entry > p.join)
		}
		for at := p.header + 4; safe && at < p.join; at += 4 {
			safe = !branchTargeted(targets, at)
		}
		for from := 0; safe && from < len(b); from += 4 {
			if target, branch := branchTarget(from, rdWord(b, from)); branch && target == p.join {
				safe = from > p.skip && from < p.join
			}
		}
		if !safe {
			continue
		}
		for _, cache := range [...]Reg{X10, X11, X12, X13, X14, X15} {
			if cache == p.value || !swapHeadRegisterUnused(b, p.header, p.join, cache) ||
				!spanExitClobbersDeadWith(b, p.join+4, 1<<cache, f.relocs, swapHeadEffects, true) {
				continue
			}
			wrWord(b, p.load, rdWord(b, p.load)&^31|uint32(cache))
			wrWord(b, p.store, rdWord(b, p.store)&^31|uint32(cache))
			wrWord(b, p.join, 0x2a0003e0|uint32(cache)<<16|uint32(p.value))
			wrWord(b, p.skip, rdWord(b, p.skip)&^uint32(0x00ffffe0))
			f.patchBranch19(p.skip, p.join)
			hits++
			break
		}
	}
	return hits
}

func swapHeadRegisterUnused(b []byte, start, end int, r Reg) bool {
	for at := start; at < end; at += 4 {
		word := rdWord(b, at)
		if _, branch := branchTarget(at, word); branch {
			continue
		}
		x, ok := swapHeadEffects(word)
		if !ok || (x.reads|x.writes)&(1<<r) != 0 {
			return false
		}
	}
	return true
}

func swapHeadEffects(word uint32) (guardedIntegerEffects, bool) {
	if word&0xffe0fc00 == 0x93c08000 { // ROR Xd, Xn, #32 alias of EXTR
		return guardedIntegerEffects{reads: 1<<(word>>5&31) | 1<<(word>>16&31), writes: 1 << (word & 31)}, true
	}
	return nativeLoopConstExitWord(word)
}
