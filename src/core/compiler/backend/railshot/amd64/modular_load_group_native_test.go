// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && amd64

package amd64

import (
	"encoding/binary"
	"math/rand"
	"testing"

	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
	"github.com/wago-org/wago/src/core/runtime"
)

// Exercise the emitted native check with modeled memory sizes, including 4 GiB,
// without mapping or touching a 4-GiB guest memory.
func TestModularLoadGroupNativeCheck(t *testing.T) {
	for _, group := range []modularLoadGroup{
		{first: 16, low: 0, high: 16, valid: true},
		{first: 28, low: 4, high: 32, valid: true},
		{first: 252, low: 0, high: 252, valid: true},
	} {
		a := &encoder.Asm{}
		a.Load32(R8, RDI, 0)
		a.Load64(R15, RDI, 8)
		f := &fn{a: a, sc: newScratch(), s: newStack(), memSizeReg: R15, reserved: maskOf(RDI, RCX, R15)}
		f.checkModularLoadGroup(R8, group)
		a.MovImm32(RAX, 1)
		done := a.JmpPlaceholder()
		failure := a.Len()
		for _, site := range f.sc.trapSites[trapMemOOB] {
			a.PatchRel32(int(site.branch), failure)
		}
		a.Xor32(RAX, RAX)
		a.PatchRel32(done, a.Len())
		a.Store32(RCX, 0, RAX)
		a.Ret()
		eng, err := runtime.NewEngine()
		if err != nil {
			t.Fatal(err)
		}
		jm, err := runtime.NewJobMemory(65536)
		if err != nil {
			t.Fatal(err)
		}
		ar, err := runtime.NewArena(4096)
		if err != nil {
			t.Fatal(err)
		}
		code, entry, err := runtime.MapCode(a.B)
		if err != nil {
			t.Fatal(err)
		}
		args, result, trap := ar.Alloc(16), ar.Alloc(8), ar.Alloc(runtime.TrapBufferBytes)
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
			binary.LittleEndian.PutUint32(args, base+uint32(group.first))
			binary.LittleEndian.PutUint64(args[8:], memSize)
			if err := eng.Call(entry, args, jm.LinearMemory(), trap, result); err != nil {
				t.Fatal(err)
			}
			got := binary.LittleEndian.Uint32(result) != 0
			if got != want {
				t.Fatalf("group=%+v base=%x size=%d got=%v want=%v", group, base, memSize, got, want)
			}
		}
		runtime.Unmap(code)
		ar.Close()
		jm.Close()
		eng.Close()
	}
}
