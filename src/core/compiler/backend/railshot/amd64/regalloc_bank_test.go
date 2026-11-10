// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

import (
	"testing"

	encoderamd64 "github.com/wago-org/wago/src/core/encoder/amd64"
)

func TestGPAllocatorPreservesXMMValues(t *testing.T) {
	for _, typ := range []machineType{mtF32, mtF64, mtV128, mtCustom} {
		for r := Reg(0); r < 16; r++ {
			f := &fn{a: &encoderamd64.Asm{}, s: newStack()}
			// All GP registers are occupied outside the operand stack.
			for _, gp := range gpAlloc {
				f.regUser[gp] = &elem{}
			}
			e := f.pushFReg(r, typ)
			if got := f.allocRegOrNone(0); got != regNone {
				t.Fatalf("type %v XMM%d: GP allocator returned %v", typ, r, got)
			}
			if e.st.kind != stReg || e.st.reg != r || len(f.a.B) != 0 {
				t.Fatalf("type %v XMM%d: GP allocation changed an XMM value", typ, r)
			}
		}
	}
}
