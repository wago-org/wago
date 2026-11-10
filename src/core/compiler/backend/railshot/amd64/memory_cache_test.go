// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/amd64"
)

// Check header widths at the native emission boundary.
func TestIndexedMemoryReadsBackingHeaders(t *testing.T) {
	for _, size := range []bool{false, true} {
		sc := newScratch()
		f := fn{a: sc.asm, sc: sc, s: sc.stack, m: &wasm.Module{Memories: make([]wasm.MemType, 2)}, memSizeReg: regNone}
		if size {
			r := wasm.ReaderFrom([]byte{1})
			if err := f.memorySize(&r); err != nil {
				t.Fatal(err)
			}
		} else {
			f.absoluteBulkAddr(1, RDI, RCX)
		}
		found := false
		for dst := Reg(0); dst < 16; dst++ {
			for base := Reg(0); base < 16; base++ {
				var want enc.Asm
				if size {
					want.Load32(dst, base, -bdCurPages)
				} else {
					want.Load64(dst, base, -bdCurBytes)
				}
				found = found || bytes.Contains(f.a.B, want.B)
			}
		}
		if !found {
			t.Fatalf("indexed size=%t did not read its backing header: %x", size, f.a.B)
		}
	}
}

func TestIndexedGrowReloadsPrimaryBound(t *testing.T) {
	sc := newScratch()
	f := fn{a: sc.asm, sc: sc, s: sc.stack, m: &wasm.Module{Memories: make([]wasm.MemType, 2)}, memSizeReg: R15, reserved: maskOf(R15)}
	f.s.pushValue(storage{kind: stConst, typ: mtI32, cval: 1})
	r := wasm.ReaderFrom([]byte{1})
	if err := f.memoryGrow(&r); err != nil {
		t.Fatal(err)
	}
	var want enc.Asm
	want.Load64(R15, RBX, -bdCurBytes)
	if !bytes.Contains(f.a.B, want.B) {
		t.Fatal("indexed grow did not reload the primary backing bound")
	}
}
