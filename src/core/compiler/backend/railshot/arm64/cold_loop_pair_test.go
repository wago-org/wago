// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestColdLoopPairProofAndSourcePermutation(t *testing.T) {
	f := gpClassEmitter()
	f.pinnedLocalMask = maskOf(X0, X1, X3)
	move := func(to, from Reg) uint32 { return 0xaa0003e0 | uint32(from)<<16 | uint32(to) }
	sum := uint32(0x0b000000 | 1<<16 | 0<<5 | 1)
	base := []uint32{0x34000003, move(X4, X1), sum, move(X0, X4), 0x51000463}
	pair, ok := f.findColdLoopPair(base, 0)
	if !ok || pair.a != X0 || pair.b != X1 || pair.temporary != X4 {
		t.Fatal("canonical parallel transfer rejected")
	}
	if got, want := pair.rewriteReads(sum, X1, X0), uint32(0x0b000000|0<<16|1<<5|1); got != want {
		t.Fatalf("simultaneous remap got=%x want=%x", got, want)
	}
	madd := uint32(0x1b000000 | 1<<16 | 0<<10 | 0<<5 | 5)
	if got, want := pair.rewriteReads(madd, X1, X0), uint32(0x1b000000|0<<16|1<<10|1<<5|5); got != want {
		t.Fatalf("accumulator remap got=%x want=%x", got, want)
	}
	cold := []byte{1, 2, 3, 4}
	expected := []byte{}
	for _, word := range []uint32{move(X4, X0), move(X0, X1), move(X1, X4)} {
		expected = binary.LittleEndian.AppendUint32(expected, word)
	}
	expected = append(expected, cold...)
	if got := pair.coldRestore(cold); !bytes.Equal(got, expected) || !bytes.Equal(cold, []byte{1, 2, 3, 4}) {
		t.Fatal("cold restoration changed the result payload or register order")
	}
	for _, variant := range []string{"word-save", "word-assign", "shifted-sum", "subtract", "flag-setting", "same-input", "extra-a-write", "extra-b-write", "extra-temp-write", "temp-read", "temp-test", "reserved-temp", "pinned-temp", "owned-temp", "unpinned-a", "fixed-scratch"} {
		words := append([]uint32{}, base...)
		f.reserved, f.pinned = 0, 0
		f.pinnedLocalMask = maskOf(X0, X1, X3)
		f.regUser[X4] = nil
		switch variant {
		case "word-save":
			words[1] &= 0x7fffffff
		case "word-assign":
			words[3] &= 0x7fffffff
		case "shifted-sum":
			words[2] |= 1 << 10
		case "subtract":
			words[2] |= 1 << 30
		case "flag-setting":
			words[2] |= 1 << 29
		case "same-input":
			words[2] &^= 31 << 16
		case "extra-a-write":
			words = append([]uint32{words[0], 0x11000400}, words[1:]...)
		case "extra-b-write":
			words = append(words, 0x11000421)
		case "extra-temp-write":
			words = append(words, move(X4, ZR))
		case "temp-read":
			words = append(words, 0x0b000000|4<<16|0<<5|5)
		case "temp-test":
			words[0] = 0x34000004
		case "reserved-temp":
			f.reserved = maskOf(X4)
		case "pinned-temp":
			f.pinnedLocalMask = f.pinnedLocalMask.add(X4)
		case "owned-temp":
			f.regUser[X4] = &elem{}
		case "unpinned-a":
			f.pinnedLocalMask = f.pinnedLocalMask.remove(X0)
		case "fixed-scratch":
			words[1] = move(Reg(16), X1)
			words[3] = move(X0, Reg(16))
		}
		if _, ok := f.findColdLoopPair(words, 0); ok {
			t.Fatalf("unsafe pair admitted: %s", variant)
		}
		if len(f.a.B) != 0 {
			t.Fatal("pair proof emitted instructions")
		}
	}
}
