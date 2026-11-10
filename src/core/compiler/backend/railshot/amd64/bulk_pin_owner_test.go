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

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestBulkBoundsCheckPreservesCallerPins(t *testing.T) {
	for _, alreadyPinned := range []bool{false, true} {
		name := "temporary"
		if alreadyPinned {
			name = "caller-owned"
		}
		t.Run(name, func(t *testing.T) {
			sc := newScratch()
			sc.asm.B = make([]byte, 0, 1024)
			f := fn{a: sc.asm, sc: sc, s: sc.stack, m: &wasm.Module{Memories: []wasm.MemType{{}}}, memSizeReg: regNone}
			f.pinned = maskOf(R11)
			if alreadyPinned {
				f.pinned = f.pinned.add(R10)
			}
			want := f.pinned
			f.bulkBoundsCheck(R10, 16, 0)
			if f.pinned != want {
				t.Fatalf("pins = %#x, want caller pins %#x", f.pinned, want)
			}
		})
	}
}
