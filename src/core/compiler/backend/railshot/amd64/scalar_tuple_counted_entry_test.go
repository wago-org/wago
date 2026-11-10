// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"bytes"
	"encoding/binary"
	"testing"

	x64 "github.com/wago-org/wago/src/core/encoder/amd64"
)

func TestTupleCountedEntryUsesOnlyRecordedPadding(t *testing.T) {
	for _, counter := range []Reg{RAX, R12, R15} {
		for _, wide := range []bool{false, true} {
			for padding := 0; padding <= 32; padding++ {
				const start = 4
				scalar := start + padding
				group := scalar + 32
				original := append([]byte{0x41, 0x83, 0xc1, 1}, bytes.Repeat([]byte{0x90}, padding+48)...)
				a := x64.Asm{B: append([]byte(nil), original...)}
				f := fn{a: &a, ctrl: []ctrlFrame{{kind: cfLoop, paramN: 3, controlSite: scalar}}, loopLocalParams: loopLocalParamHomes{depth: 1, entryPadStart: start}}
				cmpBytes := 3
				if wide || counter >= 8 {
					cmpBytes++
				}
				guardBytes := cmpBytes + 6
				ok := f.tryTupleCountedInitialGroup(counter, wide, scalar, group)
				if ok != (padding >= guardBytes) {
					t.Fatalf("counter=%d wide=%t padding=%d admitted=%t", counter, wide, padding, ok)
				}
				if !ok {
					if !bytes.Equal(a.B, original) {
						t.Fatal("rejected entry changed code")
					}
					continue
				}
				if !bytes.Equal(a.B[:start], original[:start]) || !bytes.Equal(a.B[scalar:], original[scalar:]) {
					t.Fatal("entry guard changed an instruction outside its recorded padding")
				}
				jump := start + cmpBytes
				if a.B[jump] != 0x0f || a.B[jump+1] != 0x83 {
					t.Fatal("initial group guard is not unsigned >=")
				}
				if target := jump + 6 + int(int32(binary.LittleEndian.Uint32(a.B[jump+2:]))); target != group {
					t.Fatalf("entry guard targets %d want=%d", target, group)
				}
				for p := start + guardBytes; p < scalar; p++ {
					if a.B[p] != 0x90 {
						t.Fatal("remaining entry padding is not complete NOP instructions")
					}
				}
			}
		}
	}
	for _, variant := range []string{"non-nop", "partial-nop", "stale-frame", "missing-homes", "outside-code"} {
		a := x64.Asm{B: bytes.Repeat([]byte{0x90}, 64)}
		f := fn{a: &a, ctrl: []ctrlFrame{{kind: cfLoop, paramN: 3, controlSite: 16}}, loopLocalParams: loopLocalParamHomes{depth: 1, entryPadStart: 0}}
		group := 32
		switch variant {
		case "non-nop":
			a.B[8] = 0xcc
		case "partial-nop":
			a.B[15] = 0x66
		case "stale-frame":
			f.ctrl[0].controlSite++
		case "missing-homes":
			f.loopLocalParams.depth = 0
		case "outside-code":
			group = 80
		}
		before := append([]byte(nil), a.B...)
		if f.tryTupleCountedInitialGroup(R12, false, 16, group) || !bytes.Equal(a.B, before) {
			t.Fatalf("unproved %s padding admitted or changed", variant)
		}
	}
}
