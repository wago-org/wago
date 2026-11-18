// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import "github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"

func linearSpanWord(w uint32) (x guardedIntegerEffects, ok bool) {
	if w&0xffe0fc00 == 0xbc606800 || w&0xffe0fc00 == 0xfc606800 || w&0xffe0fc00 == 0xbc206800 || w&0xffe0fc00 == 0xfc206800 {
		x.reads = (uint32(1)<<(w>>5&31) | uint32(1)<<(w>>16&31)) & 0x7fffffff
		return x, true
	}
	switch w & 0xffc00000 {
	case 0xbd000000, 0xbd400000, 0xfd000000, 0xfd400000:
		x.reads = uint32(1) << (w >> 5 & 31) & 0x7fffffff
		return x, true
	}
	switch w & 0xffa0fc00 {
	case 0x1e200800, 0x1e202800, 0x1e203800:
		return x, true // scalar MUL/ADD/SUB
	}
	return guardedIntegerWord(w)
}

func (f *fn) spanProfileDirectories() ([]shared.NativeSourceRange, []shared.NativeCodeSite) {
	if !profileEnabled || f.stats == nil || !f.stats.RecordSources {
		return nil, nil
	}
	return append([]shared.NativeSourceRange{}, f.stats.SourceRanges...), append([]shared.NativeCodeSite{}, f.stats.CodeSites...)
}
func (f *fn) copySpanProfileWord(source []shared.NativeSourceRange, sites []shared.NativeCodeSite, old, new int) {
	if !profileEnabled || f.stats == nil || !f.stats.RecordSources {
		return
	}
	for _, r := range source {
		if r.Offset <= uint64(old) && r.Offset+r.Size >= uint64(old+4) {
			r.Offset, r.Size = uint64(new), 4
			f.stats.SourceRanges = append(f.stats.SourceRanges, r)
			break
		}
	}
	for _, r := range sites {
		if r.Offset <= uint64(old) && r.Offset+r.Size >= uint64(old+4) {
			r.Offset, r.Size = uint64(new), 4
			f.stats.CodeSites = append(f.stats.CodeSites, r)
			break
		}
	}
}

// Exit analysis also understands ordinary frame loads/stores and unwind SP
// copies. Unknown instructions and unresolved call/tail relocations decline.
func linearSpanExitWord(w uint32) (x guardedIntegerEffects, ok bool) {
	if x, ok = linearSpanWord(w); ok {
		return
	}
	if (w&0x3b000000 == 0x39000000 || w&0x3b200c00 == 0x38000000) && w&(1<<26) == 0 && w>>22&3 <= 1 {
		x = guardedIntegerEffects{reads: uint32(1) << (w >> 5 & 31) & 0x7fffffff}
		if w&(1<<22) != 0 {
			x.writes = uint32(1) << (w & 31) & 0x7fffffff
		} else {
			x.reads |= uint32(1) << (w & 31) & 0x7fffffff
		}
		return x, true
	}
	if w&0xffc00000 == 0x91000000 { // ADD-immediate, including an unwind SP copy.
		return guardedIntegerEffects{reads: uint32(1) << (w >> 5 & 31) & 0x7fffffff, writes: uint32(1) << (w & 31) & 0x7fffffff}, true
	}
	return x, false
}

func spanExitClobbersDeadWith(b []byte, pc int, dirty uint32, relocs []callReloc, effects func(uint32) (guardedIntegerEffects, bool)) bool {
	type state struct {
		pc    int
		dirty uint32
	}
	var queue [64]state
	queue[0] = state{pc, dirty}
	count, head, steps := 1, 0, 0
	for head < count {
		s := queue[head]
		head++
		for s.dirty != 0 {
			steps++
			if steps > 512 || s.pc < 0 || s.pc+4 > len(b) {
				return false
			}
			for _, r := range relocs {
				if int(r.at) == s.pc {
					return false
				}
			}
			w := rdWord(b, s.pc)
			if w == 0xd65f03c0 {
				// These original scratch writes already belong to the callee's clobber
				// surface. Result and fixed ABI registers cannot be discarded at RET.
				if s.dirty & ^uint32(0xfe00) != 0 {
					return false
				}
				break
			}
			if to, branch := branchTarget(s.pc, w); branch {
				if w&0xfc000000 == 0x94000000 || to < 0 || to >= len(b) {
					return false
				}
				if (w&0x7e000000 == 0x34000000 || w&0x7e000000 == 0x36000000) && s.dirty&(uint32(1)<<(w&31)&0x7fffffff) != 0 {
					return false
				}
				seen := false
				for _, prev := range queue[:count] {
					if prev.pc == to && prev.dirty == s.dirty {
						seen = true
						break
					}
				}
				if !seen {
					if count == len(queue) {
						return false
					}
					queue[count] = state{to, s.dirty}
					count++
				}
				if w&0xfc000000 == 0x14000000 {
					break
				}
				s.pc += 4
				continue
			}
			x, ok := effects(w)
			if !ok || x.reads&s.dirty != 0 {
				return false
			}
			s.dirty &^= x.writes
			s.pc += 4
		}
	}
	return true
}
