// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import (
	"fmt"
	"testing"
)

func TestBulkCopyScratchRegisters(t *testing.T) {
	for _, pressure := range []int{0, 12, 27, 28} {
		t.Run(fmt.Sprintf("pins=%d", pressure), func(t *testing.T) {
			f := fn{fpinned: maskOf(16, 17)}
			for r := 0; r < pressure; r++ {
				f.fpinnedLocalMask = f.fpinnedLocalMask.add(Reg(r))
			}
			f.fconsts = []floatConstReg{{reg: 30}}
			f.vconsts = []v128ConstReg{{reg: 31}}
			before := f.blockedFRegs(0)
			regs := f.bulkCopyRegs()
			var seen regMask
			for _, r := range regs {
				if r == regNone {
					continue
				}
				if before.has(r) || seen.has(r) {
					t.Fatalf("scratch %v conflicts with owners %#x", regs, before)
				}
				seen = seen.add(r)
			}
			if f.blockedFRegs(0) != before {
				t.Fatal("scratch selection changed another owner's reservation")
			}
			if pressure >= 27 && (regs[0] == regNone || regs[1] == regNone || regs[2] != regNone || regs[3] != regNone) {
				t.Fatalf("pressure fallback = %v; want two scratch registers", regs)
			}
			if n := testing.AllocsPerRun(100, func() { f.bulkCopyRegs() }); n != 0 {
				t.Fatalf("scratch selection allocated %g objects", n)
			}
		})
	}
}
