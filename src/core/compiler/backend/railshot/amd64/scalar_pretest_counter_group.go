// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

import "encoding/binary"

// A trailing unit increment may be delayed only if neither the guard nor the
// rest of the body reads or writes its pinned register. All original exit tests
// remain; partial groups repair the delayed increment on their own exit edges.
func scalarPretestPrivateIncrement(prefix, tail []byte, pins regMask) (body []byte, counter Reg, wide, ok bool) {
	guardReads, guardWrites, guardFlags, valid := scalarLoopBodyEffects(prefix, pins)
	if !valid || !guardFlags {
		return nil, regNone, false, false
	}
	for _, size := range []int{3, 4, 6, 7} {
		if len(tail) <= size {
			continue
		}
		cut := len(tail) - size
		b := tail[cut:]
		p, rex := 0, byte(0)
		if b[0]&0xf0 == 0x40 {
			p, rex = 1, b[0]
		}
		if len(b)-p < 3 || rex&6 != 0 || b[p+1]>>6 != 3 || (b[p+1]>>3)&7 != 0 {
			continue
		}
		reg := Reg(b[p+1]&7) | Reg(rex&1)<<3
		if reg == RSP || !pins.has(reg) || guardReads.has(reg) || guardWrites.has(reg) {
			continue
		}
		if !(b[p] == 0x83 && len(b)-p == 3 && b[p+2] == 1 ||
			b[p] == 0x81 && len(b)-p == 6 && binary.LittleEndian.Uint32(b[p+2:]) == 1) {
			continue
		}
		reads, writes, valid := scalarLoopBodyRegisters(tail[:cut], pins)
		if valid && !reads.has(reg) && !writes.has(reg) {
			return tail[:cut], reg, rex&8 != 0, true
		}
	}
	return nil, regNone, false, false
}

func (f *fn) tryScalarPretestCounterGroup(prefix, tail []byte, condition Cond, start int, owner *ctrlFrame) bool {
	body, counter, wide, ok := scalarPretestPrivateIncrement(prefix, tail, f.pinnedLocalMask)
	if !ok {
		return false
	}
	// The already-emitted first iteration retains its own increment. Delay the
	// other three until their group completes; earlier exits add exactly the
	// number of completed copied bodies (zero, one, or two).
	var partial [2]int
	for phase := 1; phase < 4; phase++ {
		f.a.B = append(f.a.B, prefix...)
		site := f.a.JccPlaceholder(condition)
		if phase == 1 {
			f.frameAddEnd(owner, site)
		} else {
			partial[phase-2] = site
		}
		f.a.B = append(f.a.B, body...)
	}
	f.a.LeaDispW(counter, counter, 3, wide)
	f.a.JmpBack(start)
	for i, site := range partial {
		f.a.PatchRel32(site, f.a.Len())
		// Preserve the original guard flags on every exit edge as well.
		f.a.LeaDispW(counter, counter, int32(i+1), wide)
		f.frameAddEnd(owner, f.a.JmpPlaceholder())
	}
	f.stats.peep("scalar-pretest-private-counter-group")
	return true
}
