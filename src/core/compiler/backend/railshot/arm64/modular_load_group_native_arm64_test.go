// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (linux || darwin) && arm64

package arm64

import (
	"math/rand"
	"testing"

	encoder "github.com/wago-org/wago/src/core/encoder/arm64"
	"github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/arm64spike"
)

// Exercise the native check with modeled sizes, including full i32 memory.
func TestModularLoadGroupNativeCheck(t *testing.T) {
	for _, group := range []modularLoadGroup{
		{first: 16, low: 0, high: 16},
		{first: 28, low: 4, high: 32},
		{first: 252, low: 0, high: 252},
	} {
		a := &encoder.Asm{}
		f := &fn{a: a, sc: newScratch(), s: newStack(), memSizeReg: X1, reserved: maskOf(X0, X1)}
		f.checkModularLoadGroup(X0, group)
		a.MovImm64(X0, 1)
		a.Ret()
		failure := a.Len()
		for _, site := range f.sc.trapSites[trapMemOOB] {
			f.patchBranch19(int(site.branch), failure)
		}
		a.MovImm64(X0, 0)
		a.Ret()
		code, entry, err := runtime.MapCode(a.B)
		if err != nil {
			t.Fatal(err)
		}
		defer runtime.Unmap(code)
		rng := rand.New(rand.NewSource(42))
		for i := 0; i < 20000; i++ {
			base := rng.Uint32()
			switch i % 4 {
			case 0:
				base = uint32(i % 300)
			case 1:
				base = ^uint32(0) - uint32(i%300)
			case 2:
				base = 65536 - uint32(i%300)
			}
			sizes := [...]uint64{0, 65536, 2 << 16, 1<<32 - 65536, 1 << 32}
			memSize := sizes[i%len(sizes)]
			want := true
			for off := uint32(group.low); off <= uint32(group.high); off += 4 {
				if uint64(base+off)+4 > memSize {
					want = false
					break
				}
			}
			got := arm64spike.Call2(entry, uintptr(base+uint32(group.first)), uintptr(memSize)) != 0
			if got != want {
				t.Fatalf("group=%+v base=%x size=%d got=%v want=%v", group, base, memSize, got, want)
			}
		}
	}
}
