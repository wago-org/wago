// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import "os"

var conditionalNegationEnabled = os.Getenv("WAGO_ARM64_NO_CONDITIONAL_NEGATION") != "1"

func condNegWord(src, dst uint32, cc Cond) uint32 {
	return 0x5a800400 | src<<16 | uint32(cc)<<12 | src<<5 | dst
}

func conditionalNegationJoin(pc int, targets []uint64, entries []int) bool {
	if branchTargeted(targets, pc) {
		return true
	}
	for _, off := range entries {
		if off == pc {
			return true
		}
	}
	return false
}

func (f *fn) foldConditionalNegation(b []byte, candidates []int, targets []uint64, entries []int) {
	for _, pc := range candidates {
		word := rdWord(b, pc)
		if word&0xffffffe0 == 0xd2800000 {
			f.foldNegatedSelect(b, pc, word&31, targets, entries)
		} else if word&0xfffffc00 == 0x131f7c00 {
			f.foldComparedAbsoluteValue(b, pc, word, targets, entries)
		}
	}
}

func (f *fn) foldNegatedSelect(b []byte, pc int, temp uint32, targets []uint64, entries []int) {
	neg := rdWord(b, pc+4)
	input := neg >> 16 & 31
	if temp == 31 || input == 31 || temp == input ||
		neg != 0x4b000000|input<<16|temp<<5|temp ||
		conditionalNegationJoin(pc+4, targets, entries) {
		return
	}
	for at := pc + 8; at+4 <= len(b) && at <= pc+20; at += 4 {
		if conditionalNegationJoin(at, targets, entries) {
			return
		}
		word := rdWord(b, at)
		if word&0xffe00c00 == 0x1a800000 {
			left, right, dst := word>>5&31, word>>16&31, word&31
			cc := Cond(word >> 12 & 15)
			if left == temp && right == input {
				left, right = right, left
				cc = invertCond(cc)
			}
			if left == input && right == temp && cc < 14 &&
				(dst == temp || spanExitClobbersDeadWith(b, at+4, 1<<temp, f.relocs, nativeLoopConstExitWord)) {
				wrWord(b, pc, nopWord)
				wrWord(b, pc+4, nopWord)
				wrWord(b, at, condNegWord(input, dst, cc))
				f.stats.peep("conditional-negation-select")
				return
			}
		}
		x, ok := guardedIntegerWord(word)
		if !ok || x.reads&(1<<temp) != 0 || x.writes&(1<<temp|1<<input) != 0 {
			return
		}
	}
}

func (f *fn) foldComparedAbsoluteValue(b []byte, pc int, shift uint32, targets []uint64, entries []int) {
	input, sign := shift>>5&31, shift&31
	xor, sub := rdWord(b, pc+4), rdWord(b, pc+8)
	tmp, dst := xor&31, sub&31
	if input == 31 || sign == 31 || tmp == 31 || sign == input || sign == tmp ||
		xor != 0x4a000000|sign<<16|input<<5|tmp &&
			xor != 0x4a000000|input<<16|sign<<5|tmp ||
		sub != 0x4b000000|sign<<16|tmp<<5|dst ||
		conditionalNegationJoin(pc+4, targets, entries) || conditionalNegationJoin(pc+8, targets, entries) {
		return
	}
	// The previous CMP against zero supplies the exact sign of the
	// unmodified input. Reusing NZCV also preserves downstream flag users.
	for at := pc - 4; at >= 0 && at >= pc-12; at -= 4 {
		if conditionalNegationJoin(at+4, targets, entries) {
			return
		}
		word := rdWord(b, at)
		if word == 0x7100001f|input<<5 {
			dirty := (uint32(1)<<sign | uint32(1)<<tmp) &^ (uint32(1) << dst)
			if dirty != 0 && !spanExitClobbersDeadWith(b, pc+12, dirty, f.relocs, nativeLoopConstExitWord) {
				return
			}
			wrWord(b, pc, nopWord)
			wrWord(b, pc+4, nopWord)
			wrWord(b, pc+8, condNegWord(input, dst, invertCond(condS)))
			f.stats.peep("compared-absolute-value")
			return
		}
		x, ok := guardedIntegerWord(word)
		if !ok || x.flags || x.writes&(1<<input) != 0 {
			return
		}
	}
}
